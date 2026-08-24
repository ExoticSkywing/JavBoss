package jav

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	defaultJavDBAppAPIHost = "https://apidd.czssdgz.com"
	// These values are the public mobile API request shape used by the
	// upstream MDCx JavDB adapter. Keeping them in this small package means
	// the rest of JavBoss does not need the full MDCx runtime.
	javDBAppSignaturePrefix = "71cf27bb3c0bcdf207b64abecddc970098c7421ee7203b9cdae54478478a199e7d5a6e1a57691123c1a931c057842fb73ba3b3c83bcd69c17ccf174081e3d8aa"
	javDBAppSignatureSuffix = "lpw6vgqzsp"
	javDBAppVersion         = "official"
	javDBAppVersionNumber   = "1.9.35"
	javDBAppDeviceUUID      = "1d26b2df-f042-5138-90b3-28980fe1d98a"
	maxJavDBAppResponseSize = 8 << 20
)

var javCodeSeparatorPattern = regexp.MustCompile(`[\s_\-]+`)

var (
	javDBAppFC2DigitsPattern      = regexp.MustCompile(`(?i)FC2(?:[\s_\-]*PPV)?[\s_\-]*(\d{5,})`)
	javDBAppSurenPrefixPattern    = regexp.MustCompile(`(?i)^\d{3,}([A-Z][A-Z0-9]*[\s_\-]+\d+)$`)
	javDBAppUncensoredCodePattern = regexp.MustCompile(`(?i)^(?:FC2|HEYZO)[\s_\-]*\d+$`)
)

var errJavDBAppMovieNotFound = errors.New("JavDB App movie not found")

// JavDBAppMagnet is a single JavDB candidate. JavDB returns Size in MiB.
type JavDBAppMagnet struct {
	Hash      string `json:"hash"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	HD        bool   `json:"hd"`
	CNSub     bool   `json:"cnsub"`
	Files     int    `json:"files_count"`
	CreatedAt string `json:"created_at"`
}

// JavDBAppMovie contains only fields needed by the input review screen.
type JavDBAppMovie struct {
	ID           string `json:"id"`
	Number       string `json:"number"`
	Title        string `json:"title"`
	ReleaseDate  string `json:"release_date"`
	MagnetsCount int    `json:"magnets_count"`
}

type javDBAppMovieDetail struct {
	ID              string                 `json:"id"`
	Number          string                 `json:"number"`
	Title           string                 `json:"title"`
	OriginTitle     string                 `json:"origin_title"`
	Summary         string                 `json:"summary"`
	ThumbURL        string                 `json:"thumb_url"`
	CoverURL        string                 `json:"cover_url"`
	Duration        int                    `json:"duration"`
	ReleaseDate     string                 `json:"release_date"`
	MakerName       string                 `json:"maker_name"`
	DirectorName    string                 `json:"director_name"`
	PublisherName   string                 `json:"publisher_name"`
	SeriesName      string                 `json:"series_name"`
	Tags            []javDBAppNamedValue   `json:"tags"`
	Actors          []javDBAppActor        `json:"actors"`
	PreviewImages   []javDBAppPreviewImage `json:"preview_images"`
	PreviewVideoURL string                 `json:"preview_video_url"`
}

type javDBAppNamedValue struct {
	Name string `json:"name"`
}

type javDBAppActor struct {
	Name   string `json:"name"`
	Gender any    `json:"gender"`
}

type javDBAppPreviewImage struct {
	LargeURL string `json:"large_url"`
	ThumbURL string `json:"thumb_url"`
}

// JavDBAppResolveItem is the result for one input line. Errors are item-scoped so a
// failed or unknown code does not discard successful results from the batch.
type JavDBAppResolveItem struct {
	InputCode string           `json:"input_code"`
	Matched   bool             `json:"matched"`
	Movie     *JavDBAppMovie   `json:"movie,omitempty"`
	Magnets   []JavDBAppMagnet `json:"magnets,omitempty"`
	Error     string           `json:"error,omitempty"`
}

// JavDBAppResolveResponse is the stable API shape consumed by the JavBoss frontend.
type JavDBAppResolveResponse struct {
	Items []JavDBAppResolveItem `json:"items"`
}

// JavDBAppOptions controls a client. Hosts are tried in order; Interval spaces all
// requests made by this client to avoid hammering the mobile API.
type JavDBAppOptions struct {
	HTTPClient *http.Client
	Hosts      []string
	Interval   time.Duration
}

type JavDBAppClient struct {
	httpClient *http.Client
	hosts      []string
	interval   time.Duration

	rateMu sync.Mutex
	next   time.Time
}

// NewJavDBAppClient constructs the JavDB client shared by resource input and
// trailer fallback. When no host is supplied, the production mirror and its
// known fallbacks are used.
func NewJavDBAppClient(options JavDBAppOptions) *JavDBAppClient {
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 35 * time.Second}
	}
	hosts := make([]string, 0, len(options.Hosts))
	for _, host := range options.Hosts {
		if host = strings.TrimRight(strings.TrimSpace(host), "/"); host != "" {
			hosts = append(hosts, host)
		}
	}
	if len(hosts) == 0 {
		hosts = []string{defaultJavDBAppAPIHost, "https://apidd.spthgb.com", "https://jdforrepam.com"}
	}
	interval := options.Interval
	if interval < 0 {
		interval = 0
	}
	return &JavDBAppClient{httpClient: httpClient, hosts: hosts, interval: interval}
}

var defaultJavDBAppClient = NewJavDBAppClient(JavDBAppOptions{Interval: defaultJavDBAppInterval()})

type javDBApp struct {
	client *JavDBAppClient
}

var javDBAppProvider lookupProvider = javDBApp{client: defaultJavDBAppClient}

func defaultJavDBAppInterval() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("JAVBOSS_JAVDB_INTERVAL_MS")); raw != "" {
		if value, err := time.ParseDuration(raw + "ms"); err == nil && value >= 0 {
			return value
		}
	}
	// The upstream adapter spaces requests by a random 3–8 seconds. This is the
	// lower bound; waitRateLimit adds up to five seconds of jitter.
	return 3 * time.Second
}

// DefaultJavDBAppClient returns the process-wide, rate-limited client used by the API.
func DefaultJavDBAppClient() *JavDBAppClient { return defaultJavDBAppClient }

func (provider javDBApp) LookupJavByCode(code string) (*JavInfo, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, ResourceNotFonud
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := provider.client
	if client == nil {
		client = DefaultJavDBAppClient()
	}
	detail, err := client.lookupMovieDetail(ctx, code)
	if err != nil {
		if errors.Is(err, errJavDBAppMovieNotFound) {
			return nil, ResourceNotFonud
		}
		return nil, err
	}
	info := javInfoFromJavDBAppDetail(detail)
	if info != nil && normalizeJAVCode(code) == normalizeJAVCode(detail.Number) && !strings.Contains(strings.ToUpper(code), "FC2-PPV") {
		info.Code = strings.ToUpper(strings.NewReplacer("_", "-", " ", "-").Replace(code))
	}
	return info, nil
}

func (javDBApp) LookupActressByCode(string) (*ActressInfo, error) {
	return nil, errors.New("javdb app: lookup actress not supported")
}

func (javDBApp) LookupActressByName(string) (*ActressInfo, error) {
	return nil, errors.New("javdb app: lookup actress not supported")
}

func (javDBApp) LookupActressURLByCodeAndName(string, string) (string, error) {
	return "", errors.New("javdb app: lookup actress URL not supported")
}

func (javDBApp) LookupSeriesURLByCode(string) (string, error) {
	return "", errors.New("javdb app: lookup series URL not supported")
}

func (javDBApp) LookupStudioURLByCode(string) (string, error) {
	return "", errors.New("javdb app: lookup studio URL not supported")
}

func javInfoFromJavDBAppDetail(detail *javDBAppMovieDetail) *JavInfo {
	if detail == nil {
		return nil
	}
	tags := make([]string, 0, len(detail.Tags))
	for _, tag := range detail.Tags {
		tags = append(tags, strings.TrimSpace(tag.Name))
	}
	actors := make([]string, 0, len(detail.Actors))
	for _, actor := range detail.Actors {
		if javDBAppActorIsFemale(actor.Gender) {
			actors = append(actors, strings.TrimSpace(actor.Name))
		}
	}
	sampleImages := make([]SampleImage, 0, len(detail.PreviewImages))
	seenImages := make(map[string]struct{}, len(detail.PreviewImages))
	for _, image := range detail.PreviewImages {
		appendSampleImage(
			&sampleImages,
			seenImages,
			normalizeJavDBAppImageURL(image.ThumbURL),
			normalizeJavDBAppImageURL(image.LargeURL),
		)
	}
	title := strings.TrimSpace(detail.Title)
	if title == "" {
		title = strings.TrimSpace(detail.OriginTitle)
	}
	info := &JavInfo{
		Title:        title,
		Code:         strings.TrimSpace(detail.Number),
		Studio:       strings.TrimSpace(detail.MakerName),
		Series:       strings.TrimSpace(detail.SeriesName),
		ReleaseUnix:  parseDateUnix(detail.ReleaseDate),
		DurationMin:  detail.Duration,
		Tags:         dedupeNonEmpty(tags),
		Actors:       dedupeNonEmpty(actors),
		CoverURL:     normalizeJavDBAppImageURL(detail.CoverURL),
		SampleImages: sampleImages,
		Provider:     ProviderJavDBApp,
	}
	if javDBAppUncensoredCodePattern.MatchString(info.Code) {
		uncensored := true
		info.IsUncensored = &uncensored
	}
	return info
}

func javDBAppActorIsFemale(gender any) bool {
	switch value := gender.(type) {
	case nil:
		return true
	case bool:
		return !value
	case float64:
		return value == 0
	case int:
		return value == 0
	case string:
		value = strings.ToLower(strings.TrimSpace(value))
		return value == "" || value == "female" || value == "女" || value == "女优" || value == "女優" || value == "0"
	default:
		return true
	}
}

func normalizeJavDBAppImageURL(raw string) string {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(value, "//") {
		value = "https:" + value
	}
	value = strings.Replace(value, "/small_covers/", "/thumbs/", 1)
	for _, oldPrefix := range []string{
		"https://tp.cmastd.com/rhe951l4q/",
		"https://tp.spfcas.com/rhe951l4q/",
	} {
		if strings.HasPrefix(value, oldPrefix) {
			value = "https://c0.jdbstatic.com/" + strings.TrimPrefix(value, oldPrefix)
			break
		}
	}
	return normalizeHTTPSURL(value)
}

// ResolveBatch resolves each input number in order. A malformed or missing
// number is reported in its own item; successful items remain usable.
func (c *JavDBAppClient) ResolveBatch(ctx context.Context, numbers []string) JavDBAppResolveResponse {
	items := make([]JavDBAppResolveItem, 0, len(numbers))
	seen := make(map[string]struct{}, len(numbers))
	for _, raw := range numbers {
		input := strings.TrimSpace(raw)
		if input == "" {
			continue
		}
		key := normalizeJAVCode(input)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		item := JavDBAppResolveItem{InputCode: input}
		movie, magnets, err := c.resolveOne(ctx, input)
		if err != nil {
			item.Error = err.Error()
		} else {
			item.Matched = true
			item.Movie = movie
			item.Magnets = magnets
		}
		items = append(items, item)
		if ctx.Err() != nil {
			break
		}
	}
	return JavDBAppResolveResponse{Items: items}
}

func (c *JavDBAppClient) resolveOne(ctx context.Context, input string) (*JavDBAppMovie, []JavDBAppMagnet, error) {
	movie, err := c.lookupMovie(ctx, input)
	if err != nil {
		return nil, nil, err
	}

	magnets, err := c.getMagnets(ctx, movie.ID)
	if err != nil {
		return movie, nil, fmt.Errorf("magnets failed for %s: %w", input, err)
	}
	return movie, magnets, nil
}

func (c *JavDBAppClient) lookupMovie(ctx context.Context, input string) (*JavDBAppMovie, error) {
	candidates := javDBAppSearchCandidates(input)
	wanted := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if key := normalizeJAVCode(candidate); key != "" {
			wanted[key] = struct{}{}
		}
	}

	var lastErr error
	hadSuccessfulSearch := false
	for _, searchCode := range candidates {
		search, err := c.getJSON(ctx, "/api/v2/search", map[string]string{"q": searchCode, "page": "1"})
		if err != nil {
			lastErr = fmt.Errorf("search %s failed: %w", searchCode, err)
			continue
		}
		hadSuccessfulSearch = true
		var searchPayload struct {
			Data struct {
				Movies []JavDBAppMovie `json:"movies"`
			} `json:"data"`
		}
		if err := json.Unmarshal(search, &searchPayload); err != nil {
			lastErr = fmt.Errorf("decode search response for %s: %w", searchCode, err)
			continue
		}
		for index := range searchPayload.Data.Movies {
			candidate := searchPayload.Data.Movies[index]
			candidate.ID = strings.TrimSpace(candidate.ID)
			candidate.Number = strings.TrimSpace(candidate.Number)
			_, exact := wanted[normalizeJAVCode(candidate.Number)]
			if candidate.ID != "" && exact {
				return &candidate, nil
			}
		}
	}
	if !hadSuccessfulSearch && lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("%w: %s", errJavDBAppMovieNotFound, input)
}

func (c *JavDBAppClient) lookupPreviewVideo(ctx context.Context, code string) (string, error) {
	detail, err := c.lookupMovieDetail(ctx, code)
	if err != nil {
		return "", err
	}
	previewURL := strings.TrimSpace(detail.PreviewVideoURL)
	if strings.HasPrefix(previewURL, "//") {
		previewURL = "https:" + previewURL
	}
	return previewURL, nil
}

func (c *JavDBAppClient) lookupMovieDetail(ctx context.Context, code string) (*javDBAppMovieDetail, error) {
	movie, err := c.lookupMovie(ctx, code)
	if err != nil {
		return nil, err
	}
	payload, err := c.getJSON(ctx, "/api/v4/movies/"+url.PathEscape(movie.ID), nil)
	if err != nil {
		return nil, fmt.Errorf("movie detail failed for %s: %w", code, err)
	}
	var response struct {
		Data struct {
			Movie javDBAppMovieDetail `json:"movie"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("decode movie detail response: %w", err)
	}
	detail := &response.Data.Movie
	if detail.Number = strings.TrimSpace(detail.Number); detail.Number == "" {
		detail.Number = movie.Number
	}
	wanted := make(map[string]struct{})
	for _, candidate := range javDBAppSearchCandidates(code) {
		wanted[normalizeJAVCode(candidate)] = struct{}{}
	}
	if _, exact := wanted[normalizeJAVCode(detail.Number)]; !exact {
		return nil, fmt.Errorf("JavDB detail code mismatch: got %s for %s", detail.Number, code)
	}
	return detail, nil
}

func javDBAppSearchCandidates(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	candidates := []string{input}
	if match := javDBAppFC2DigitsPattern.FindStringSubmatch(input); len(match) == 2 {
		candidates = append(candidates, "FC2-"+match[1], match[1])
	}
	if match := javDBAppSurenPrefixPattern.FindStringSubmatch(strings.ToUpper(input)); len(match) == 2 {
		candidates = append(candidates, match[1])
	}
	result := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		key := strings.ToUpper(candidate)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

func (c *JavDBAppClient) getMagnets(ctx context.Context, movieID string) ([]JavDBAppMagnet, error) {
	payload, err := c.getJSON(ctx, "/api/v1/movies/"+url.PathEscape(movieID)+"/magnets", nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			Magnets []JavDBAppMagnet `json:"magnets"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("decode magnets response: %w", err)
	}
	for index := range response.Data.Magnets {
		response.Data.Magnets[index].Hash = strings.TrimSpace(response.Data.Magnets[index].Hash)
	}
	return response.Data.Magnets, nil
}

func (c *JavDBAppClient) getJSON(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	var lastErr error
	for _, host := range c.hosts {
		if err := c.waitRateLimit(ctx); err != nil {
			return nil, err
		}
		endpoint, err := url.Parse(strings.TrimRight(host, "/") + path)
		if err != nil {
			lastErr = err
			continue
		}
		query := endpoint.Query()
		query.Set("platform", "android")
		query.Set("app_channel", "official")
		query.Set("app_version", javDBAppVersion)
		query.Set("app_version_number", javDBAppVersionNumber)
		query.Set("system_version", "13")
		query.Set("device_model", "Pixel 6")
		query.Set("device_name", "Pixel")
		query.Set("device_uuid", javDBAppDeviceUUID)
		for key, value := range params {
			query.Set(key, value)
		}
		endpoint.RawQuery = query.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			lastErr = err
			continue
		}
		ts := time.Now().Unix()
		hash := md5.Sum([]byte(fmt.Sprintf("%d%s", ts, javDBAppSignaturePrefix)))
		req.Header.Set("jdsignature", fmt.Sprintf("%d.%s.%s", ts, javDBAppSignatureSuffix, hex.EncodeToString(hash[:])))
		req.Header.Set("accept-language", "zh")
		req.Header.Set("User-Agent", "Dart/3.5 (dart:io)")
		response, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxJavDBAppResponseSize+1))
		response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if len(body) > maxJavDBAppResponseSize {
			lastErr = errors.New("response too large")
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			lastErr = fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
			continue
		}
		return body, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no JavDB API host configured")
	}
	return nil, lastErr
}

func (c *JavDBAppClient) waitRateLimit(ctx context.Context) error {
	if c.interval <= 0 {
		return nil
	}
	c.rateMu.Lock()
	wait := time.Until(c.next)
	if wait < 0 {
		wait = 0
	}
	c.next = time.Now().Add(wait + c.interval + time.Duration(rand.IntN(5001))*time.Millisecond)
	c.rateMu.Unlock()
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func normalizeJAVCode(value string) string {
	key := strings.ToUpper(javCodeSeparatorPattern.ReplaceAllString(strings.TrimSpace(value), ""))
	return strings.Replace(key, "FC2PPV", "FC2", 1)
}

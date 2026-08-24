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
	defaultAPIHost = "https://apidd.czssdgz.com"
	// These values are the public mobile API request shape used by the
	// upstream MDCx JavDB adapter. Keeping them in this small package means
	// the rest of JavBoss does not need the full MDCx runtime.
	signaturePrefix = "71cf27bb3c0bcdf207b64abecddc970098c7421ee7203b9cdae54478478a199e7d5a6e1a57691123c1a931c057842fb73ba3b3c83bcd69c17ccf174081e3d8aa"
	signatureSuffix = "lpw6vgqzsp"
	appVersion      = "official"
	appVersionNum   = "1.9.35"
	deviceUUID      = "1d26b2df-f042-5138-90b3-28980fe1d98a"
	maxResponseSize = 8 << 20
)

var nonCode = regexp.MustCompile(`[\s_\-]+`)

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

// NewJavDBAppClient constructs an input-side JavDB client. When no host is supplied,
// the production mirror and its known fallbacks are used.
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
		hosts = []string{defaultAPIHost, "https://apidd.spthgb.com", "https://jdforrepam.com"}
	}
	interval := options.Interval
	if interval < 0 {
		interval = 0
	}
	return &JavDBAppClient{httpClient: httpClient, hosts: hosts, interval: interval}
}

var defaultJavDBAppClient = NewJavDBAppClient(JavDBAppOptions{Interval: defaultJavDBAppInterval()})

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
		key := normalizeCode(input)
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
	search, err := c.getJSON(ctx, "/api/v2/search", map[string]string{"q": input, "page": "1"})
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	var searchPayload struct {
		Data struct {
			Movies []JavDBAppMovie `json:"movies"`
		} `json:"data"`
	}
	if err := json.Unmarshal(search, &searchPayload); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	want := normalizeCode(input)
	var movie *JavDBAppMovie
	for index := range searchPayload.Data.Movies {
		candidate := searchPayload.Data.Movies[index]
		candidate.ID = strings.TrimSpace(candidate.ID)
		candidate.Number = strings.TrimSpace(candidate.Number)
		if candidate.ID != "" && normalizeCode(candidate.Number) == want {
			movie = &candidate
			break
		}
	}
	if movie == nil {
		return nil, fmt.Errorf("%w: %s", errJavDBAppMovieNotFound, input)
	}
	return movie, nil
}

func (c *JavDBAppClient) lookupPreviewVideo(ctx context.Context, code string) (string, error) {
	movie, err := c.lookupMovie(ctx, code)
	if err != nil {
		return "", err
	}
	payload, err := c.getJSON(ctx, "/api/v4/movies/"+url.PathEscape(movie.ID), nil)
	if err != nil {
		return "", fmt.Errorf("movie detail failed for %s: %w", code, err)
	}
	var response struct {
		Data struct {
			Movie struct {
				Number          string `json:"number"`
				PreviewVideoURL string `json:"preview_video_url"`
			} `json:"movie"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return "", fmt.Errorf("decode movie detail response: %w", err)
	}
	if number := strings.TrimSpace(response.Data.Movie.Number); number != "" && normalizeCode(number) != normalizeCode(code) {
		return "", fmt.Errorf("JavDB detail code mismatch: got %s for %s", number, code)
	}
	previewURL := strings.TrimSpace(response.Data.Movie.PreviewVideoURL)
	if strings.HasPrefix(previewURL, "//") {
		previewURL = "https:" + previewURL
	}
	return previewURL, nil
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
		query.Set("app_version", appVersion)
		query.Set("app_version_number", appVersionNum)
		query.Set("system_version", "13")
		query.Set("device_model", "Pixel 6")
		query.Set("device_name", "Pixel")
		query.Set("device_uuid", deviceUUID)
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
		hash := md5.Sum([]byte(fmt.Sprintf("%d%s", ts, signaturePrefix)))
		req.Header.Set("jdsignature", fmt.Sprintf("%d.%s.%s", ts, signatureSuffix, hex.EncodeToString(hash[:])))
		req.Header.Set("accept-language", "zh")
		req.Header.Set("User-Agent", "Dart/3.5 (dart:io)")
		response, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
		response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if len(body) > maxResponseSize {
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

func normalizeCode(value string) string {
	return strings.ToUpper(nonCode.ReplaceAllString(strings.TrimSpace(value), ""))
}

// Package javdbinput contains the small, input-side JavDB client used by the
// resource discovery screen. It deliberately does not depend on JavBoss's
// presentation metadata or database packages.
package javdbinput

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

// Magnet is a single JavDB candidate. JavDB returns Size in MiB.
type Magnet struct {
	Hash      string `json:"hash"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	HD        bool   `json:"hd"`
	CNSub     bool   `json:"cnsub"`
	Files     int    `json:"files_count"`
	CreatedAt string `json:"created_at"`
}

// Movie contains only fields needed by the input review screen.
type Movie struct {
	ID           string `json:"id"`
	Number       string `json:"number"`
	Title        string `json:"title"`
	ReleaseDate  string `json:"release_date"`
	MagnetsCount int    `json:"magnets_count"`
}

// ResolveItem is the result for one input line. Errors are item-scoped so a
// failed or unknown code does not discard successful results from the batch.
type ResolveItem struct {
	InputCode string   `json:"input_code"`
	Matched   bool     `json:"matched"`
	Movie     *Movie   `json:"movie,omitempty"`
	Magnets   []Magnet `json:"magnets,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// ResolveResponse is the stable API shape consumed by the JavBoss frontend.
type ResolveResponse struct {
	Items []ResolveItem `json:"items"`
}

// Options controls a client. Hosts are tried in order; Interval spaces all
// requests made by this client to avoid hammering the mobile API.
type Options struct {
	HTTPClient *http.Client
	Hosts      []string
	Interval   time.Duration
}

type Client struct {
	httpClient *http.Client
	hosts      []string
	interval   time.Duration

	rateMu sync.Mutex
	next   time.Time
}

// NewClient constructs an input-side JavDB client. When no host is supplied,
// the production mirror and its known fallbacks are used.
func NewClient(options Options) *Client {
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
	return &Client{httpClient: httpClient, hosts: hosts, interval: interval}
}

var defaultClient = NewClient(Options{Interval: defaultInterval()})

func defaultInterval() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("JAVBOSS_JAVDB_INTERVAL_MS")); raw != "" {
		if value, err := time.ParseDuration(raw + "ms"); err == nil && value >= 0 {
			return value
		}
	}
	// The upstream adapter spaces requests by a random 3–8 seconds. A fixed
	// lower bound keeps this service predictable while retaining that safety
	// envelope for batch input.
	return 3 * time.Second
}

// DefaultClient returns the process-wide, rate-limited client used by the API.
func DefaultClient() *Client { return defaultClient }

// ResolveBatch resolves each input number in order. A malformed or missing
// number is reported in its own item; successful items remain usable.
func (c *Client) ResolveBatch(ctx context.Context, numbers []string) ResolveResponse {
	items := make([]ResolveItem, 0, len(numbers))
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
		item := ResolveItem{InputCode: input}
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
	return ResolveResponse{Items: items}
}

func (c *Client) resolveOne(ctx context.Context, input string) (*Movie, []Magnet, error) {
	search, err := c.getJSON(ctx, "/api/v2/search", map[string]string{"q": input, "page": "1"})
	if err != nil {
		return nil, nil, fmt.Errorf("search failed: %w", err)
	}
	var searchPayload struct {
		Data struct {
			Movies []Movie `json:"movies"`
		} `json:"data"`
	}
	if err := json.Unmarshal(search, &searchPayload); err != nil {
		return nil, nil, fmt.Errorf("decode search response: %w", err)
	}
	want := normalizeCode(input)
	var movie *Movie
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
		return nil, nil, fmt.Errorf("no exact JavDB match for %s", input)
	}

	magnets, err := c.getMagnets(ctx, movie.ID)
	if err != nil {
		return movie, nil, fmt.Errorf("magnets failed for %s: %w", input, err)
	}
	return movie, magnets, nil
}

func (c *Client) getMagnets(ctx context.Context, movieID string) ([]Magnet, error) {
	payload, err := c.getJSON(ctx, "/api/v1/movies/"+url.PathEscape(movieID)+"/magnets", nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			Magnets []Magnet `json:"magnets"`
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

func (c *Client) getJSON(ctx context.Context, path string, params map[string]string) ([]byte, error) {
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

func (c *Client) waitRateLimit(ctx context.Context) error {
	if c.interval <= 0 {
		return nil
	}
	c.rateMu.Lock()
	wait := time.Until(c.next)
	if wait < 0 {
		wait = 0
	}
	c.next = time.Now().Add(wait + c.interval + time.Duration(rand.IntN(250))*time.Millisecond)
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

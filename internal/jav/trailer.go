package jav

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultDMMTrailerEndpoint = "https://api.thejavdb.net/v1/movies"
	maxTrailerResponseSize    = 2 << 20
	defaultTrailerCacheTTL    = 6 * time.Hour
	defaultTrailerMissTTL     = 30 * time.Minute
	maxTrailerCacheEntries    = 4096
)

// ErrTrailerNotFound indicates that every configured source confirmed a miss.
var ErrTrailerNotFound = errors.New("jav trailer not found")

// Trailer is the lightweight response consumed by the JAV detail view.
type Trailer struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

// TrailerResolverOptions configures a resolver. The DMM endpoint returns a
// DMM-hosted sample_movie_url using the same adapter tracked by MDCx.
type TrailerResolverOptions struct {
	HTTPClient       *http.Client
	DMMEndpoint      string
	JavDBAppClient   *JavDBAppClient
	CacheTTL         time.Duration
	NegativeCacheTTL time.Duration
}

type cachedTrailer struct {
	trailer   *Trailer
	expiresAt time.Time
}

// TrailerResolver resolves DMM-hosted previews first and falls back to the
// JavDB App detail API. It has no dependency on the MDCx runtime.
type TrailerResolver struct {
	httpClient       *http.Client
	dmmEndpoint      string
	javDBAppClient   *JavDBAppClient
	cacheTTL         time.Duration
	negativeCacheTTL time.Duration

	cacheMu sync.Mutex
	cache   map[string]cachedTrailer
}

func NewTrailerResolver(options TrailerResolverOptions) *TrailerResolver {
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	dmmEndpoint := strings.TrimSpace(options.DMMEndpoint)
	if dmmEndpoint == "" {
		dmmEndpoint = defaultDMMTrailerEndpoint
	}
	javDBAppClient := options.JavDBAppClient
	if javDBAppClient == nil {
		javDBAppClient = DefaultJavDBAppClient()
	}
	cacheTTL := options.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = defaultTrailerCacheTTL
	}
	negativeCacheTTL := options.NegativeCacheTTL
	if negativeCacheTTL <= 0 {
		negativeCacheTTL = defaultTrailerMissTTL
	}
	return &TrailerResolver{
		httpClient:       httpClient,
		dmmEndpoint:      dmmEndpoint,
		javDBAppClient:   javDBAppClient,
		cacheTTL:         cacheTTL,
		negativeCacheTTL: negativeCacheTTL,
		cache:            make(map[string]cachedTrailer),
	}
}

var defaultTrailerResolver = NewTrailerResolver(TrailerResolverOptions{})

func DefaultTrailerResolver() *TrailerResolver { return defaultTrailerResolver }

// Resolve returns the first usable trailer. Refresh skips the in-memory cache.
func (r *TrailerResolver) Resolve(ctx context.Context, code string, refresh bool) (*Trailer, error) {
	code = strings.TrimSpace(code)
	key := normalizeCode(code)
	if key == "" {
		return nil, ErrTrailerNotFound
	}
	if !refresh {
		if trailer, ok := r.cached(key, time.Now()); ok {
			if trailer == nil {
				return nil, ErrTrailerNotFound
			}
			return trailer, nil
		}
	}

	dmmTrailer, dmmErr := r.lookupDMMTrailer(ctx, code)
	if dmmErr == nil && dmmTrailer != nil {
		r.store(key, dmmTrailer, r.cacheTTL)
		return cloneTrailer(dmmTrailer), nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	appURL, appErr := r.javDBAppClient.lookupPreviewVideo(ctx, code)
	if appErr == nil && strings.TrimSpace(appURL) != "" {
		if normalized := normalizeHTTPSURL(appURL); normalized != "" {
			trailer := &Trailer{URL: normalized, Source: "javdb_app"}
			r.store(key, trailer, r.cacheTTL)
			return cloneTrailer(trailer), nil
		}
		appErr = errors.New("JavDB App returned an invalid preview URL")
	}
	if appErr == nil || errors.Is(appErr, errJavDBAppMovieNotFound) {
		appErr = ErrTrailerNotFound
	}

	if errors.Is(dmmErr, ErrTrailerNotFound) && errors.Is(appErr, ErrTrailerNotFound) {
		r.store(key, nil, r.negativeCacheTTL)
		return nil, ErrTrailerNotFound
	}
	return nil, errors.Join(
		wrapTrailerSourceError("DMM", dmmErr),
		wrapTrailerSourceError("JavDB App", appErr),
	)
}

func (r *TrailerResolver) lookupDMMTrailer(ctx context.Context, code string) (*Trailer, error) {
	endpoint, err := url.Parse(r.dmmEndpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, errors.New("DMM trailer endpoint is invalid")
	}
	query := endpoint.Query()
	query.Set("q", code)
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create DMM trailer request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "JavBoss/1.0")
	response, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request DMM trailer catalog: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, ErrTrailerNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("DMM trailer catalog returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTrailerResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("read DMM trailer response: %w", err)
	}
	if len(body) > maxTrailerResponseSize {
		return nil, errors.New("DMM trailer response is too large")
	}
	var payload struct {
		UniversalID    string `json:"universal_id"`
		SampleMovieURL string `json:"sample_movie_url"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode DMM trailer response: %w", err)
	}
	if normalizeCode(payload.UniversalID) != normalizeCode(code) {
		return nil, ErrTrailerNotFound
	}
	trailerURL := normalizeDMMTrailerURL(payload.SampleMovieURL)
	if trailerURL == "" {
		return nil, ErrTrailerNotFound
	}
	return &Trailer{URL: trailerURL, Source: "thejavdb_dmm"}, nil
}

func (r *TrailerResolver) cached(key string, now time.Time) (*Trailer, bool) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	entry, ok := r.cache[key]
	if !ok {
		return nil, false
	}
	if !now.Before(entry.expiresAt) {
		delete(r.cache, key)
		return nil, false
	}
	return cloneTrailer(entry.trailer), true
}

func (r *TrailerResolver) store(key string, trailer *Trailer, ttl time.Duration) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	now := time.Now()
	if len(r.cache) >= maxTrailerCacheEntries {
		for cachedKey, entry := range r.cache {
			if !now.Before(entry.expiresAt) {
				delete(r.cache, cachedKey)
			}
		}
	}
	if len(r.cache) >= maxTrailerCacheEntries {
		for cachedKey := range r.cache {
			delete(r.cache, cachedKey)
			break
		}
	}
	r.cache[key] = cachedTrailer{trailer: cloneTrailer(trailer), expiresAt: now.Add(ttl)}
}

func cloneTrailer(trailer *Trailer) *Trailer {
	if trailer == nil {
		return nil
	}
	copy := *trailer
	return &copy
}

func normalizeDMMTrailerURL(raw string) string {
	normalized := normalizeHTTPSURL(raw)
	if normalized == "" {
		return ""
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "dmm.co.jp" && !strings.HasSuffix(host, ".dmm.co.jp") && host != "dmm.com" && !strings.HasSuffix(host, ".dmm.com") {
		return ""
	}
	return parsed.String()
}

func normalizeHTTPSURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" {
		return ""
	}
	parsed.Fragment = ""
	return parsed.String()
}

func wrapTrailerSourceError(source string, err error) error {
	if err == nil || errors.Is(err, ErrTrailerNotFound) {
		return nil
	}
	return fmt.Errorf("%s trailer lookup: %w", source, err)
}

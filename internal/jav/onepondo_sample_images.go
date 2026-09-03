package jav

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"javboss/internal/util"
)

const (
	onePondoBaseURL            = "https://www.1pondo.tv"
	maxOnePondoGalleryResponse = 2 << 20
)

var onePondoCodePattern = regexp.MustCompile(`^\d{6}_\d{3}$`)

type onePondoGalleryResponse struct {
	Rows []onePondoGalleryRow `json:"Rows"`
}

type onePondoGalleryRow struct {
	MovieID   string `json:"MovieID"`
	Filename  string `json:"Filename"`
	Protected bool   `json:"Protected"`
}

// LookupOnePondoSampleImages returns the public photo gallery exposed by
// 1Pondo's official movie page. JavDB does not always copy these images for
// uncensored works even when the original studio still serves them.
func LookupOnePondoSampleImages(ctx context.Context, code string) ([]SampleImage, error) {
	return lookupOnePondoSampleImages(ctx, code, onePondoBaseURL, util.NewHTTPClient(20*time.Second))
}

func lookupOnePondoSampleImages(ctx context.Context, code, baseURL string, client *http.Client) ([]SampleImage, error) {
	code = strings.TrimSpace(code)
	if !onePondoCodePattern.MatchString(code) {
		return []SampleImage{}, nil
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("1pondo base URL is empty")
	}
	if client == nil {
		client = util.NewHTTPClient(20 * time.Second)
	}

	galleryURL := fmt.Sprintf(
		"%s/dyn/phpauto/movie_galleries/movie_id/%s.json",
		baseURL,
		url.PathEscape(code),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, galleryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build 1pondo gallery request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request 1pondo gallery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return []SampleImage{}, nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("1pondo gallery returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOnePondoGalleryResponse+1))
	if err != nil {
		return nil, fmt.Errorf("read 1pondo gallery: %w", err)
	}
	if len(body) > maxOnePondoGalleryResponse {
		return nil, errors.New("1pondo gallery response is too large")
	}
	var gallery onePondoGalleryResponse
	if err := json.Unmarshal(body, &gallery); err != nil {
		return nil, fmt.Errorf("decode 1pondo gallery: %w", err)
	}

	images := make([]SampleImage, 0, len(gallery.Rows))
	seen := make(map[string]struct{}, len(gallery.Rows))
	for _, row := range gallery.Rows {
		if row.Protected || strings.TrimSpace(row.MovieID) != code {
			continue
		}
		filename := strings.TrimSpace(row.Filename)
		if filename == "" || path.Base(filename) != filename || !strings.EqualFold(path.Ext(filename), ".jpg") {
			continue
		}
		filename = url.PathEscape(filename)
		appendSampleImage(
			&images,
			seen,
			fmt.Sprintf("%s/assets/sample/%s/thum_106/%s", baseURL, url.PathEscape(code), filename),
			fmt.Sprintf("%s/assets/sample/%s/popu/%s", baseURL, url.PathEscape(code), filename),
		)
	}
	return images, nil
}

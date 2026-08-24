package jav

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTrailerResolverPrefersDMMAndCaches(t *testing.T) {
	var requests atomic.Int32
	dmmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("q") != "DPMX-004" {
			t.Fatalf("query = %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"universal_id":"DPMX-004","sample_movie_url":"https://cc3001.dmm.co.jp/pv/token/dpmx004_dmb_w.mp4"}`))
	}))
	defer dmmServer.Close()

	var appRequests atomic.Int32
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appRequests.Add(1)
		http.NotFound(w, r)
	}))
	defer appServer.Close()
	resolver := NewTrailerResolver(TrailerResolverOptions{
		DMMEndpoint:    dmmServer.URL,
		JavDBAppClient: NewJavDBAppClient(JavDBAppOptions{Hosts: []string{appServer.URL}}),
		CacheTTL:       time.Hour,
	})

	for range 2 {
		trailer, err := resolver.Resolve(context.Background(), "DPMX-004", false)
		if err != nil {
			t.Fatalf("resolve trailer: %v", err)
		}
		if trailer.Source != "thejavdb_dmm" || trailer.URL != "https://cc3001.dmm.co.jp/pv/token/dpmx004_dmb_w.mp4" {
			t.Fatalf("trailer = %#v", trailer)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("DMM request count = %d", requests.Load())
	}
	if appRequests.Load() != 0 {
		t.Fatalf("JavDB App request count = %d", appRequests.Load())
	}
}

func TestTrailerResolverFallsBackToJavDBApp(t *testing.T) {
	dmmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer dmmServer.Close()
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/search":
			_, _ = w.Write([]byte(`{"data":{"movies":[{"id":"movie-1","number":"TEST-001"}]}}`))
		case "/api/v4/movies/movie-1":
			_, _ = w.Write([]byte(`{"data":{"movie":{"number":"TEST-001","preview_video_url":"https://media.javdb.example/preview.mp4"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer appServer.Close()
	resolver := NewTrailerResolver(TrailerResolverOptions{
		DMMEndpoint:    dmmServer.URL,
		JavDBAppClient: NewJavDBAppClient(JavDBAppOptions{Hosts: []string{appServer.URL}}),
	})

	trailer, err := resolver.Resolve(context.Background(), "TEST-001", false)
	if err != nil {
		t.Fatalf("resolve trailer: %v", err)
	}
	if trailer.Source != "javdb_app" || trailer.URL != "https://media.javdb.example/preview.mp4" {
		t.Fatalf("trailer = %#v", trailer)
	}
}

func TestTrailerResolverCachesConfirmedMiss(t *testing.T) {
	var dmmRequests atomic.Int32
	dmmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dmmRequests.Add(1)
		http.NotFound(w, r)
	}))
	defer dmmServer.Close()
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"movies":[]}}`))
	}))
	defer appServer.Close()
	resolver := NewTrailerResolver(TrailerResolverOptions{
		DMMEndpoint:      dmmServer.URL,
		JavDBAppClient:   NewJavDBAppClient(JavDBAppOptions{Hosts: []string{appServer.URL}}),
		NegativeCacheTTL: time.Hour,
	})

	for range 2 {
		_, err := resolver.Resolve(context.Background(), "MISS-001", false)
		if !errors.Is(err, ErrTrailerNotFound) {
			t.Fatalf("resolve miss error = %v", err)
		}
	}
	if dmmRequests.Load() != 1 {
		t.Fatalf("DMM request count = %d", dmmRequests.Load())
	}
}

func TestNormalizeDMMTrailerURLRejectsNonDMMHost(t *testing.T) {
	if got := normalizeDMMTrailerURL("https://example.com/preview.mp4"); got != "" {
		t.Fatalf("normalized URL = %q", got)
	}
}

func TestTrailerResolverLiveDPMX(t *testing.T) {
	if os.Getenv("JAVBOSS_LIVE_TEST") != "1" {
		t.Skip("set JAVBOSS_LIVE_TEST=1 to query real trailer providers")
	}
	trailer, err := NewTrailerResolver(TrailerResolverOptions{}).Resolve(
		context.Background(),
		"DPMX-004",
		true,
	)
	if err != nil {
		t.Fatalf("resolve live trailer: %v", err)
	}
	if trailer == nil || trailer.Source != "thejavdb_dmm" || !strings.Contains(trailer.URL, "dmm.co.jp") {
		t.Fatalf("live trailer = %#v", trailer)
	}
}

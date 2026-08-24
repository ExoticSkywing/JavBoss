package javdbinput

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveBatchRequiresExactCodeAndGetsMagnets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v2/search":
			if got := r.URL.Query().Get("q"); got != "dpmx_004" {
				t.Errorf("search q = %q", got)
			}
			if got := r.URL.Query().Get("page"); got != "1" {
				t.Errorf("search page = %q", got)
			}
			if got := r.URL.Query().Get("platform"); got != "android" {
				t.Errorf("search platform = %q", got)
			}
			_, _ = w.Write([]byte(`{"data":{"movies":[{"id":"movie-1","number":"DPMX-004","title":"Title","release_date":"2026-08-24","magnets_count":1},{"id":"movie-2","number":"DPMX-0040"}]}}`))
		case r.URL.Path == "/api/v1/movies/movie-1/magnets":
			_, _ = w.Write([]byte(`{"data":{"magnets":[{"hash":"ABC","name":"DPMX-004.mp4","size":4096,"hd":true,"cnsub":false,"files_count":1}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(Options{Hosts: []string{server.URL}})
	response := client.ResolveBatch(context.Background(), []string{"dpmx_004", "DPMX-004"})
	if len(response.Items) != 1 || !response.Items[0].Matched {
		t.Fatalf("response = %#v", response)
	}
	item := response.Items[0]
	if item.Movie == nil || item.Movie.Number != "DPMX-004" || item.Movie.Title != "Title" || len(item.Magnets) != 1 {
		t.Fatalf("item = %#v", item)
	}
	if got := item.Magnets[0].Hash; got != "ABC" {
		t.Fatalf("magnet hash = %q", got)
	}
}

func TestResolveBatchKeepsItemScopedErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v2/search" && r.URL.Query().Get("q") == "BAD-001":
			_, _ = w.Write([]byte(`{"data":{"movies":[]}}`))
		case r.URL.Path == "/api/v2/search" && r.URL.Query().Get("q") == "OK-001":
			_, _ = w.Write([]byte(`{"data":{"movies":[{"id":"m","number":"OK-001","title":"OK"}]}}`))
		case r.URL.Path == "/api/v1/movies/m/magnets":
			_, _ = w.Write([]byte(`{"data":{"magnets":[{"hash":"OKHASH","name":"OK-001.mp4","size":2048,"files_count":1}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(Options{Hosts: []string{server.URL}})
	response := client.ResolveBatch(context.Background(), []string{"BAD-001", "OK-001"})
	if len(response.Items) != 2 {
		t.Fatalf("response = %#v", response)
	}
	if response.Items[0].Matched || response.Items[0].Error == "" {
		t.Fatalf("failed item = %#v", response.Items[0])
	}
	if !response.Items[1].Matched || response.Items[1].Error != "" || len(response.Items[1].Magnets) != 1 {
		t.Fatalf("successful item = %#v", response.Items[1])
	}
}

package jav

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
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

	client := NewJavDBAppClient(JavDBAppOptions{Hosts: []string{server.URL}})
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

func TestJavDBAppProviderMapsFullMovieDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v2/search" && r.URL.Query().Get("q") == "FC2-PPV-1579280":
			_, _ = w.Write([]byte(`{"data":{"movies":[]}}`))
		case r.URL.Path == "/api/v2/search" && r.URL.Query().Get("q") == "FC2-1579280":
			_, _ = w.Write([]byte(`{"data":{"movies":[{"id":"movie-fc2","number":"FC2-1579280"}]}}`))
		case r.URL.Path == "/api/v4/movies/movie-fc2":
			_, _ = w.Write([]byte(`{"data":{"movie":{"id":"movie-fc2","number":"FC2-1579280","title":"Localized title","origin_title":"Original title","cover_url":"https://tp.spfcas.com/rhe951l4q/small_covers/cover.jpg","duration":121,"release_date":"2020-05-04","maker_name":"Maker","series_name":"Series","tags":[{"name":"Tag"},{"name":"Tag"}],"actors":[{"name":"Actress","gender":0},{"name":"Actor","gender":1}],"preview_images":[{"thumb_url":"//img.example/thumb.jpg","large_url":"https://img.example/large.jpg"}],"preview_video_url":"//media.example/preview.mp4"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := javDBApp{client: NewJavDBAppClient(JavDBAppOptions{Hosts: []string{server.URL}})}
	info, err := provider.LookupJavByCode("FC2-PPV-1579280")
	if err != nil {
		t.Fatalf("lookup metadata: %v", err)
	}
	if info.Code != "FC2-1579280" || info.Title != "Localized title" || info.Studio != "Maker" || info.Series != "Series" {
		t.Fatalf("metadata = %#v", info)
	}
	if info.ReleaseUnix != parseDateUnix("2020-05-04") || info.DurationMin != 121 {
		t.Fatalf("release/runtime = %d/%d", info.ReleaseUnix, info.DurationMin)
	}
	if len(info.Tags) != 1 || info.Tags[0] != "Tag" || len(info.Actors) != 1 || info.Actors[0] != "Actress" {
		t.Fatalf("tags/actors = %#v/%#v", info.Tags, info.Actors)
	}
	if info.CoverURL != "https://c0.jdbstatic.com/thumbs/cover.jpg" {
		t.Fatalf("cover URL = %q", info.CoverURL)
	}
	if len(info.SampleImages) != 1 || info.SampleImages[0].ThumbnailURL != "https://img.example/thumb.jpg" || info.SampleImages[0].DetailURL != "https://img.example/large.jpg" {
		t.Fatalf("sample images = %#v", info.SampleImages)
	}
	if info.Provider != ProviderJavDBApp || info.IsUncensored == nil || !*info.IsUncensored {
		t.Fatalf("provider/uncensored = %s/%v", info.Provider, info.IsUncensored)
	}
}

func TestJavDBAppProviderReturnsResourceNotFoundForExactMiss(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"movies":[{"id":"near","number":"ABC-0010"}]}}`))
	}))
	defer server.Close()

	provider := javDBApp{client: NewJavDBAppClient(JavDBAppOptions{Hosts: []string{server.URL}})}
	_, err := provider.LookupJavByCode("ABC-001")
	if !errors.Is(err, ResourceNotFonud) {
		t.Fatalf("lookup error = %v, want resource not found", err)
	}
}

func TestJavDBAppSearchCandidatesNormalizeSpecialCodes(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{input: "FC2-PPV-1579280", want: []string{"FC2-PPV-1579280", "FC2-1579280", "1579280"}},
		{input: "259LUXU-1033", want: []string{"259LUXU-1033", "LUXU-1033"}},
		{input: "HEYZO-0678", want: []string{"HEYZO-0678"}},
	}
	for _, tt := range tests {
		if got := javDBAppSearchCandidates(tt.input); !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("javDBAppSearchCandidates(%q) = %#v, want %#v", tt.input, got, tt.want)
		}
	}
	if normalizeJAVCode("FC2-PPV-1579280") != normalizeJAVCode("FC2-1579280") {
		t.Fatal("FC2 PPV aliases should have the same comparison key")
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
	client := NewJavDBAppClient(JavDBAppOptions{Hosts: []string{server.URL}})
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

func TestLookupPreviewVideoUsesAppMovieDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v2/search":
			_, _ = w.Write([]byte(`{"data":{"movies":[{"id":"movie-1","number":"DPMX-004"}]}}`))
		case "/api/v4/movies/movie-1":
			_, _ = w.Write([]byte(`{"data":{"movie":{"number":"DPMX-004","preview_video_url":"//media.example/DPMX-004.mp4"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewJavDBAppClient(JavDBAppOptions{Hosts: []string{server.URL}})
	previewURL, err := client.lookupPreviewVideo(context.Background(), "DPMX-004")
	if err != nil {
		t.Fatalf("lookup preview video: %v", err)
	}
	if previewURL != "https://media.example/DPMX-004.mp4" {
		t.Fatalf("preview URL = %q", previewURL)
	}
}

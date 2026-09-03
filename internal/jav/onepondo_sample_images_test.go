package jav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestLookupOnePondoSampleImagesReturnsOnlyPublicGalleryRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dyn/phpauto/movie_galleries/movie_id/062913_618.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Rows":[
			{"MovieID":"062913_618","Filename":"1.jpg","Protected":false},
			{"MovieID":"062913_618","Filename":"2.jpg","Protected":true},
			{"MovieID":"other","Filename":"3.jpg","Protected":false},
			{"MovieID":"062913_618","Filename":"../4.jpg","Protected":false},
			{"MovieID":"062913_618","Filename":"5.png","Protected":false}
		]}`))
	}))
	defer server.Close()

	got, err := lookupOnePondoSampleImages(context.Background(), "062913_618", server.URL, server.Client())
	if err != nil {
		t.Fatalf("lookup 1pondo sample images: %v", err)
	}
	want := []SampleImage{{
		ThumbnailURL: server.URL + "/assets/sample/062913_618/thum_106/1.jpg",
		DetailURL:    server.URL + "/assets/sample/062913_618/popu/1.jpg",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sample images = %#v, want %#v", got, want)
	}
}

func TestLookupOnePondoSampleImagesSkipsOtherCodeFormats(t *testing.T) {
	requested := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requested = true
	}))
	defer server.Close()

	images, err := lookupOnePondoSampleImages(context.Background(), "N1355", server.URL, server.Client())
	if err != nil {
		t.Fatalf("lookup unsupported code: %v", err)
	}
	if requested {
		t.Fatal("unsupported code triggered a 1pondo request")
	}
	if len(images) != 0 {
		t.Fatalf("sample image count = %d, want 0", len(images))
	}
}

func TestLookupOnePondoSampleImagesPreservesTemporaryErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	images, err := lookupOnePondoSampleImages(context.Background(), "062913_618", server.URL, server.Client())
	if err == nil {
		t.Fatal("temporary upstream failure returned no error")
	}
	if len(images) != 0 {
		t.Fatalf("sample image count = %d, want 0", len(images))
	}
}

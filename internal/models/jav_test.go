package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestJavJSONUsesEmptySampleImageArray(t *testing.T) {
	data, err := json.Marshal(Jav{Code: "ABC-001"})
	if err != nil {
		t.Fatalf("marshal jav: %v", err)
	}
	if !strings.Contains(string(data), `"sample_images":[]`) {
		t.Fatalf("sample_images is not an empty array: %s", data)
	}
}

func TestJavSampleImagesNotFoundSentinel(t *testing.T) {
	images := NewJavSampleImagesNotFound()
	if !images.IsNotFound() {
		t.Fatalf("not-found sentinel was not recognized: %#v", images)
	}
	data, err := json.Marshal(images)
	if err != nil {
		t.Fatalf("marshal sentinel: %v", err)
	}
	if string(data) != `[{"thumbnail_url":":not_found","detail_url":":not_found"}]` {
		t.Fatalf("unexpected sentinel JSON: %s", data)
	}
}

func TestNormalizeJavCode(t *testing.T) {
	tests := map[string]string{
		" IPX-001 ":       "IPX001",
		"ipx_001":         "IPX001",
		"IPX 001":         "IPX001",
		"FC2-PPV-1579280": "FC21579280",
		"fc2_1579280":     "FC21579280",
		"110316-005":      "110316005",
		"---":             "",
	}
	for input, want := range tests {
		if got := NormalizeJavCode(input); got != want {
			t.Errorf("NormalizeJavCode(%q) = %q, want %q", input, got, want)
		}
	}
}

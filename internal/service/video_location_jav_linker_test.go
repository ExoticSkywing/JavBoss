package service

import (
	"reflect"
	"testing"

	"javboss/internal/jav"
)

func TestJavScrapeCodesForVideoUsesForcedCodeOnly(t *testing.T) {
	got := javScrapeCodesForVideo("ABC-001 DEF-002.mp4", "XYZ-999")
	want := []string{"XYZ-999"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("javScrapeCodesForVideo() = %#v, want %#v", got, want)
	}
}

func TestJavLinkProvidersForCode(t *testing.T) {
	tests := []struct {
		name string
		code string
		want []jav.Provider
	}{
		{
			name: "gana prefers javmenu",
			code: "gana-1234",
			want: []jav.Provider{jav.ProviderJavMenu, jav.ProviderJavBus},
		},
		{
			name: "stars prefers javbus",
			code: " STARS-001 ",
			want: []jav.Provider{jav.ProviderJavBus, jav.ProviderAvmoo},
		},
		{
			name: "ap uses avmoo only",
			code: "ap-001",
			want: []jav.Provider{jav.ProviderAvmoo},
		},
		{
			name: "other uses javbus only",
			code: "IPX-228",
			want: []jav.Provider{jav.ProviderJavBus},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := javLinkProvidersForCode(tt.code)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("javLinkProvidersForCode(%q) = %#v, want %#v", tt.code, got, tt.want)
			}
		})
	}
}

func TestForcedJavScrapeCodeSupportsManualOverride(t *testing.T) {
	got := forcedJavScrapeCode(":manual:abc-001")
	if got != "ABC-001" {
		t.Fatalf("forcedJavScrapeCode() = %q, want ABC-001", got)
	}
}

func TestJavLinkBatchSummarizesScrapeOutcomes(t *testing.T) {
	batch := &javLinkBatch{}
	batch.record(javLinkResult{Outcome: javLinkOutcomeAlreadyLinked})
	batch.record(javLinkResult{Outcome: javLinkOutcomeExistingLinked})
	batch.record(javLinkResult{Outcome: javLinkOutcomeScraped, Provider: jav.ProviderJavBus})
	batch.record(javLinkResult{Outcome: javLinkOutcomeScraped, Provider: jav.ProviderJavDBApp})
	batch.record(javLinkResult{Outcome: javLinkOutcomeSkipped})
	batch.record(javLinkResult{Outcome: javLinkOutcomeNoCode})
	batch.record(javLinkResult{Outcome: javLinkOutcomeNotFound})
	batch.record(javLinkResult{Outcome: javLinkOutcomeError})

	got := batch.Summary()
	want := JavLinkSummary{
		Processed:       8,
		AlreadyLinked:   1,
		ExistingLinked:  1,
		Scraped:         2,
		JavDBAppScraped: 1,
		Skipped:         1,
		NoCode:          1,
		NotFound:        1,
		Errors:          1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JavLinkSummary = %#v, want %#v", got, want)
	}
}

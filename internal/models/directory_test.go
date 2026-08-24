package models

import (
	"reflect"
	"testing"
)

func TestDirectoryScanSummaryValueAndScan(t *testing.T) {
	want := DirectoryScanSummary{
		FilesSeen:         12,
		Inserted:          3,
		Updated:           4,
		Removed:           2,
		JavProcessed:      12,
		JavAlreadyLinked:  4,
		JavExistingLinked: 2,
		JavScraped:        3,
		JavDBAppScraped:   1,
		JavSkipped:        1,
		JavNoCode:         1,
		JavNotFound:       1,
		JavErrors:         0,
		DurationMS:        1500,
		FinishedAtUnixMS:  1720000000123,
	}
	value, err := want.Value()
	if err != nil {
		t.Fatalf("encode directory scan summary: %v", err)
	}

	var got DirectoryScanSummary
	if err := got.Scan(value); err != nil {
		t.Fatalf("scan directory scan summary: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("directory scan summary = %+v, want %+v", got, want)
	}
}

func TestDirectoryScanSummaryScanEmptyValue(t *testing.T) {
	summary := DirectoryScanSummary{FilesSeen: 1}
	if err := summary.Scan(nil); err != nil {
		t.Fatalf("scan empty directory summary: %v", err)
	}
	if summary != (DirectoryScanSummary{}) {
		t.Fatalf("empty directory summary = %+v", summary)
	}
}

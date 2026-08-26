package service

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"javboss/internal/common"
	"javboss/internal/db"
	"javboss/internal/jav"
	"javboss/internal/models"
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

func TestProcessVideoLocationJavLinkTreatsCanonicalForcedAliasAsAlreadyLinked(t *testing.T) {
	gdb, err := db.Open(filepath.Join(t.TempDir(), "forced-alias-link.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	previousDB := common.DB
	common.DB = gdb
	t.Cleanup(func() {
		common.DB = previousDB
		if sqlDB, dbErr := gdb.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	previousLookup := lookupJavByCodeForLocationLink
	lookupCalls := 0
	lookupJavByCodeForLocationLink = func(string, jav.Provider) (*jav.JavInfo, error) {
		lookupCalls++
		return nil, jav.ResourceNotFonud
	}
	t.Cleanup(func() {
		lookupJavByCodeForLocationLink = previousLookup
	})

	ctx := context.Background()
	canonical, err := db.SaveJavInfo(ctx, &jav.JavInfo{
		Code:     "FC2-123456",
		Title:    "Canonical FC2 work",
		Provider: jav.ProviderJavBus,
	})
	if err != nil {
		t.Fatalf("save canonical jav: %v", err)
	}
	directory := models.Directory{Path: "/tmp/forced-alias-media"}
	if err := gdb.Create(&directory).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	video := models.Video{Fingerprint: "forced-alias-video", DurationSec: 3600}
	if err := gdb.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	location, err := db.UpsertVideoLocation(
		ctx,
		video.ID,
		directory.ID,
		"FC2-PPV-123456.mp4",
		time.Unix(1710000000, 0).UTC(),
	)
	if err != nil {
		t.Fatalf("create video location: %v", err)
	}
	if err := db.SetVideoLocationJavIDForVideo(ctx, location.ID, video.ID, canonical.ID, location.UpdatedAt); err != nil {
		t.Fatalf("link canonical jav: %v", err)
	}
	if _, err := db.UpdateVideoJavScrapeOverride(ctx, video.ID, models.JavScrapeOverrideManualPrefix+"FC2-PPV-123456"); err != nil {
		t.Fatalf("set alias forced override: %v", err)
	}

	result, err := processVideoLocationJavLinkResult(ctx, location.ID)
	if err != nil {
		t.Fatalf("process canonical forced alias: %v", err)
	}
	if result.Outcome != javLinkOutcomeAlreadyLinked {
		t.Fatalf("link outcome = %v, want already-linked", result.Outcome)
	}
	if lookupCalls != 0 {
		t.Fatalf("provider lookup calls = %d, want 0", lookupCalls)
	}
	var linked models.VideoLocation
	if err := gdb.First(&linked, location.ID).Error; err != nil {
		t.Fatalf("reload linked location: %v", err)
	}
	if linked.JavID == nil || *linked.JavID != canonical.ID {
		t.Fatalf("location jav id = %#v, want %d", linked.JavID, canonical.ID)
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

func TestProcessVideoLocationJavLinkUsesPreScrapedJavWithoutLookup(t *testing.T) {
	gdb, err := db.Open(filepath.Join(t.TempDir(), "pre-scraped-link.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	previousDB := common.DB
	common.DB = gdb
	t.Cleanup(func() {
		common.DB = previousDB
		if sqlDB, dbErr := gdb.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	previousLookup := lookupJavByCodeForLocationLink
	lookupCalls := 0
	lookupJavByCodeForLocationLink = func(string, jav.Provider) (*jav.JavInfo, error) {
		lookupCalls++
		return nil, jav.ResourceNotFonud
	}
	t.Cleanup(func() {
		lookupJavByCodeForLocationLink = previousLookup
	})

	ctx := context.Background()
	inputBatch, err := db.CreateJavInputBatch(ctx, "pre_001")
	if err != nil {
		t.Fatalf("create top-down JAV input: %v", err)
	}
	if inputBatch.AcceptedCount != 1 || len(inputBatch.Items) != 1 || inputBatch.Items[0].JavID == nil {
		t.Fatalf("top-down input did not create one canonical work: %#v", inputBatch)
	}
	preScraped, err := db.SaveJavInfo(ctx, &jav.JavInfo{
		Code:     "PRE-001",
		Title:    "Metadata prepared before the file exists",
		Provider: jav.ProviderJavBus,
	})
	if err != nil {
		t.Fatalf("save pre-scraped jav: %v", err)
	}
	if preScraped.ID != *inputBatch.Items[0].JavID {
		t.Fatalf("pre-scrape jav id = %d, input jav id = %d", preScraped.ID, *inputBatch.Items[0].JavID)
	}
	pending, pendingTotal, err := db.SearchJavWithPrefixFilters(
		ctx, nil, nil, "", "", "code", 20, 0, nil, nil,
		db.JavSearchFilters{StudioID: -1, Inventory: models.JavInventoryPending},
	)
	if err != nil {
		t.Fatalf("list pending JAVs before scan: %v", err)
	}
	if pendingTotal != 1 || len(pending) != 1 || pending[0].ID != preScraped.ID ||
		pending[0].AcquisitionStage != models.JavAcquisitionStageMagnetCollecting {
		t.Fatalf("pending work before scan = total %d items %#v", pendingTotal, pending)
	}
	dir := models.Directory{Path: "/tmp/pre-scraped-media"}
	if err := gdb.Create(&dir).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	video := models.Video{
		Fingerprint: "pre-scraped-video-content",
		DurationSec: 3600,
	}
	if err := gdb.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	location, err := db.UpsertVideoLocation(
		ctx,
		video.ID,
		dir.ID,
		"incoming/PRE-001.mp4",
		time.Unix(1710000000, 0).UTC(),
	)
	if err != nil {
		t.Fatalf("create video location: %v", err)
	}

	result, err := processVideoLocationJavLinkResult(ctx, location.ID)
	if err != nil {
		t.Fatalf("link pre-scraped jav: %v", err)
	}
	if result.Outcome != javLinkOutcomeExistingLinked {
		t.Fatalf("link outcome = %v, want existing-linked", result.Outcome)
	}
	if lookupCalls != 0 {
		t.Fatalf("provider lookup calls = %d, want 0", lookupCalls)
	}

	var linked models.VideoLocation
	if err := gdb.First(&linked, location.ID).Error; err != nil {
		t.Fatalf("reload linked location: %v", err)
	}
	if linked.JavID == nil || *linked.JavID != preScraped.ID {
		t.Fatalf("location jav id = %#v, want %d", linked.JavID, preScraped.ID)
	}

	var javCount int64
	if err := gdb.Model(&models.Jav{}).Count(&javCount).Error; err != nil {
		t.Fatalf("count jav records: %v", err)
	}
	if javCount != 1 {
		t.Fatalf("jav record count = %d, want 1", javCount)
	}
	var reloadedJav models.Jav
	if err := gdb.First(&reloadedJav, preScraped.ID).Error; err != nil {
		t.Fatalf("reload pre-scraped jav: %v", err)
	}
	if reloadedJav.Title != "Metadata prepared before the file exists" {
		t.Fatalf("pre-scraped metadata was overwritten: title = %q", reloadedJav.Title)
	}
	if reloadedJav.NormalizedCode != "PRE001" {
		t.Fatalf("normalized code = %q, want PRE001", reloadedJav.NormalizedCode)
	}
	imported, importedTotal, err := db.SearchJavWithPrefixFilters(
		ctx, nil, nil, "", "", "code", 20, 0, nil, nil,
		db.JavSearchFilters{StudioID: -1, Inventory: models.JavInventoryImported},
	)
	if err != nil {
		t.Fatalf("list imported JAVs after scan: %v", err)
	}
	if importedTotal != 1 || len(imported) != 1 || imported[0].ID != preScraped.ID ||
		imported[0].AcquisitionStage != models.JavAcquisitionStageImported {
		t.Fatalf("imported work after scan = total %d items %#v", importedTotal, imported)
	}
	var acquisition models.JavAcquisition
	if err := gdb.First(&acquisition, preScraped.ID).Error; err != nil {
		t.Fatalf("load imported acquisition: %v", err)
	}
	if acquisition.Stage != models.JavAcquisitionStageImported {
		t.Fatalf("persisted acquisition stage = %q, want imported", acquisition.Stage)
	}
}

func TestProcessVideoLocationJavLinkStopsAfterExistingJavMediaConflict(t *testing.T) {
	gdb, err := db.Open(filepath.Join(t.TempDir(), "existing-jav-media-conflict.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	previousDB := common.DB
	common.DB = gdb
	t.Cleanup(func() {
		common.DB = previousDB
		if sqlDB, dbErr := gdb.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	previousLookup := lookupJavByCodeForLocationLink
	lookupCalls := 0
	lookupJavByCodeForLocationLink = func(string, jav.Provider) (*jav.JavInfo, error) {
		lookupCalls++
		return &jav.JavInfo{Code: "CON-001", Title: "Provider overwrite"}, nil
	}
	t.Cleanup(func() {
		lookupJavByCodeForLocationLink = previousLookup
	})

	ctx := context.Background()
	javRec, err := db.SaveJavInfo(ctx, &jav.JavInfo{
		Code:     "CON-001",
		Title:    "Canonical metadata",
		Provider: jav.ProviderJavBus,
	})
	if err != nil {
		t.Fatalf("save existing jav: %v", err)
	}
	directories := []models.Directory{{Path: "/tmp/conflict-existing"}, {Path: "/tmp/conflict-incoming"}}
	if err := gdb.Create(&directories).Error; err != nil {
		t.Fatalf("create directories: %v", err)
	}
	videos := []models.Video{
		{Fingerprint: "conflict-existing-media", DurationSec: 3600},
		{Fingerprint: "conflict-different-media", DurationSec: 3600},
	}
	if err := gdb.Create(&videos).Error; err != nil {
		t.Fatalf("create videos: %v", err)
	}
	existingLocation, err := db.UpsertVideoLocation(
		ctx, videos[0].ID, directories[0].ID, "CON-001.mp4", time.Unix(1710000000, 0).UTC(),
	)
	if err != nil {
		t.Fatalf("create existing media location: %v", err)
	}
	if err := db.SetVideoLocationJavIDForVideo(
		ctx, existingLocation.ID, videos[0].ID, javRec.ID, existingLocation.UpdatedAt,
	); err != nil {
		t.Fatalf("link existing media: %v", err)
	}
	incomingLocation, err := db.UpsertVideoLocation(
		ctx, videos[1].ID, directories[1].ID, "incoming/CON-001.mp4", time.Unix(1710000060, 0).UTC(),
	)
	if err != nil {
		t.Fatalf("create incoming media location: %v", err)
	}

	result, err := processVideoLocationJavLinkResult(ctx, incomingLocation.ID)
	if err != nil {
		t.Fatalf("process conflicting media: %v", err)
	}
	if result.Outcome != javLinkOutcomeError {
		t.Fatalf("link outcome = %v, want error", result.Outcome)
	}
	if lookupCalls != 0 {
		t.Fatalf("provider lookup calls after media conflict = %d, want 0", lookupCalls)
	}

	var incoming models.VideoLocation
	if err := gdb.First(&incoming, incomingLocation.ID).Error; err != nil {
		t.Fatalf("reload incoming location: %v", err)
	}
	if incoming.JavID != nil {
		t.Fatalf("conflicting incoming media was linked: %#v", incoming.JavID)
	}
	var storedJav models.Jav
	if err := gdb.First(&storedJav, javRec.ID).Error; err != nil {
		t.Fatalf("reload existing jav: %v", err)
	}
	if storedJav.Title != "Canonical metadata" {
		t.Fatalf("existing metadata was overwritten: title = %q", storedJav.Title)
	}
}

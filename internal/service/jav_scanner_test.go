package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"javboss/internal/common"
	"javboss/internal/db"
	"javboss/internal/jav"
	"javboss/internal/models"
)

func TestJavMetadataFastZhProvidersExcludeSlowProviders(t *testing.T) {
	got := javFastZhMetadataProviders()
	want := []jav.Provider{jav.ProviderJavBus}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("javFastZhMetadataProviders() = %#v, want %#v", got, want)
	}
}

func TestJavMetadataProvidersReuseScannerFallbackOrder(t *testing.T) {
	tests := []struct {
		code string
		want []jav.Provider
	}{
		{code: "GANA-1234", want: []jav.Provider{jav.ProviderJavDB, jav.ProviderJavMenu, jav.ProviderJavBus, jav.ProviderAvsox, jav.ProviderJavDBApp}},
		{code: "AP-001", want: []jav.Provider{jav.ProviderJavDB, jav.ProviderAvmoo, jav.ProviderAvsox, jav.ProviderJavDBApp}},
		{code: "IPX-001", want: []jav.Provider{jav.ProviderJavDB, jav.ProviderJavBus, jav.ProviderAvsox, jav.ProviderJavDBApp}},
		{code: "FC2-1579280", want: []jav.Provider{jav.ProviderJavDB, jav.ProviderJavBus, jav.ProviderAvsox, jav.ProviderJavDBApp}},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			got := javMetadataProvidersForCode(test.code, javFastZhMetadataProviders())
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("providers for %s = %#v, want %#v", test.code, got, test.want)
			}
		})
	}
}

func TestScanMissingJavZhInfoUsesCodeSpecificFallback(t *testing.T) {
	gdb, err := db.Open(filepath.Join(t.TempDir(), "top-down-metadata.db"))
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

	batch, err := db.CreateJavInputBatch(context.Background(), "ap_001")
	if err != nil {
		t.Fatalf("create top-down input: %v", err)
	}
	if batch.AcceptedCount != 1 || len(batch.Items) != 1 || batch.Items[0].JavID == nil {
		t.Fatalf("input batch = %#v", batch)
	}

	previousLookup := lookupJavMetadataByCode
	previousCoverEnqueue := enqueueJavMetadataCover
	var calls []jav.Provider
	var enqueuedCoverCode string
	lookupJavMetadataByCode = func(code string, provider jav.Provider) (*jav.JavInfo, error) {
		if code != "AP-001" {
			t.Fatalf("lookup code = %q, want AP-001", code)
		}
		calls = append(calls, provider)
		if provider == jav.ProviderAvmoo {
			return nil, jav.ResourceNotFonud
		}
		if provider == jav.ProviderAvsox {
			uncensored := true
			return &jav.JavInfo{
				Code:         code,
				Title:        "AVSOX fallback title",
				Actors:       []string{"AVSOX fallback idol"},
				Provider:     provider,
				IsUncensored: &uncensored,
			}, nil
		}
		return nil, jav.ResourceNotFonud
	}
	enqueueJavMetadataCover = func(code string) { enqueuedCoverCode = code }
	t.Cleanup(func() {
		lookupJavMetadataByCode = previousLookup
		enqueueJavMetadataCover = previousCoverEnqueue
	})

	if err := scanMissingJavZhInfo(context.Background(), javFastZhMetadataProviders()); err != nil {
		t.Fatalf("scan missing metadata: %v", err)
	}
	wantCalls := []jav.Provider{jav.ProviderJavDB, jav.ProviderAvmoo, jav.ProviderAvsox}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("provider calls = %#v, want %#v", calls, wantCalls)
	}
	if enqueuedCoverCode != "AP-001" {
		t.Fatalf("enqueued cover code = %q, want AP-001", enqueuedCoverCode)
	}
	var stored models.Jav
	if err := gdb.First(&stored, *batch.Items[0].JavID).Error; err != nil {
		t.Fatalf("load scraped JAV: %v", err)
	}
	if stored.Title != "AVSOX fallback title" || stored.IsUncensored != nil {
		t.Fatalf("scraped JAV = %#v", stored)
	}
	var acquisition models.JavAcquisition
	if err := gdb.First(&acquisition, stored.ID).Error; err != nil {
		t.Fatalf("load acquisition: %v", err)
	}
	if acquisition.Stage != models.JavAcquisitionStageMagnetCollecting {
		t.Fatalf("acquisition stage = %q, want magnet_collecting", acquisition.Stage)
	}
}

func TestScanMissingJavZhInfoAvsoxDoesNotClassifyOrdinaryCode(t *testing.T) {
	gdb, err := db.Open(filepath.Join(t.TempDir(), "avsox-classification.db"))
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

	batch, err := db.CreateJavInputBatch(context.Background(), "IPX-001")
	if err != nil {
		t.Fatalf("create input: %v", err)
	}
	previousLookup := lookupJavMetadataByCode
	previousCoverEnqueue := enqueueJavMetadataCover
	var calls []jav.Provider
	lookupJavMetadataByCode = func(code string, provider jav.Provider) (*jav.JavInfo, error) {
		calls = append(calls, provider)
		if provider != jav.ProviderAvsox {
			return nil, jav.ResourceNotFonud
		}
		uncensored := true
		return &jav.JavInfo{
			Code:         code,
			Title:        "AVSOX title",
			Actors:       []string{"AVSOX idol"},
			IsUncensored: &uncensored,
			Provider:     provider,
		}, nil
	}
	enqueueJavMetadataCover = func(string) {}
	t.Cleanup(func() {
		lookupJavMetadataByCode = previousLookup
		enqueueJavMetadataCover = previousCoverEnqueue
	})

	if err := scanMissingJavZhInfo(context.Background(), javFastZhMetadataProviders()); err != nil {
		t.Fatalf("scan metadata: %v", err)
	}
	wantCalls := []jav.Provider{jav.ProviderJavDB, jav.ProviderJavBus, jav.ProviderAvsox}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("provider calls = %#v, want %#v", calls, wantCalls)
	}
	var stored models.Jav
	if err := gdb.First(&stored, *batch.Items[0].JavID).Error; err != nil {
		t.Fatalf("load jav: %v", err)
	}
	if stored.IsUncensored != nil {
		t.Fatalf("AVSOX generic fallback classified ordinary code: %#v", stored.IsUncensored)
	}
}

func TestScanMissingJavUncensoredContinuesAfterHistoricalBackfill(t *testing.T) {
	gdb, err := db.Open(filepath.Join(t.TempDir(), "uncensored-incremental.db"))
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

	if err := db.UpsertConfig(context.Background(), map[string]string{javUncensoredBackfillDoneConfigKey: "1"}); err != nil {
		t.Fatalf("seed completed backfill flag: %v", err)
	}
	batch, err := db.CreateJavInputBatch(context.Background(), "NEW-UNC-001")
	if err != nil {
		t.Fatalf("create first input: %v", err)
	}
	firstID := *batch.Items[0].JavID

	previousLookup := lookupJavUncensoredByCode
	lookupCalls := 0
	lookupJavUncensoredByCode = func(code string, provider jav.Provider) (*jav.JavInfo, error) {
		lookupCalls++
		if provider != jav.ProviderJavBus || code == "" {
			t.Fatalf("unexpected uncensored lookup: code=%q provider=%s", code, provider.String())
		}
		classified := false
		return &jav.JavInfo{Code: code, IsUncensored: &classified, Provider: provider}, nil
	}
	t.Cleanup(func() { lookupJavUncensoredByCode = previousLookup })

	if err := scanMissingJavUncensoredBackfillOnce(context.Background()); err != nil {
		t.Fatalf("scan first incremental row: %v", err)
	}
	var first models.Jav
	if err := gdb.First(&first, firstID).Error; err != nil {
		t.Fatalf("load first row: %v", err)
	}
	if first.IsUncensored == nil || *first.IsUncensored {
		t.Fatalf("first classification = %#v, want false", first.IsUncensored)
	}

	secondBatch, err := db.CreateJavInputBatch(context.Background(), "NEW-UNC-002")
	if err != nil {
		t.Fatalf("create second input: %v", err)
	}
	if err := scanMissingJavUncensoredBackfillOnce(context.Background()); err != nil {
		t.Fatalf("scan second incremental row: %v", err)
	}
	if lookupCalls != 2 {
		t.Fatalf("uncensored lookup calls = %d, want 2", lookupCalls)
	}
	var second models.Jav
	if err := gdb.First(&second, *secondBatch.Items[0].JavID).Error; err != nil {
		t.Fatalf("load second row: %v", err)
	}
	if second.IsUncensored == nil || *second.IsUncensored {
		t.Fatalf("second classification = %#v, want false", second.IsUncensored)
	}
}

func TestScanMissingJavZhInfoRetriesTitleOnlyWorkForIdols(t *testing.T) {
	gdb, err := db.Open(filepath.Join(t.TempDir(), "missing-idols.db"))
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

	batch, err := db.CreateJavInputBatch(context.Background(), "RETRY-001")
	if err != nil {
		t.Fatalf("create input: %v", err)
	}
	javID := *batch.Items[0].JavID
	if err := gdb.Model(&models.Jav{}).
		Where("id = ?", javID).
		Update("title", "Title arrived without an idol").Error; err != nil {
		t.Fatalf("seed title-only metadata: %v", err)
	}

	previousLookup := lookupJavMetadataByCode
	previousCoverEnqueue := enqueueJavMetadataCover
	lookupJavMetadataByCode = func(code string, provider jav.Provider) (*jav.JavInfo, error) {
		return &jav.JavInfo{
			Code:     code,
			Title:    "A provider title that must not overwrite the existing title",
			Actors:   []string{"待补全女优"},
			Provider: provider,
		}, nil
	}
	enqueueJavMetadataCover = func(string) {}
	t.Cleanup(func() {
		lookupJavMetadataByCode = previousLookup
		enqueueJavMetadataCover = previousCoverEnqueue
	})

	if err := scanMissingJavZhInfo(context.Background(), []jav.Provider{jav.ProviderJavBus}); err != nil {
		t.Fatalf("scan missing idol metadata: %v", err)
	}
	var stored models.Jav
	if err := gdb.Preload("Idols").First(&stored, javID).Error; err != nil {
		t.Fatalf("load updated jav: %v", err)
	}
	if len(stored.Idols) != 1 || stored.Idols[0].Name != "待补全女优" {
		t.Fatalf("idols were not filled: %#v", stored.Idols)
	}
	if stored.Title != "Title arrived without an idol" {
		t.Fatalf("existing title was overwritten: %q", stored.Title)
	}
}

func TestScanMissingJavZhInfoDoesNotReportActorNoopAsUpdated(t *testing.T) {
	gdb, err := db.Open(filepath.Join(t.TempDir(), "missing-idols-race.db"))
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

	batch, err := db.CreateJavInputBatch(context.Background(), "RACE-001")
	if err != nil {
		t.Fatalf("create input: %v", err)
	}
	javID := *batch.Items[0].JavID
	if err := gdb.Model(&models.Jav{}).Where("id = ?", javID).Update("title", "Existing title").Error; err != nil {
		t.Fatalf("seed title: %v", err)
	}

	previousLookup := lookupJavMetadataByCode
	previousCoverEnqueue := enqueueJavMetadataCover
	coverEnqueues := 0
	lookupJavMetadataByCode = func(code string, provider jav.Provider) (*jav.JavInfo, error) {
		// Simulate a concurrent writer attaching the actor after the scanner
		// candidate list was read.  The append helper must report a no-op.
		idol := models.JavIdol{Name: "Concurrent idol"}
		if err := gdb.FirstOrCreate(&idol, models.JavIdol{Name: idol.Name}).Error; err != nil {
			t.Fatalf("create concurrent idol: %v", err)
		}
		if err := gdb.FirstOrCreate(&models.JavIdolMap{JavID: javID, JavIdolID: idol.ID}, models.JavIdolMap{JavID: javID, JavIdolID: idol.ID}).Error; err != nil {
			t.Fatalf("create concurrent idol map: %v", err)
		}
		return &jav.JavInfo{Code: code, Actors: []string{"Concurrent idol"}, Provider: provider}, nil
	}
	enqueueJavMetadataCover = func(string) { coverEnqueues++ }
	t.Cleanup(func() {
		lookupJavMetadataByCode = previousLookup
		enqueueJavMetadataCover = previousCoverEnqueue
	})

	if err := scanMissingJavZhInfo(context.Background(), []jav.Provider{jav.ProviderJavBus}); err != nil {
		t.Fatalf("scan missing actor metadata: %v", err)
	}
	if coverEnqueues != 0 {
		t.Fatalf("cover enqueue count = %d, want 0 for actor append no-op", coverEnqueues)
	}
}

func TestScanMissingJavZhInfoContinuesUntilTitleAndIdolsAreFilled(t *testing.T) {
	gdb, err := db.Open(filepath.Join(t.TempDir(), "split-primary-metadata.db"))
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

	batch, err := db.CreateJavInputBatch(context.Background(), "IPX-991")
	if err != nil {
		t.Fatalf("create input: %v", err)
	}
	javID := *batch.Items[0].JavID

	previousLookup := lookupJavMetadataByCode
	previousCoverEnqueue := enqueueJavMetadataCover
	var calls []jav.Provider
	lookupJavMetadataByCode = func(code string, provider jav.Provider) (*jav.JavInfo, error) {
		calls = append(calls, provider)
		switch provider {
		case jav.ProviderJavBus:
			return &jav.JavInfo{Code: code, Title: "Split title", Provider: provider}, nil
		case jav.ProviderAvsox:
			return &jav.JavInfo{Code: "WRONG-999", Actors: []string{"Wrong idol"}, Provider: provider}, nil
		case jav.ProviderJavDBApp:
			return &jav.JavInfo{Code: code, Actors: []string{"Split idol"}, Provider: provider}, nil
		default:
			return nil, jav.ResourceNotFonud
		}
	}
	enqueueJavMetadataCover = func(string) {}
	t.Cleanup(func() {
		lookupJavMetadataByCode = previousLookup
		enqueueJavMetadataCover = previousCoverEnqueue
	})

	if err := scanMissingJavZhInfo(context.Background(), []jav.Provider{jav.ProviderJavBus}); err != nil {
		t.Fatalf("scan split metadata: %v", err)
	}
	wantCalls := []jav.Provider{jav.ProviderJavDB, jav.ProviderJavBus, jav.ProviderAvsox, jav.ProviderJavDBApp}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("provider calls = %#v, want %#v", calls, wantCalls)
	}
	var stored models.Jav
	if err := gdb.Preload("Idols").First(&stored, javID).Error; err != nil {
		t.Fatalf("load split metadata: %v", err)
	}
	if stored.Title != "Split title" || len(stored.Idols) != 1 || stored.Idols[0].Name != "Split idol" {
		t.Fatalf("split metadata was not completed: %#v", stored)
	}
}

func TestScanJavSeriesMetadataProviderRoundFallsBackToJavMenu(t *testing.T) {
	var avmooNoUpdateRounds atomic.Uint32
	avmooUpdates := []int64{0, 0, 3, 0, 0}
	avmooIndex := 0
	var calls []string
	avmooScan := func(context.Context) (int64, error) {
		calls = append(calls, "avmoo")
		updated := avmooUpdates[avmooIndex]
		avmooIndex++
		return updated, nil
	}
	javMenuScan := func(context.Context) (int64, error) {
		calls = append(calls, "javmenu")
		return 1, nil
	}

	for range 7 {
		if err := scanJavSeriesMetadataProviderRound(
			context.Background(),
			&avmooNoUpdateRounds,
			avmooScan,
			javMenuScan,
		); err != nil {
			t.Fatalf("scan provider round: %v", err)
		}
	}

	want := []string{"avmoo", "avmoo", "javmenu", "avmoo", "avmoo", "avmoo", "javmenu"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("provider calls = %#v, want %#v", calls, want)
	}
	if got := avmooNoUpdateRounds.Load(); got != 0 {
		t.Fatalf("avmoo no-update rounds = %d, want 0 after JavMenu fallback", got)
	}
}

func TestScanMissingJavLocalSeriesUsesOnlyJavMenu(t *testing.T) {
	gdb, err := db.Open(filepath.Join(t.TempDir(), "missing-series.db"))
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

	now := time.Unix(1710000000, 0).UTC()
	series := []models.JavSeries{
		{Name: "Existing Local Series"},
		{Name: "English Hint", IsEnglish: true},
	}
	if err := gdb.Create(&series).Error; err != nil {
		t.Fatalf("create series: %v", err)
	}
	localSeries := series[0]
	englishSeries := series[1]
	uncensored := true
	rows := []models.Jav{
		{Code: "NO-HINT-001", FetchedAt: now, CreatedAt: now},
		{Code: "EN-HINT-001", SeriesEnID: &englishSeries.ID, FetchedAt: now.Add(time.Second), CreatedAt: now.Add(time.Second)},
		{Code: "EXISTING-001", SeriesID: &localSeries.ID, FetchedAt: now.Add(2 * time.Second), CreatedAt: now.Add(2 * time.Second)},
		{Code: "UNCENSORED-001", IsUncensored: &uncensored, FetchedAt: now.Add(3 * time.Second), CreatedAt: now.Add(3 * time.Second)},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("create jav rows: %v", err)
	}

	cache := &javScannerLookupCache{values: map[string]jav.JavInfo{
		"v2:jav:javmenu:lookup_jav:NO-HINT-001": {Series: "JavMenu Local Series"},
		"v2:jav:javmenu:lookup_jav:EN-HINT-001": {},
	}}
	jav.SetCache(cache)
	t.Cleanup(func() {
		jav.SetCache(nil)
	})

	updated, err := scanMissingJavLocalSeriesWithJavMenu(context.Background())
	if err != nil {
		t.Fatalf("scan missing local series: %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated series = %d, want 1", updated)
	}

	wantKeys := []string{
		"v2:jav:javmenu:lookup_jav:NO-HINT-001",
		"v2:jav:javmenu:lookup_jav:EN-HINT-001",
	}
	sort.Strings(cache.keys)
	sort.Strings(wantKeys)
	if !reflect.DeepEqual(cache.keys, wantKeys) {
		t.Fatalf("lookup cache keys = %#v, want %#v", cache.keys, wantKeys)
	}

	var filled models.Jav
	if err := gdb.Preload("Series").Where("code = ?", "NO-HINT-001").First(&filled).Error; err != nil {
		t.Fatalf("load filled jav: %v", err)
	}
	if filled.Series == nil || filled.Series.Name != "JavMenu Local Series" {
		t.Fatalf("unexpected filled series: %#v", filled.Series)
	}

	var empty models.Jav
	if err := gdb.Where("code = ?", "EN-HINT-001").First(&empty).Error; err != nil {
		t.Fatalf("load empty-series jav: %v", err)
	}
	if empty.SeriesID != nil {
		t.Fatalf("empty JavMenu series should not be persisted: %#v", empty.SeriesID)
	}
}

func TestScanJavSeriesMetadataDoesNotStarveRowsWithoutEnglishHint(t *testing.T) {
	gdb, err := db.Open(filepath.Join(t.TempDir(), "missing-series-no-hint.db"))
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

	previousNoUpdateRounds := javSeriesAvmooNoUpdateRounds.Load()
	javSeriesAvmooNoUpdateRounds.Store(0)
	t.Cleanup(func() { javSeriesAvmooNoUpdateRounds.Store(previousNoUpdateRounds) })
	row := models.Jav{Code: "WAAA-407", Title: "No English hint", FetchedAt: time.Now().UTC()}
	if err := gdb.Create(&row).Error; err != nil {
		t.Fatalf("create no-hint jav: %v", err)
	}
	cache := &javScannerLookupCache{values: map[string]jav.JavInfo{
		"v2:jav:javmenu:lookup_jav:WAAA-407": {Series: "WAAA local series"},
	}}
	jav.SetCache(cache)
	t.Cleanup(func() { jav.SetCache(nil) })

	if err := ScanJavSeriesMetadata(context.Background()); err != nil {
		t.Fatalf("scan series metadata: %v", err)
	}
	var stored models.Jav
	if err := gdb.Preload("Series").First(&stored, row.ID).Error; err != nil {
		t.Fatalf("load no-hint jav: %v", err)
	}
	if stored.Series == nil || stored.Series.Name != "WAAA local series" {
		t.Fatalf("no-hint series was not filled: %#v", stored.Series)
	}
	if len(cache.keys) == 0 || cache.keys[0] != "v2:jav:javmenu:lookup_jav:WAAA-407" {
		t.Fatalf("first lookup key = %#v, want JavMenu WAAA-407", cache.keys)
	}
}

type javScannerLookupCache struct {
	values map[string]jav.JavInfo
	keys   []string
}

func (c *javScannerLookupCache) Get(key string, _ time.Time) ([]byte, bool, error) {
	c.keys = append(c.keys, key)
	info, ok := c.values[key]
	if !ok {
		return nil, false, nil
	}
	raw, err := json.Marshal(struct {
		Status string      `json:"status"`
		Data   jav.JavInfo `json:"data"`
	}{
		Status: "hit",
		Data:   info,
	})
	return raw, true, err
}

func (c *javScannerLookupCache) Set(string, []byte, time.Time) error {
	return nil
}

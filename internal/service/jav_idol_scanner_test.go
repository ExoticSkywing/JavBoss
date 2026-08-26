package service

import (
	"fmt"
	"testing"
	"time"

	"javboss/internal/jav"
	"javboss/internal/models"
)

func TestLookupActressProfilesConcurrently(t *testing.T) {
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	lookups := make([]idolActressLookup, 3)
	for index := range lookups {
		index := index
		lookups[index] = func() (*jav.ActressInfo, error) {
			started <- struct{}{}
			<-release
			return &jav.ActressInfo{JapaneseName: fmt.Sprintf("actress-%d", index)}, nil
		}
	}

	resultChannel := make(chan []idolActressLookupResult, 1)
	go func() {
		resultChannel <- lookupActressProfilesConcurrently(lookups...)
	}()

	for index := 0; index < len(lookups); index++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("lookup %d did not start concurrently", index)
		}
	}
	close(release)

	results := <-resultChannel
	for index, result := range results {
		if result.err != nil {
			t.Fatalf("lookup %d: %v", index, result.err)
		}
		wantName := fmt.Sprintf("actress-%d", index)
		if result.info == nil || result.info.JapaneseName != wantName {
			t.Fatalf("lookup %d result = %#v, want %s", index, result.info, wantName)
		}
	}
}

func TestMergeActressInfosUsesProviderPriority(t *testing.T) {
	minnanoAVInfo := &jav.ActressInfo{
		RomanName:    "Minnano Roman",
		JapaneseName: "みんなの名前",
		HeightCM:     160,
	}
	javDatabaseInfo := &jav.ActressInfo{
		RomanName:   "JavDatabase Roman",
		ChineseName: "数据库中文名",
		HeightCM:    170,
		Bust:        88,
	}
	javModelInfo := &jav.ActressInfo{
		RomanName:   "JavModel Roman",
		ChineseName: "模型中文名",
		Bust:        90,
		Cup:         5,
	}

	info := mergeActressInfosByPriority(minnanoAVInfo, javDatabaseInfo, javModelInfo)
	if info == nil {
		t.Fatal("mergeActressInfosByPriority returned nil")
	}
	if info.RomanName != "Minnano Roman" || info.JapaneseName != "みんなの名前" || info.HeightCM != 160 {
		t.Fatalf("minnanoav priority fields were replaced: %#v", info)
	}
	if info.ChineseName != "数据库中文名" || info.Bust != 88 {
		t.Fatalf("javdatabase fallback fields were not used: %#v", info)
	}
	if info.Cup != 5 {
		t.Fatalf("javmodel fallback field was not used: %#v", info)
	}
}

func TestIdolChineseNameRetryUsesTTL(t *testing.T) {
	resetIdolProfileRetryState()
	t.Cleanup(resetIdolProfileRetryState)

	base := time.Unix(1710000000, 0).UTC()
	height, bust, waist, hips, cup := 160, 88, 60, 89, 5
	birthDate := base.Add(-25 * 365 * 24 * time.Hour)
	idol := models.JavIdol{
		ID:           101,
		Name:         "TTL Idol",
		RomanName:    "TTL Idol",
		JapaneseName: "TTL アイドル",
		HeightCM:     &height,
		BirthDate:    &birthDate,
		Bust:         &bust,
		Waist:        &waist,
		Hips:         &hips,
		Cup:          &cup,
	}
	if idolMissingOnlyChineseName(idol) != true {
		t.Fatal("fixture should be missing only ChineseName")
	}
	if shouldSkipIdolProfileAttempt(idol, base) {
		t.Fatal("first ChineseName attempt was unexpectedly skipped")
	}
	if !shouldSkipIdolProfileAttempt(idol, base.Add(time.Hour)) {
		t.Fatal("retry inside TTL was not skipped")
	}
	if shouldSkipIdolProfileAttempt(idol, base.Add(idolChineseNameRetryTTL+time.Second)) {
		t.Fatal("retry after TTL was skipped")
	}

	// A profile missing another field must remain eligible on every pass.
	idol.ChineseName = ""
	idol.HeightCM = nil
	if shouldSkipIdolProfileAttempt(idol, base) {
		t.Fatal("non-Chinese-only profile was throttled")
	}
}

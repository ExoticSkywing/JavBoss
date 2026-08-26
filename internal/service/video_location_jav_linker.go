package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"

	"javboss/internal/common"
	"javboss/internal/common/logging"
	"javboss/internal/db"
	"javboss/internal/jav"
	"javboss/internal/models"
	"javboss/internal/util"
)

const (
	javLinkWorkerCount = 4 // 增加worker数可能会导致首次扫描目录时jav相关查询接口严重阻塞
	javLinkQueueSize   = 4096
)

var lookupJavByCodeForLocationLink = jav.LookupJavByCode

type javLinkBatch struct {
	ctx     context.Context
	tasks   chan int64
	seen    map[int64]struct{}
	mu      sync.Mutex
	summary JavLinkSummary
	closed  bool
	workers sync.WaitGroup
}

// JavLinkSummary describes how the JAV association stage handled every video
// queued by one directory scan.
type JavLinkSummary struct {
	Processed       int
	AlreadyLinked   int
	ExistingLinked  int
	Scraped         int
	JavDBAppScraped int
	Skipped         int
	NoCode          int
	NotFound        int
	Errors          int
}

type javLinkOutcome int

const (
	javLinkOutcomeUnknown javLinkOutcome = iota
	javLinkOutcomeAlreadyLinked
	javLinkOutcomeExistingLinked
	javLinkOutcomeScraped
	javLinkOutcomeSkipped
	javLinkOutcomeNoCode
	javLinkOutcomeNotFound
	javLinkOutcomeError
)

type javLinkResult struct {
	Outcome  javLinkOutcome
	Provider jav.Provider
}

func newJavLinkBatch(ctx context.Context) *javLinkBatch {
	if ctx == nil {
		ctx = context.Background()
	}
	batch := &javLinkBatch{
		ctx:   ctx,
		tasks: make(chan int64, javLinkQueueSize),
		seen:  make(map[int64]struct{}),
	}
	for i := 0; i < javLinkWorkerCount; i++ {
		batch.workers.Add(1)
		go batch.worker()
	}
	return batch
}

func (b *javLinkBatch) Enqueue(locationID int64) {
	if b == nil || locationID <= 0 {
		return
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	if _, ok := b.seen[locationID]; ok {
		b.mu.Unlock()
		return
	}
	b.seen[locationID] = struct{}{}
	b.mu.Unlock()

	select {
	case b.tasks <- locationID:
	case <-b.ctx.Done():
	}
}

func (b *javLinkBatch) Wait() {
	if b == nil {
		return
	}

	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.tasks)
	}
	b.mu.Unlock()
	b.workers.Wait()
}

func (b *javLinkBatch) Summary() JavLinkSummary {
	if b == nil {
		return JavLinkSummary{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.summary
}

func (b *javLinkBatch) record(result javLinkResult) {
	if b == nil || result.Outcome == javLinkOutcomeUnknown {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.summary.Processed++
	switch result.Outcome {
	case javLinkOutcomeAlreadyLinked:
		b.summary.AlreadyLinked++
	case javLinkOutcomeExistingLinked:
		b.summary.ExistingLinked++
	case javLinkOutcomeScraped:
		b.summary.Scraped++
		if result.Provider == jav.ProviderJavDBApp {
			b.summary.JavDBAppScraped++
		}
	case javLinkOutcomeSkipped:
		b.summary.Skipped++
	case javLinkOutcomeNoCode:
		b.summary.NoCode++
	case javLinkOutcomeNotFound:
		b.summary.NotFound++
	case javLinkOutcomeError:
		b.summary.Errors++
	}
}

func (b *javLinkBatch) worker() {
	defer b.workers.Done()
	for locationID := range b.tasks {
		if err := b.ctx.Err(); err != nil {
			return
		}
		result, err := processVideoLocationJavLinkResult(b.ctx, locationID)
		b.record(result)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			logging.Error("video location jav link failed location=%d err=%v", locationID, err)
		}
	}
}

func finishJavLinkBatch(batch *javLinkBatch) {
	batch.Wait()
}

func processVideoLocationJavLink(ctx context.Context, locationID int64) error {
	_, err := processVideoLocationJavLinkResult(ctx, locationID)
	return err
}

func processVideoLocationJavLinkResult(ctx context.Context, locationID int64) (javLinkResult, error) {
	v, err := db.GetVideoForJavScan(ctx, locationID)
	if err != nil {
		return javLinkResult{Outcome: javLinkOutcomeError}, err
	}
	if v == nil {
		return javLinkResult{}, nil
	}

	override := normalizeJavScrapeOverride(v.JavScrapeOverride)
	if override == models.JavScrapeOverrideSkip {
		return javLinkResult{Outcome: javLinkOutcomeSkipped}, nil
	}

	filename := filepath.Base(filepath.FromSlash(v.Filename))
	forcedCode := forcedJavScrapeCode(override)
	if forcedCode == "" {
		if v.JavID != nil {
			return javLinkResult{Outcome: javLinkOutcomeAlreadyLinked}, nil
		}
		if v.DurationSec > 0 && v.DurationSec < 900 {
			return javLinkResult{Outcome: javLinkOutcomeSkipped}, nil
		}
	} else if v.JavID != nil {
		if javCodesEquivalent(v.JavCode, forcedCode) {
			return javLinkResult{Outcome: javLinkOutcomeAlreadyLinked}, nil
		}
		if err := db.ClearVideoLocationJavIDForVideo(ctx, v.LocationID, v.VideoID, v.UpdatedAt); err != nil {
			logging.Error("clear video location jav before forced scrape failed location=%d code=%s err=%v", v.LocationID, forcedCode, err)
			return javLinkResult{Outcome: javLinkOutcomeError}, err
		}
		v.JavID = nil
		v.JavCode = ""
	}

	possibleCodes := javScrapeCodesForVideo(filename, forcedCode)
	if len(possibleCodes) == 0 {
		return javLinkResult{Outcome: javLinkOutcomeNoCode}, nil
	}

	if linked, hadError := linkExistingJav(ctx, v, possibleCodes); linked {
		return javLinkResult{Outcome: javLinkOutcomeExistingLinked}, nil
	} else if hadError {
		return javLinkResult{Outcome: javLinkOutcomeError}, nil
	}

	hadLookupError := false
	for _, code := range possibleCodes {
		for _, provider := range javLinkProvidersForCode(code) {
			linked, lookupError := lookupAndLinkVideoLocationJav(ctx, v, filename, []string{code}, provider)
			hadLookupError = hadLookupError || lookupError
			if linked {
				return javLinkResult{Outcome: javLinkOutcomeScraped, Provider: provider}, nil
			}
		}
	}

	uncensoredPossibleCodes := util.ExtractUncensoredCodesFromName(filename)
	if forcedCode != "" {
		uncensoredPossibleCodes = possibleCodes
	}
	linked, lookupError := lookupAndLinkVideoLocationJav(ctx, v, filename, uncensoredPossibleCodes, jav.ProviderAvsox)
	hadLookupError = hadLookupError || lookupError
	if linked {
		return javLinkResult{Outcome: javLinkOutcomeScraped, Provider: jav.ProviderAvsox}, nil
	}
	linked, lookupError = lookupAndLinkVideoLocationJav(ctx, v, filename, possibleCodes, jav.ProviderJavDBApp)
	hadLookupError = hadLookupError || lookupError
	if linked {
		return javLinkResult{Outcome: javLinkOutcomeScraped, Provider: jav.ProviderJavDBApp}, nil
	}
	if hadLookupError {
		return javLinkResult{Outcome: javLinkOutcomeError}, nil
	}
	return javLinkResult{Outcome: javLinkOutcomeNotFound}, nil
}

// javCodesEquivalent compares display codes by the same canonical identity
// used by the database.  A forced scrape may use a provider alias such as
// FC2-PPV-123 while the linked Jav stores FC2-123; these must not trigger an
// unnecessary unlink/relink cycle.  Keep the direct case-insensitive check as
// a compatibility fallback for legacy display values that cannot be
// normalized.
func javCodesEquivalent(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if strings.EqualFold(left, right) {
		return true
	}
	leftNormalized := models.NormalizeJavCode(left)
	rightNormalized := models.NormalizeJavCode(right)
	return leftNormalized != "" && leftNormalized == rightNormalized
}

func linkExistingJav(ctx context.Context, v *db.JavScanVideo, possibleCodes []string) (bool, bool) {
	hadError := false
	for _, code := range possibleCodes {
		existJav, err := db.GetJavByCode(ctx, code)
		if err != nil {
			logging.Error("jav lookup existing failed location=%d code=%s err=%v", v.LocationID, code, err)
			hadError = true
			continue
		}
		if existJav == nil {
			continue
		}
		if err := db.SetVideoLocationJavIDForVideo(ctx, v.LocationID, v.VideoID, existJav.ID, v.UpdatedAt); err != nil {
			logging.Error("set video location jav failed location=%d code=%s err=%v", v.LocationID, code, err)
			return false, true
		} else {
			enqueueCover(existJav.Code)
		}
		return true, hadError
	}
	return false, hadError
}

func javScrapeCodesForVideo(filename, forcedCode string) []string {
	forcedCode = strings.TrimSpace(forcedCode)
	if forcedCode != "" {
		return []string{forcedCode}
	}
	return util.ExtractCodeFromName(filename)
}

func javLinkProvidersForCode(code string) []jav.Provider {
	code = strings.ToUpper(strings.TrimSpace(code))
	switch {
	case strings.HasPrefix(code, "GANA-"):
		return []jav.Provider{jav.ProviderJavMenu, jav.ProviderJavBus}
	case strings.HasPrefix(code, "STARS-"):
		return []jav.Provider{jav.ProviderJavBus, jav.ProviderAvmoo}
	case strings.HasPrefix(code, "AP-"):
		return []jav.Provider{jav.ProviderAvmoo}
	default:
		return []jav.Provider{jav.ProviderJavBus}
	}
}

func normalizeJavScrapeOverride(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, models.JavScrapeOverrideSkip) {
		return models.JavScrapeOverrideSkip
	}
	if strings.HasPrefix(strings.ToLower(raw), models.JavScrapeOverrideManualPrefix) {
		code := strings.TrimSpace(raw[len(models.JavScrapeOverrideManualPrefix):])
		if code == "" {
			return ""
		}
		return models.JavScrapeOverrideManualPrefix + strings.ToUpper(code)
	}
	return strings.ToUpper(raw)
}

func forcedJavScrapeCode(override string) string {
	override = normalizeJavScrapeOverride(override)
	if override == "" || override == models.JavScrapeOverrideSkip {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(override), models.JavScrapeOverrideManualPrefix) {
		return strings.TrimSpace(override[len(models.JavScrapeOverrideManualPrefix):])
	}
	return override
}

func lookupAndLinkVideoLocationJav(ctx context.Context, v *db.JavScanVideo, filename string, possibleCodes []string, provider jav.Provider) (bool, bool) {
	hadError := false
	for _, code := range possibleCodes {
		info, err := lookupJavByCodeForLocationLink(code, provider)
		if err != nil {
			if errors.Is(err, jav.ResourceNotFonud) {
				continue
			}
			logging.Error("jav lookup failed provider=%s location=%s code=%s err=%v", provider.String(), filename, code, err)
			hadError = true
			continue
		}
		if info == nil {
			continue
		}

		if _, err := db.SaveJavInfoAndLinkLocationForVideo(ctx, info, v.LocationID, v.VideoID, v.UpdatedAt); err != nil {
			logging.Error("link video location->jav failed provider=%s location=%s code=%s err=%v", provider.String(), filename, info.Code, err)
			return false, true
		} else {
			logging.Info("link video location->jav success provider=%s location=%s code=%s", provider.String(), filename, info.Code)
			enqueueCover(info.Code)
		}
		return true, hadError
	}
	return false, hadError
}

func enqueueCover(code string) {
	mgr := common.CoverManager
	if mgr == nil {
		return
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return
	}
	mgr.Enqueue(code)
}

// enqueueMissingCoversForDirectory 只补充指定目录中已关联 JAV 的缺失封面。
func enqueueMissingCoversForDirectory(ctx context.Context, directoryID int64) error {
	mgr := common.CoverManager
	if common.DB == nil || mgr == nil {
		return nil
	}
	codes, err := db.ListJavCodesForDirectory(ctx, directoryID)
	if err != nil {
		return err
	}
	for _, c := range codes {
		code := strings.TrimSpace(c)
		if code == "" {
			continue
		}
		if mgr.Exists(code) {
			continue
		}
		mgr.Enqueue(code)
	}
	return nil
}

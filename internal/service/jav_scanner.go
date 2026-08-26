package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"time"

	"javboss/internal/common"
	"javboss/internal/common/logging"
	"javboss/internal/db"
	"javboss/internal/jav"
	"javboss/internal/models"
	"javboss/internal/util"
)

const javUncensoredBackfillDoneConfigKey = "jav_uncensored_backfill_done"
const javDBIdolReconciliationBatchSize = 10

var javSeriesAvmooNoUpdateRounds atomic.Uint32
var javMetadataScanRequests = make(chan struct{}, 1)
var lookupJavMetadataByCode = jav.LookupJavByCode
var lookupJavUncensoredByCode = jav.LookupJavByCode
var enqueueJavMetadataCover = enqueueCover

type periodicScanFunc func(context.Context) error
type localSeriesScanFunc func(context.Context) (int64, error)

// StartJavMetadataScanner periodically fills missing JAV metadata using the fast providers.
func StartJavMetadataScanner(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if err := ScanJavMetadata(ctx); err != nil {
				logging.Error("jav metadata scan failed: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-javMetadataScanRequests:
			}
		}
	}()
}

// RequestJavMetadataScan coalesces input bursts into an immediate background
// metadata pass. It never blocks the local transaction that creates Jav rows.
func RequestJavMetadataScan() {
	select {
	case javMetadataScanRequests <- struct{}{}:
	default:
	}
}

// StartUncensoredJavMetadataScanner periodically fills uncensored metadata through AVSOX.
func StartUncensoredJavMetadataScanner(ctx context.Context, interval time.Duration) {
	startPeriodicScanner(ctx, interval, "uncensored jav metadata", ScanUncensoredJavMetadata)
}

// StartJavSeriesMetadataScanner periodically runs the non-uncensored series pipeline.
func StartJavSeriesMetadataScanner(ctx context.Context, interval time.Duration) {
	startPeriodicScanner(ctx, interval, "jav series metadata", ScanJavSeriesMetadata)
}

func startPeriodicScanner(ctx context.Context, interval time.Duration, name string, scan periodicScanFunc) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if err := scan(ctx); err != nil {
				logging.Error("%s scan failed: %v", name, err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// ScanJavMetadata fills general metadata plus JavDatabase studio and internal
// English-series hints. Frontend-visible series remain in the dedicated scanner.
func ScanJavMetadata(ctx context.Context) error {
	if common.DB == nil {
		return errors.New("nil db")
	}

	if err := scanMissingJavZhInfo(ctx, javFastZhMetadataProviders()); err != nil {
		return err
	}
	if err := scanMissingJavUncensoredBackfillOnce(ctx); err != nil {
		return err
	}
	if err := scanMissingJavStudioAndEnglishSeries(ctx); err != nil {
		return err
	}
	if err := scanPendingJavDBIdolIdentities(ctx); err != nil {
		return err
	}
	return nil
}

func scanPendingJavDBIdolIdentities(ctx context.Context) error {
	items, err := db.ListJavsPendingJavDBIdolReconciliation(ctx, javDBIdolReconciliationBatchSize)
	if err != nil {
		return err
	}
	if len(items) > 0 {
		logging.Info("reconciling %d JAV actress identities with JavDB", len(items))
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		code := strings.TrimSpace(item.Code)
		if code == "" {
			continue
		}
		info, err := lookupJavMetadataByCode(code, jav.ProviderJavDB)
		if err != nil {
			if errors.Is(err, jav.ResourceNotFonud) {
				if markErr := db.MarkJavDBIdolsReconciled(ctx, item.ID, code); markErr != nil {
					logging.Error("mark missing JavDB actress metadata id=%d code=%s err=%v", item.ID, code, markErr)
				}
				continue
			}
			logging.Error("lookup JavDB actress identities failed id=%d code=%s err=%v", item.ID, code, err)
			continue
		}
		if info == nil {
			continue
		}
		if responseCode := models.NormalizeJavCode(info.Code); responseCode != "" && responseCode != models.NormalizeJavCode(code) {
			logging.Error("ignore mismatched JavDB actress identities id=%d requested=%s response=%s", item.ID, code, strings.TrimSpace(info.Code))
			continue
		}
		if len(info.Actors) == 0 {
			if err := db.MarkJavDBIdolsReconciled(ctx, item.ID, code); err != nil {
				logging.Error("mark empty JavDB actress metadata id=%d code=%s err=%v", item.ID, code, err)
			}
			continue
		}
		info.Provider = jav.ProviderJavDB
		updated, err := db.ReconcileJavDBIdols(ctx, item.ID, code, info)
		if err != nil {
			logging.Error("reconcile JavDB actress identities failed id=%d code=%s err=%v", item.ID, code, err)
			continue
		}
		if updated {
			logging.Info("reconciled JavDB actress identities id=%d code=%s actors=%d", item.ID, code, len(info.Actors))
		}
	}
	return nil
}

func scanMissingJavStudioAndEnglishSeries(ctx context.Context) error {
	items, err := db.ListJavsMissingStudioOrEnglishSeries(ctx)
	if err != nil {
		return err
	}
	shuffleJavMetadataScanItems(items)
	for _, item := range items {
		info, code, ok, err := lookupJavDatabaseMetadata(ctx, item)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		studio := ""
		seriesEn := ""
		if info != nil {
			studio = strings.TrimSpace(info.Studio)
			seriesEn = strings.TrimSpace(info.Series)
		}
		if item.StudioID == nil && studio != "" {
			if updated, err := db.UpdateJavStudioIfMissing(ctx, item.ID, studio); err != nil {
				logging.Error("update jav studio failed id=%d code=%s err=%v", item.ID, code, err)
			} else if updated {
				logging.Info("jav studio updated id=%d code=%s studio=%s", item.ID, code, studio)
			}
		}
		if item.SeriesEnID == nil && seriesEn != "" {
			if updated, err := db.UpdateJavEnglishSeriesIfMissing(ctx, item.ID, seriesEn); err != nil {
				logging.Error("update jav internal english series failed id=%d code=%s err=%v", item.ID, code, err)
			} else if updated {
				logging.Info("jav internal english series updated id=%d code=%s series=%s", item.ID, code, seriesEn)
			}
		}
	}
	return nil
}

func lookupJavDatabaseMetadata(ctx context.Context, item db.JavMetadataScanItem) (*jav.JavInfo, string, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", false, err
	}
	code := strings.TrimSpace(item.Code)
	if code == "" {
		return nil, "", false, nil
	}

	info, err := jav.LookupJavByCode(code, jav.ProviderJavDatabase)
	if err != nil {
		if !errors.Is(err, jav.ResourceNotFonud) {
			logging.Error("lookup javdatabase metadata failed id=%d code=%s err=%v", item.ID, code, err)
		}
		return nil, code, false, nil
	}
	return info, code, true, nil
}

func javFastZhMetadataProviders() []jav.Provider {
	return []jav.Provider{jav.ProviderJavBus}
}

// ScanJavSeriesMetadata normally runs Avmoo. After two consecutive Avmoo rounds
// without updates, the next round runs JavMenu instead and resets the fallback state.
func ScanJavSeriesMetadata(ctx context.Context) error {
	if common.DB == nil {
		return errors.New("nil db")
	}
	// Rows without an internal English-series hint are not candidates for the
	// Avmoo pass.  Run the JavMenu pass for those rows on ordinary rounds so
	// they cannot be starved forever by unrelated Avmoo updates.  When the
	// normal Avmoo fallback is due, let that pass handle the full set once;
	// otherwise the no-hint rows would be queried twice in one round.
	fallbackDue := javSeriesAvmooNoUpdateRounds.Load() >= 2
	if !fallbackDue {
		if _, err := scanMissingJavLocalSeriesWithoutEnglishHintWithJavMenu(ctx); err != nil {
			return err
		}
	}
	if err := scanJavSeriesMetadataProviderRound(
		ctx,
		&javSeriesAvmooNoUpdateRounds,
		scanMissingJavLocalSeriesWithAvmoo,
		scanMissingJavLocalSeriesWithJavMenu,
	); err != nil {
		return err
	}
	updated, err := db.UpdateMissingJavSeriesStudios(ctx)
	if err != nil {
		return err
	}
	if updated > 0 {
		logging.Info("updated %d jav series studio ids", updated)
	}
	return nil
}

func scanJavSeriesMetadataProviderRound(
	ctx context.Context,
	avmooNoUpdateRounds *atomic.Uint32,
	avmooScan localSeriesScanFunc,
	javMenuScan localSeriesScanFunc,
) error {
	if avmooNoUpdateRounds == nil || avmooScan == nil || javMenuScan == nil {
		return errors.New("invalid jav series scanner state")
	}
	if avmooNoUpdateRounds.Load() >= 2 {
		logging.Info("starting javmenu series scan after two avmoo rounds without updates")
		if _, err := javMenuScan(ctx); err != nil {
			return err
		}
		avmooNoUpdateRounds.Store(0)
		return nil
	}

	updated, err := avmooScan(ctx)
	if err != nil {
		return err
	}
	if updated > 0 {
		avmooNoUpdateRounds.Store(0)
	} else {
		avmooNoUpdateRounds.Add(1)
	}
	return nil
}

func scanMissingJavLocalSeriesWithJavMenu(ctx context.Context) (int64, error) {
	items, err := db.ListJavsMissingLocalSeries(ctx)
	if err != nil {
		return 0, err
	}
	var updatedCount int64
	logging.Info("found %d javs missing local series for javmenu", len(items))
	shuffleJavMetadataScanItems(items)
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return updatedCount, err
		}

		code := strings.TrimSpace(item.Code)
		if code == "" {
			continue
		}
		info, err := jav.LookupJavByCode(code, jav.ProviderJavMenu)
		if err != nil {
			if !errors.Is(err, jav.ResourceNotFonud) {
				logging.Error("lookup javmenu series failed id=%d code=%s err=%v", item.ID, code, err)
			}
			continue
		}
		series := ""
		if info != nil {
			series = strings.TrimSpace(info.Series)
		}
		if series == "" {
			continue
		}
		if updated, err := db.UpdateJavSeriesIfMissing(ctx, item.ID, series); err != nil {
			logging.Error("update javmenu local series failed id=%d code=%s err=%v", item.ID, code, err)
		} else if updated {
			updatedCount++
			logging.Info("jav local series updated provider=%s id=%d code=%s series=%s", jav.ProviderJavMenu.String(), item.ID, code, series)
		}
	}
	return updatedCount, nil
}

// scanMissingJavLocalSeriesWithoutEnglishHintWithJavMenu handles works that
// have no JavDatabase English-series hint.  Such rows are intentionally not
// sent to Avmoo, so they must have an independent JavMenu path instead of
// waiting for the Avmoo no-update fallback (which can be starved by updates to
// other rows).
func scanMissingJavLocalSeriesWithoutEnglishHintWithJavMenu(ctx context.Context) (int64, error) {
	items, err := db.ListJavsMissingLocalSeries(ctx)
	if err != nil {
		return 0, err
	}
	var updatedCount int64
	logging.Info("found %d javs missing local series without english hint", countJavSeriesWithoutEnglishHint(items))
	shuffleJavMetadataScanItems(items)
	for _, item := range items {
		if item.SeriesEnID != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return updatedCount, err
		}
		code := strings.TrimSpace(item.Code)
		if code == "" {
			continue
		}
		info, err := jav.LookupJavByCode(code, jav.ProviderJavMenu)
		if err != nil {
			if !errors.Is(err, jav.ResourceNotFonud) {
				logging.Error("lookup javmenu series without english hint failed id=%d code=%s err=%v", item.ID, code, err)
			}
			continue
		}
		series := ""
		if info != nil {
			series = strings.TrimSpace(info.Series)
		}
		if series == "" {
			continue
		}
		if updated, err := db.UpdateJavSeriesIfMissing(ctx, item.ID, series); err != nil {
			logging.Error("update javmenu local series without english hint failed id=%d code=%s err=%v", item.ID, code, err)
		} else if updated {
			updatedCount++
			logging.Info("jav local series updated provider=%s id=%d code=%s series=%s", jav.ProviderJavMenu.String(), item.ID, code, series)
		}
	}
	return updatedCount, nil
}

func countJavSeriesWithoutEnglishHint(items []db.JavMetadataScanItem) int {
	count := 0
	for _, item := range items {
		if item.SeriesEnID == nil {
			count++
		}
	}
	return count
}

func scanMissingJavUncensoredBackfillOnce(ctx context.Context) error {
	done, err := javUncensoredBackfillDone(ctx)
	if err != nil {
		return err
	}
	if !done {
		// The one-time flag only describes the historical backfill. New works
		// can still arrive with an unknown classification, so the incremental
		// unknown-row pass must continue on every metadata cycle below.
		if err := scanMissingJavUncensored(ctx); err != nil {
			return err
		}
		if err := db.UpsertConfig(ctx, map[string]string{javUncensoredBackfillDoneConfigKey: "1"}); err != nil {
			return fmt.Errorf("mark jav uncensored backfill done: %w", err)
		}
		logging.Info("jav uncensored backfill marked done")
		return nil
	}
	return scanMissingJavUncensored(ctx)
}

// ScanUncensoredJavMetadata fills missing uncensored metadata through AVSOX.
func ScanUncensoredJavMetadata(ctx context.Context) error {
	if common.DB == nil {
		return errors.New("nil db")
	}
	logging.Info("starting uncensored jav metadata scan")
	return scanMissingUncensoredJavInfoWithAvsox(ctx)
}

func javUncensoredBackfillDone(ctx context.Context) (bool, error) {
	entries, err := db.ListConfig(ctx)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(entries[javUncensoredBackfillDoneConfigKey]) == "1", nil
}

// jav表uncensored字段是新增的，存量数据中使用javbus获取的jav的uncensored状态未知，这个函数专门用javbus重新获取一遍来补齐这个信息。
func scanMissingJavUncensored(ctx context.Context) error {
	items, err := db.ListJavsMissingUncensored(ctx)
	if err != nil {
		return err
	}
	shuffleJavMetadataScanItems(items)
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}

		code := strings.TrimSpace(item.Code)
		if code == "" {
			continue
		}

		for _, provider := range []jav.Provider{jav.ProviderJavBus} {
			info, err := lookupJavUncensoredByCode(code, provider)
			if err != nil {
				if !errors.Is(err, jav.ResourceNotFonud) {
					logging.Error("lookup %s uncensored state failed id=%d code=%s err=%v", provider.String(), item.ID, code, err)
				}
				continue
			}
			if info == nil || info.IsUncensored == nil {
				continue
			}
			if responseCode := models.NormalizeJavCode(info.Code); responseCode != "" && responseCode != models.NormalizeJavCode(code) {
				logging.Error("ignore mismatched jav uncensored metadata provider=%s id=%d requested=%s response=%s", provider.String(), item.ID, code, strings.TrimSpace(info.Code))
				continue
			}
			if err := db.UpdateJavIsUncensoredIfUnknown(ctx, item.ID, *info.IsUncensored); err != nil {
				logging.Error("update jav is_uncensored failed provider=%s id=%d code=%s err=%v", provider.String(), item.ID, code, err)
				continue
			}
			logging.Info("jav is_uncensored updated provider=%s id=%d code=%s is_uncensored=%t", provider.String(), item.ID, code, *info.IsUncensored)
			break
		}
	}
	return nil
}

func scanMissingUncensoredJavInfoWithAvsox(ctx context.Context) error {
	items, err := db.ListUncensoredJavsMissingAvsoxMetadata(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}

		code := strings.TrimSpace(item.Code)
		if code == "" {
			continue
		}

		info, err := jav.LookupJavByCode(code, jav.ProviderAvsox)
		if err != nil {
			if !errors.Is(err, jav.ResourceNotFonud) {
				logging.Error("lookup avsox uncensored metadata failed id=%d code=%s err=%v", item.ID, code, err)
			}
			continue
		}
		if info == nil {
			continue
		}

		studio := strings.TrimSpace(info.Studio)
		if item.StudioID == nil && studio != "" {
			if updated, err := db.UpdateJavStudioIfMissing(ctx, item.ID, studio); err != nil {
				logging.Error("update uncensored jav studio failed id=%d code=%s err=%v", item.ID, code, err)
			} else if updated {
				logging.Info("uncensored jav studio updated provider=%s id=%d code=%s studio=%s", jav.ProviderAvsox.String(), item.ID, code, studio)
			}
		}

		series := strings.TrimSpace(info.Series)
		if item.SeriesID == nil && series != "" {
			if updated, err := db.UpdateJavSeriesIfMissing(ctx, item.ID, series); err != nil {
				logging.Error("update uncensored jav series failed id=%d code=%s err=%v", item.ID, code, err)
			} else if updated {
				logging.Info("uncensored jav series updated provider=%s id=%d code=%s series=%s", jav.ProviderAvsox.String(), item.ID, code, series)
			}
		}

		if len(info.Actors) > 0 {
			updated, err := db.AppendJavIdolsIfMissingForProvider(ctx, item.ID, info.Actors, jav.ProviderAvsox)
			if err != nil {
				logging.Error("update uncensored jav idols failed id=%d code=%s err=%v", item.ID, code, err)
			} else if updated {
				logging.Info("uncensored jav idols updated provider=%s id=%d code=%s count=%d", jav.ProviderAvsox.String(), item.ID, code, len(info.Actors))
			}
		}
	}
	return nil
}

func scanMissingJavLocalSeriesWithAvmoo(ctx context.Context) (int64, error) {
	logging.Info("starting scan missing jav local series with avmoo")
	items, err := db.ListJavsMissingLocalSeriesWithEnglishSeries(ctx)
	if err != nil {
		return 0, err
	}
	var updatedCount int64
	logging.Info("found %d javs missing series", len(items))
	shuffleJavMetadataScanItems(items)
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return updatedCount, err
		}

		code := strings.TrimSpace(item.Code)
		if code == "" {
			continue
		}

		info, err := jav.LookupJavByCode(code, jav.ProviderAvmoo)
		if err != nil {
			if !errors.Is(err, jav.ResourceNotFonud) {
				logging.Error("lookup avmoo metadata failed id=%d code=%s err=%v", item.ID, code, err)
			}
			continue
		}

		series := ""
		if info != nil {
			series = strings.TrimSpace(info.Series)
		}
		if series == "" {
			continue
		}
		if updated, err := db.UpdateJavSeriesIfMissing(ctx, item.ID, series); err != nil {
			logging.Error("update jav local series failed id=%d code=%s err=%v", item.ID, code, err)
			continue
		} else if updated {
			updatedCount++
			logging.Info("jav local series updated id=%d code=%s series=%s", item.ID, code, series)
		}
	}
	return updatedCount, nil
}

func shuffleJavMetadataScanItems(items []db.JavMetadataScanItem) {
	rand.Shuffle(len(items), func(i, j int) {
		items[i], items[j] = items[j], items[i]
	})
}

// 某些信息可能是通过英文数据源javdatabase获取的，缺少中日文元数据信息，这个函数专门用来补齐。
// 已有标题但仍没有任何女优关联的作品也必须继续进入此扫描；否则
// 女优资料扫描器没有实体可补全，该缺项会永久保留。
func scanMissingJavZhInfo(ctx context.Context, providers []jav.Provider) error {
	items, err := db.ListJavsMissingPrimaryMetadata(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}

		code := strings.TrimSpace(item.Code)
		if code == "" {
			continue
		}
		needsTitle := strings.TrimSpace(item.Title) == ""
		needsIdols := item.IdolCount == 0

		for _, provider := range javMetadataProvidersForCode(code, providers) {
			info, err := lookupJavMetadataByCode(code, provider)
			if err != nil {
				if !errors.Is(err, jav.ResourceNotFonud) {
					logging.Error("lookup %s metadata failed id=%d code=%s err=%v", provider.String(), item.ID, code, err)
				}
				continue
			}
			if info == nil {
				continue
			}
			if responseCode := models.NormalizeJavCode(info.Code); responseCode != "" && responseCode != models.NormalizeJavCode(code) {
				logging.Error("ignore mismatched jav metadata provider=%s id=%d requested=%s response=%s", provider.String(), item.ID, code, strings.TrimSpace(info.Code))
				continue
			}

			providerValue := jav.ParseProvider(int(info.Provider))
			if providerValue == jav.ProviderUnknown {
				providerValue = provider
			}
			updated := false
			if needsTitle && strings.TrimSpace(info.Title) != "" {
				metadata := *info
				metadata.Code = code
				metadata.Provider = providerValue
				// AVSOX is a useful metadata fallback, but its parser marks
				// every result as uncensored.  Classification belongs to the
				// dedicated JavBus/uncensored scanner; never let this generic
				// pass overwrite an unknown state with AVSOX's assumption.
				if provider == jav.ProviderAvsox || providerValue == jav.ProviderAvsox {
					metadata.IsUncensored = nil
				}
				if _, err := db.SaveJavPrimaryMetadataIfMissing(ctx, item.ID, code, &metadata); err != nil {
					logging.Error("update jav primary metadata failed provider=%s id=%d code=%s err=%v", provider.String(), item.ID, code, err)
					continue
				}
				needsTitle = false
				updated = true
			} else if needsIdols && hasNonEmptyJavMetadataValue(info.Actors) {
				var appended bool
				var err error
				if providerValue == jav.ProviderJavDB {
					metadata := *info
					metadata.Code = code
					metadata.Provider = jav.ProviderJavDB
					appended, err = db.ReconcileJavDBIdols(ctx, item.ID, code, &metadata)
				} else {
					appended, err = db.AppendJavIdolsIfMissingForProvider(ctx, item.ID, info.Actors, providerValue)
				}
				if err != nil {
					logging.Error("update jav idol metadata failed provider=%s id=%d code=%s err=%v", provider.String(), item.ID, code, err)
					continue
				}
				// A stale scan item may race with another writer that already
				// attached idols.  Only report an update when this call actually
				// appended a mapping; callers use this signal to enqueue cover work.
				updated = appended
			}
			if needsIdols {
				hasIdols, err := db.JavHasIdols(ctx, item.ID)
				if err != nil {
					logging.Error("verify jav idol metadata id=%d code=%s err=%v", item.ID, code, err)
				} else if hasIdols {
					needsIdols = false
				}
			}
			if updated {
				enqueueJavMetadataCover(code)
				logging.Info("jav primary metadata updated provider=%s id=%d code=%s needs_title=%t needs_idols=%t", provider.String(), item.ID, code, needsTitle, needsIdols)
			}
			if !needsTitle && !needsIdols {
				break
			}
		}
	}
	return nil
}

func hasNonEmptyJavMetadataValue(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func javMetadataProvidersForCode(code string, fallback []jav.Provider) []jav.Provider {
	// JavDB is the identity authority for actresses. Query it first so an
	// actor's stable /actors/<id> link and Japanese stage name become canonical
	// before any provider-specific spelling can create a local row.
	providers := []jav.Provider{jav.ProviderJavDB}
	providers = append(providers, javLinkProvidersForCode(code)...)
	if len(providers) == 0 {
		providers = fallback
	}
	// AVSOX remains a metadata fallback for codes that match the uncensored
	// filename heuristics.  Its classification field is stripped in
	// scanMissingJavZhInfo above; the dedicated uncensored scanner owns that
	// decision so a loose regex cannot mislabel ordinary codes.
	if len(util.ExtractUncensoredCodesFromName(code)) > 0 {
		providers = append(providers, jav.ProviderAvsox)
	}
	providers = append(providers, jav.ProviderJavDBApp)

	result := make([]jav.Provider, 0, len(providers))
	seen := make(map[jav.Provider]struct{}, len(providers))
	for _, provider := range providers {
		if provider == jav.ProviderUnknown {
			continue
		}
		if _, exists := seen[provider]; exists {
			continue
		}
		seen[provider] = struct{}{}
		result = append(result, provider)
	}
	return result
}

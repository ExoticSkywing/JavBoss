package service

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"sync"
	"time"

	"javboss/internal/common/logging"
	"javboss/internal/db"
	"javboss/internal/jav"
	"javboss/internal/models"
	"javboss/internal/util"
)

// A profile that is complete except for ChineseName is a common, legitimate
// state: the available providers often simply do not publish a Chinese
// translation.  Keep the row out of the minute-level scanner loop for a
// bounded period instead of issuing the same three lookups forever.  This is
// intentionally process-local so it needs no schema migration; a restart
// naturally gives the row another chance.
const idolChineseNameRetryTTL = 24 * time.Hour

var idolProfileRetryState = struct {
	sync.Mutex
	nextAttempt map[int64]time.Time
}{
	nextAttempt: make(map[int64]time.Time),
}

var idolProfileNow = time.Now

// StartIdolProfileScanner periodically scans JAV idols with incomplete profile data.
// It runs ScanIdolProfiles immediately and then on every interval until ctx is done, filling
// missing profile fields such as names, measurements, birth date, and profile URL from external
// actress metadata providers.
func StartIdolProfileScanner(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if err := ScanIdolProfiles(ctx); err != nil {
				logging.Error("idol profile scan failed: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// ScanIdolProfiles scans jav_idol rows that are missing profile fields.
// For each idol, it tries to find a solo work code, queries MinnanoAV, JavDatabase, and JavModel
// concurrently, merges details in that priority order, normalizes Chinese names, and writes the
// completed profile fields back to the database.
func ScanIdolProfiles(ctx context.Context) error {
	idols, err := db.ListIdolsMissingProfile(ctx)
	if err != nil {
		return err
	}
	rand.New(rand.NewSource(time.Now().UnixNano())).Shuffle(len(idols), func(i, j int) {
		idols[i], idols[j] = idols[j], idols[i]
	})
	logging.Info("found %d idols missing profile info", len(idols))
	for _, idol := range idols {
		if err := ctx.Err(); err != nil {
			return err
		}
		if shouldSkipIdolProfileAttempt(idol, idolProfileNow()) {
			continue
		}
		lookupName := strings.TrimSpace(idol.JapaneseName)
		if lookupName == "" {
			lookupName = strings.TrimSpace(idol.Name)
		}
		var (
			javDatabaseInfo *jav.ActressInfo
			minnanoAVInfo   *jav.ActressInfo
			javModelInfo    *jav.ActressInfo
			code            string
		)
		code, err = db.FindIdolSoloCode(ctx, idol.ID)
		if err != nil {
			logging.Error("find solo code failed idol=%s err=%v", idol.Name, err)
		}

		var javDatabaseLookup idolActressLookup
		if code != "" {
			javDatabaseLookup = func() (*jav.ActressInfo, error) {
				return jav.LookupActressByCode(code, jav.ProviderJavDatabase)
			}
		}

		var minnanoAVLookup, javModelLookup idolActressLookup
		if lookupName != "" {
			minnanoAVLookup = func() (*jav.ActressInfo, error) {
				return jav.LookupActressByJapaneseName(lookupName, jav.ProviderMinnanoAV)
			}
			javModelLookup = func() (*jav.ActressInfo, error) {
				return jav.LookupActressByJapaneseName(lookupName, jav.ProviderJavModel)
			}
		}

		lookupResults := lookupActressProfilesConcurrently(minnanoAVLookup, javDatabaseLookup, javModelLookup)
		minnanoAVInfo = lookupResults[0].info
		javDatabaseInfo = lookupResults[1].info
		javModelInfo = lookupResults[2].info
		if lookupErr := lookupResults[0].err; lookupErr != nil && !errors.Is(lookupErr, jav.ResourceNotFonud) {
			logging.Error("lookup actress (minnanoav) failed idol=%d name=%s err=%v", idol.ID, lookupName, lookupErr)
		}
		if lookupErr := lookupResults[1].err; lookupErr != nil && !errors.Is(lookupErr, jav.ResourceNotFonud) {
			logging.Error("lookup actress (javdatabase) failed idol=%s code=%s err=%v", idol.Name, code, lookupErr)
		}
		if lookupErr := lookupResults[2].err; lookupErr != nil && !errors.Is(lookupErr, jav.ResourceNotFonud) {
			logging.Error("lookup actress (javmodel) failed idol=%d name=%s err=%v", idol.ID, lookupName, lookupErr)
		}

		info := mergeActressInfosByPriority(minnanoAVInfo, javDatabaseInfo, javModelInfo)
		if info == nil {
			continue
		}
		if info.ChineseName != "" {
			info.ChineseName = util.SimplifyChineseName(info.ChineseName)
		}
		updated, err := db.UpdateIdolProfile(ctx, idol.ID, info)
		if err != nil {
			logging.Error("update idol profile failed idol=%d name=%s err=%v", idol.ID, idol.Name, err)
			continue
		}
		if updated {
			logging.Info("idol profile updated idol=%d name=%s code=%s", idol.ID, idol.Name, code)
			if strings.TrimSpace(info.ChineseName) != "" {
				clearIdolProfileRetry(idol.ID)
			}
		}
	}
	return nil
}

// idolMissingOnlyChineseName reports the narrow partial-profile state for
// which retry suppression is useful.  Other incomplete profiles continue to
// be retried normally because measurements/names can often be filled on the
// next pass.
func idolMissingOnlyChineseName(idol models.JavIdol) bool {
	return strings.TrimSpace(idol.ChineseName) == "" &&
		strings.TrimSpace(idol.JapaneseName) != "" &&
		strings.TrimSpace(idol.RomanName) != "" &&
		idol.HeightCM != nil &&
		idol.BirthDate != nil &&
		idol.Bust != nil &&
		idol.Waist != nil &&
		idol.Hips != nil &&
		idol.Cup != nil
}

// shouldSkipIdolProfileAttempt claims the next retry slot for a Chinese-name
// only profile.  It returns false for all other profile states.
func shouldSkipIdolProfileAttempt(idol models.JavIdol, now time.Time) bool {
	if !idolMissingOnlyChineseName(idol) {
		return false
	}
	idolProfileRetryState.Lock()
	defer idolProfileRetryState.Unlock()
	if next, ok := idolProfileRetryState.nextAttempt[idol.ID]; ok && now.Before(next) {
		return true
	}
	idolProfileRetryState.nextAttempt[idol.ID] = now.Add(idolChineseNameRetryTTL)
	return false
}

func clearIdolProfileRetry(idolID int64) {
	if idolID <= 0 {
		return
	}
	idolProfileRetryState.Lock()
	delete(idolProfileRetryState.nextAttempt, idolID)
	idolProfileRetryState.Unlock()
}

// resetIdolProfileRetryState is kept package-private for deterministic tests.
func resetIdolProfileRetryState() {
	idolProfileRetryState.Lock()
	idolProfileRetryState.nextAttempt = make(map[int64]time.Time)
	idolProfileRetryState.Unlock()
}

type idolActressLookup func() (*jav.ActressInfo, error)

type idolActressLookupResult struct {
	info *jav.ActressInfo
	err  error
}

func lookupActressProfilesConcurrently(lookups ...idolActressLookup) []idolActressLookupResult {
	results := make([]idolActressLookupResult, len(lookups))
	var workers sync.WaitGroup
	for index, lookup := range lookups {
		if lookup == nil {
			continue
		}
		workers.Add(1)
		go func(index int, lookup idolActressLookup) {
			defer workers.Done()
			results[index].info, results[index].err = lookup()
		}(index, lookup)
	}
	workers.Wait()
	return results
}

func mergeActressInfosByPriority(infos ...*jav.ActressInfo) *jav.ActressInfo {
	var merged *jav.ActressInfo
	for _, info := range infos {
		merged = mergeActressInfo(merged, info)
	}
	return merged
}

func mergeActressInfo(primary, secondary *jav.ActressInfo) *jav.ActressInfo {
	if primary == nil && secondary == nil {
		return nil
	}
	if primary == nil {
		copied := *secondary
		return &copied
	}
	merged := *primary
	if secondary == nil {
		return &merged
	}
	if merged.RomanName == "" {
		merged.RomanName = secondary.RomanName
	}
	if merged.JapaneseName == "" {
		merged.JapaneseName = secondary.JapaneseName
	}
	if merged.ChineseName == "" {
		merged.ChineseName = secondary.ChineseName
	}
	if merged.HeightCM == 0 {
		merged.HeightCM = secondary.HeightCM
	}
	if merged.Bust == 0 {
		merged.Bust = secondary.Bust
	}
	if merged.Waist == 0 {
		merged.Waist = secondary.Waist
	}
	if merged.Hips == 0 {
		merged.Hips = secondary.Hips
	}
	if merged.BirthDate == 0 {
		merged.BirthDate = secondary.BirthDate
	}
	if merged.Cup == 0 {
		merged.Cup = secondary.Cup
	}
	if merged.ProfileURL == "" {
		merged.ProfileURL = secondary.ProfileURL
	}
	return &merged
}

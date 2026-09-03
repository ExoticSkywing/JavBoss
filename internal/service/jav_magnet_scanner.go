package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"javboss/internal/common"
	"javboss/internal/common/logging"
	"javboss/internal/db"
	"javboss/internal/jav"
	"javboss/internal/models"
)

const (
	javMagnetCollectionBatchSize  = 3
	javMagnetCollectionRetryAfter = 15 * time.Second
)

var javMagnetScanRequests = make(chan struct{}, 1)

// StartJavMagnetScanner continuously collects JavDB candidates for works that
// have completed metadata collection. It runs independently from the HTTP
// request and metadata scanners so a slow provider never blocks input or page
// rendering.
func StartJavMagnetScanner(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		force := true
		for {
			retryAfter := javMagnetCollectionRetryAfter
			if force {
				retryAfter = 0
				force = false
			}
			if err := scanJavMagnets(ctx, retryAfter); err != nil {
				logging.Error("jav magnet scan failed: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-javMagnetScanRequests:
				force = true
			}
		}
	}()
}

// RequestJavMagnetScan wakes the collector after a metadata/input burst. The
// channel is deliberately coalescing so a bulk paste cannot create one scan
// goroutine per code.
func RequestJavMagnetScan() {
	select {
	case javMagnetScanRequests <- struct{}{}:
	default:
	}
}

// ScanJavMagnets performs one bounded pass. It is useful for tests and manual
// diagnostics; the server normally calls StartJavMagnetScanner instead.
func ScanJavMagnets(ctx context.Context) error {
	return scanJavMagnets(ctx, 0)
}

func scanJavMagnets(ctx context.Context, retryAfter time.Duration) error {
	if common.DB == nil {
		return errors.New("nil db")
	}
	items, err := db.ListJavsPendingMagnetCollection(ctx, javMagnetCollectionBatchSize, retryAfter)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	logging.Info("starting automatic JAV magnet collection: %d work(s)", len(items))
	client := jav.DefaultJavDBAppClient()
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		code := strings.TrimSpace(item.Code)
		if code == "" {
			continue
		}
		// Touch before the network call. A timeout or provider miss is therefore
		// retried on the next scheduled pass, not repeatedly in this one.
		if err := db.MarkJavMagnetCollectionAttempt(ctx, item.ID); err != nil {
			logging.Error("mark JAV magnet collection attempt id=%d code=%s: %v", item.ID, code, err)
			continue
		}
		response := client.ResolveBatch(ctx, []string{code})
		if len(response.Items) == 0 {
			logging.Error("JavDB returned no magnet response id=%d code=%s", item.ID, code)
			continue
		}
		resolved := response.Items[0]
		if resolved.Error != "" {
			logging.Error("automatic JAV magnet collection failed id=%d code=%s: %s", item.ID, code, resolved.Error)
			continue
		}
		if resolved.Movie == nil || models.NormalizeJavCode(resolved.Movie.Number) != models.NormalizeJavCode(code) {
			logging.Error("ignore mismatched automatic JAV magnets id=%d requested=%s", item.ID, code)
			continue
		}
		if _, err := db.UpsertJavMagnetCandidatesForCode(ctx, item.ID, code, resolved.Magnets); err != nil {
			logging.Error("save automatic JAV magnets failed id=%d code=%s: %v", item.ID, code, err)
			continue
		}
		logging.Info("automatic JAV magnet collection completed id=%d code=%s candidates=%d", item.ID, code, len(resolved.Magnets))
	}
	return nil
}

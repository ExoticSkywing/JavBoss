package db

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"javboss/internal/common"
	"javboss/internal/jav"
	"javboss/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrJavMagnetNotFound            = errors.New("JAV magnet candidate not found")
	ErrJavMagnetAlreadyRejected     = errors.New("JAV magnet candidate is rejected")
	ErrJavMagnetNoSelection         = errors.New("JAV magnet selection is required")
	ErrJavDownloadNoFile            = errors.New("JAV download has no active file to review")
	ErrJavDownloadAttemptRequired   = errors.New("JAV quality acceptance requires a download attempt")
	ErrJavDownloadCandidateMismatch = errors.New("JAV magnet does not match the active download attempt")
	ErrJavAlreadyQualityAccepted    = errors.New("JAV work already has a formal quality acceptance")
	ErrJavDownloadAlreadyActive     = errors.New("JAV work already has an active download attempt")
	ErrJavAlreadyHasFile            = errors.New("JAV work already has an active file")
	ErrJavDownloadAmbiguousFile     = errors.New("JAV download has multiple active file locations")
)

func javDownloadAttemptAwaitingResolutionStatuses() []string {
	return []string{
		models.JavDownloadAttemptPending,
		models.JavDownloadAttemptSubmitted,
		models.JavDownloadAttemptDownloaded,
		models.JavDownloadAttemptAwaitingQuality,
		models.JavDownloadAttemptUncertain,
	}
}

// JavMagnetCollectionItem is the minimal canonical work identity needed by
// the asynchronous magnet collector. Keeping this query small avoids loading
// cards, videos, or review history for every pending work.
type JavMagnetCollectionItem struct {
	ID   int64  `gorm:"column:id"`
	Code string `gorm:"column:code"`
}

// ListJavsPendingMagnetCollection returns works whose metadata is ready but
// which have no persisted candidates yet. UpdatedAt is used as a retry
// throttle: a failed/empty provider response is retried on a later pass rather
// than hammering JavDB continuously.
func ListJavsPendingMagnetCollection(ctx context.Context, limit int, retryAfter time.Duration) ([]JavMagnetCollectionItem, error) {
	if limit <= 0 {
		limit = 5
	}
	if retryAfter < 0 {
		retryAfter = 0
	}
	cutoff := time.Now().UTC().Add(-retryAfter)
	var items []JavMagnetCollectionItem
	query := common.DB.WithContext(ctx).
		Table("jav_acquisition ja").
		Select("j.id, j.code").
		Joins("JOIN jav j ON j.id = ja.jav_id").
		Where("ja.stage = ?", models.JavAcquisitionStageMagnetCollecting).
		Where("ja.updated_at <= ?", cutoff).
		Where("NOT EXISTS (SELECT 1 FROM jav_magnet_candidate mc WHERE mc.jav_id = ja.jav_id)").
		Where("NOT EXISTS (SELECT 1 FROM jav_quality_acceptance qa WHERE qa.jav_id = ja.jav_id)").
		Where("NOT EXISTS (SELECT 1 FROM jav_download_attempt da WHERE da.jav_id = ja.jav_id AND da.status IN ?)", javDownloadAttemptAwaitingResolutionStatuses()).
		Order("ja.updated_at ASC, ja.jav_id ASC").
		Limit(limit)
	if err := query.Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list JAV works pending magnet collection: %w", err)
	}
	if items == nil {
		items = []JavMagnetCollectionItem{}
	}
	return items, nil
}

// MarkJavMagnetCollectionAttempt advances the retry timestamp without
// changing the workflow stage. It is intentionally conditional so a manual
// review or a concurrent download callback cannot be overwritten by a stale
// scanner pass.
func MarkJavMagnetCollectionAttempt(ctx context.Context, javID int64) error {
	if javID <= 0 {
		return errors.New("jav id must be positive")
	}
	result := common.DB.WithContext(ctx).
		Model(&models.JavAcquisition{}).
		Where("jav_id = ? AND stage = ?", javID, models.JavAcquisitionStageMagnetCollecting).
		Updates(map[string]any{"updated_at": time.Now().UTC()})
	if result.Error != nil {
		return fmt.Errorf("mark JAV magnet collection attempt: %w", result.Error)
	}
	return nil
}

func javDownloadAttemptActiveStatuses() []string {
	return []string{
		models.JavDownloadAttemptPending,
		models.JavDownloadAttemptSubmitted,
		models.JavDownloadAttemptDownloaded,
		models.JavDownloadAttemptAwaitingQuality,
		models.JavDownloadAttemptUncertain,
	}
}

// javDownloadAttemptTransitionAllowed keeps transport callbacks monotonic.
// A downloader may deliver callbacks out of order, but an older callback must
// never move a task backwards or reopen a terminal result. Retrying a failed
// or uncertain attempt is handled explicitly by CreateJavDownloadBatch, which
// resets the same idempotent attempt inside its transaction.
func javDownloadAttemptTransitionAllowed(current, next string) bool {
	if current == next {
		return true
	}
	switch current {
	case models.JavDownloadAttemptAccepted,
		models.JavDownloadAttemptRejected,
		models.JavDownloadAttemptFailed:
		return false
	case models.JavDownloadAttemptPending:
		return next != models.JavDownloadAttemptPending
	case models.JavDownloadAttemptUncertain:
		return next == models.JavDownloadAttemptSubmitted ||
			next == models.JavDownloadAttemptDownloaded ||
			next == models.JavDownloadAttemptAwaitingQuality ||
			next == models.JavDownloadAttemptFailed ||
			next == models.JavDownloadAttemptAccepted ||
			next == models.JavDownloadAttemptRejected
	case models.JavDownloadAttemptSubmitted:
		return next == models.JavDownloadAttemptUncertain ||
			next == models.JavDownloadAttemptDownloaded ||
			next == models.JavDownloadAttemptAwaitingQuality ||
			next == models.JavDownloadAttemptFailed ||
			next == models.JavDownloadAttemptAccepted ||
			next == models.JavDownloadAttemptRejected
	case models.JavDownloadAttemptDownloaded:
		return next == models.JavDownloadAttemptAwaitingQuality ||
			next == models.JavDownloadAttemptAccepted ||
			next == models.JavDownloadAttemptRejected
	case models.JavDownloadAttemptAwaitingQuality:
		return next == models.JavDownloadAttemptAccepted ||
			next == models.JavDownloadAttemptRejected
	default:
		return true
	}
}

// JavMagnetQueueItem is the compact shape used by the pending-send workbench.
type JavMagnetQueueItem struct {
	Jav       models.Jav                 `json:"jav"`
	Candidate models.JavMagnetCandidate  `json:"candidate"`
	Selection models.JavMagnetSelection  `json:"selection"`
	Attempt   *models.JavDownloadAttempt `json:"attempt,omitempty"`
}

// JavImportDaySummary is the compact daily history used by the import
// timeline. Days with no accepted work are intentionally absent.
type JavImportDaySummary struct {
	Day    string       `json:"day"`
	Count  int64        `json:"count"`
	JavIDs []int64      `json:"jav_ids"`
	Items  []models.Jav `json:"items"`
}

// ListJavQualityReviewQueue returns cloud-downloaded works that already have a
// physical file but still lack formal quality acceptance. Keeping this query
// separate from inventory=imported makes the suspended state directly
// discoverable without creating a second work entity.
func ListJavQualityReviewQueue(ctx context.Context, limit, offset int, directoryIDs []int64) ([]models.Jav, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	activeLocation := common.DB.WithContext(ctx).
		Table("video_location vl_quality_queue").
		Select("1").
		Joins("JOIN directory d_quality_queue ON d_quality_queue.id = vl_quality_queue.directory_id").
		Where("vl_quality_queue.jav_id = a.jav_id").
		Where(activeLocationWhereSQL("vl_quality_queue", "d_quality_queue"))
	activeLocation = applyDirectoryFilter(activeLocation, "vl_quality_queue", directoryIDs)

	base := common.DB.WithContext(ctx).
		Table("jav_download_attempt a").
		Where("a.id = (SELECT MAX(a_latest.id) FROM jav_download_attempt a_latest WHERE a_latest.jav_id = a.jav_id)").
		Where("a.status IN ?", []string{
			models.JavDownloadAttemptPending,
			models.JavDownloadAttemptSubmitted,
			models.JavDownloadAttemptDownloaded,
			models.JavDownloadAttemptAwaitingQuality,
			models.JavDownloadAttemptUncertain,
		}).
		Where("NOT EXISTS (SELECT 1 FROM jav_quality_acceptance qa_quality_queue WHERE qa_quality_queue.jav_id = a.jav_id)").
		Where("EXISTS (?)", activeLocation)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count JAV quality review queue: %w", err)
	}
	var ids []int64
	if err := base.Select("a.jav_id").Order("a.id DESC").Limit(limit).Offset(offset).Pluck("a.jav_id", &ids).Error; err != nil {
		return nil, 0, fmt.Errorf("list JAV quality review queue: %w", err)
	}
	itemsByID, err := loadJavImportDayCards(ctx, ids, directoryIDs)
	if err != nil {
		return nil, 0, err
	}
	items := make([]models.Jav, 0, len(ids))
	for _, id := range ids {
		if item, ok := itemsByID[id]; ok {
			items = append(items, item)
		}
	}
	return items, total, nil
}

func ListJavImportDaySummaries(ctx context.Context, limit, offset int, directoryIDs []int64) ([]JavImportDaySummary, int64, error) {
	if limit <= 0 {
		limit = 31
	}
	if offset < 0 {
		offset = 0
	}
	type row struct {
		AcceptedAt time.Time `gorm:"column:accepted_at"`
		JavID      int64     `gorm:"column:jav_id"`
	}
	var rows []row
	if err := common.DB.WithContext(ctx).Table("jav_quality_acceptance qa").Select("qa.accepted_at, qa.jav_id").Order("qa.accepted_at DESC, qa.jav_id DESC").Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list JAV import days: %w", err)
	}
	grouped := make([]JavImportDaySummary, 0)
	byDay := make(map[string]int)
	for _, row := range rows {
		day := row.AcceptedAt.In(time.Local).Format("2006-01-02")
		index, ok := byDay[day]
		if !ok {
			index = len(grouped)
			byDay[day] = index
			grouped = append(grouped, JavImportDaySummary{Day: day})
		}
		grouped[index].Count++
		grouped[index].JavIDs = append(grouped[index].JavIDs, row.JavID)
	}
	total := int64(len(grouped))
	if offset >= len(grouped) {
		return []JavImportDaySummary{}, total, nil
	}
	end := offset + limit
	if end > len(grouped) {
		end = len(grouped)
	}
	page := grouped[offset:end]
	pageJavIDs := make([]int64, 0)
	for _, day := range page {
		pageJavIDs = append(pageJavIDs, day.JavIDs...)
	}
	itemsByID, err := loadJavImportDayCards(ctx, pageJavIDs, directoryIDs)
	if err != nil {
		return nil, 0, err
	}
	for dayIndex := range page {
		page[dayIndex].Items = make([]models.Jav, 0, len(page[dayIndex].JavIDs))
		for _, javID := range page[dayIndex].JavIDs {
			if item, ok := itemsByID[javID]; ok {
				page[dayIndex].Items = append(page[dayIndex].Items, item)
			}
		}
	}
	return page, total, nil
}

func loadJavImportDayCards(ctx context.Context, javIDs, directoryIDs []int64) (map[int64]models.Jav, error) {
	ids := uniqueInt64s(javIDs)
	result := make(map[int64]models.Jav, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var items []models.Jav
	if err := common.DB.WithContext(ctx).
		Preload("Studio").
		Preload("Idols").
		Preload("Series").
		Where("id IN ?", ids).
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("load JAV import day cards: %w", err)
	}
	if err := attachJavLocationVideos(ctx, items, directoryIDs); err != nil {
		return nil, err
	}
	if err := attachJavLifecycleStates(ctx, items); err != nil {
		return nil, err
	}
	if err := attachVisibleJavTags(ctx, items); err != nil {
		return nil, err
	}
	if err := attachJavFavoriteCounts(ctx, items); err != nil {
		return nil, err
	}
	for _, item := range items {
		result[item.ID] = item
	}
	return result, nil
}

// AttachJavMagnetWorkflow loads candidate and final-acceptance facts for a
// single detail response. Search/list endpoints intentionally do not preload
// this data for every card.
func AttachJavMagnetWorkflow(ctx context.Context, item *models.Jav) error {
	if item == nil || item.ID <= 0 {
		return nil
	}
	candidates, err := ListJavMagnetCandidates(ctx, item.ID, true)
	if err != nil {
		return err
	}
	item.MagnetCandidates = candidates
	var selection models.JavMagnetSelection
	if err := common.DB.WithContext(ctx).Where("jav_id = ?", item.ID).First(&selection).Error; err == nil {
		item.MagnetSelection = &selection
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("load JAV magnet selection: %w", err)
	}
	var attempt models.JavDownloadAttempt
	if err := common.DB.WithContext(ctx).Where("jav_id = ? AND status IN ?", item.ID, javDownloadAttemptAwaitingResolutionStatuses()).Order("id DESC").First(&attempt).Error; err == nil {
		item.DownloadAttempt = &attempt
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("load JAV download attempt: %w", err)
	}
	var acceptance models.JavQualityAcceptance
	if err := common.DB.WithContext(ctx).Where("jav_id = ?", item.ID).First(&acceptance).Error; err == nil {
		item.QualityAcceptance = &acceptance
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("load JAV quality acceptance: %w", err)
	}
	return nil
}

// ListJavMagnetCandidates returns source candidates in newest-first order.
// Rejected candidates are retained for research and are only hidden when the
// caller explicitly requests active candidates.
func ListJavMagnetCandidates(ctx context.Context, javID int64, includeRejected bool) ([]models.JavMagnetCandidate, error) {
	if javID <= 0 {
		return nil, errors.New("jav id must be positive")
	}
	query := common.DB.WithContext(ctx).Where("jav_id = ?", javID)
	if !includeRejected {
		query = query.Where("review_status <> ?", models.JavMagnetReviewRejected)
	}
	var candidates []models.JavMagnetCandidate
	if err := query.Order("review_status = 'rejected', CASE WHEN hd = 1 AND size_mi_b BETWEEN 5120 AND 10240 THEN 0 WHEN hd = 1 THEN 1 WHEN size_mi_b BETWEEN 5120 AND 10240 THEN 2 ELSE 3 END, ABS(size_mi_b - 7680), last_seen_at DESC, id DESC").Find(&candidates).Error; err != nil {
		return nil, fmt.Errorf("list JAV magnet candidates: %w", err)
	}
	if candidates == nil {
		candidates = []models.JavMagnetCandidate{}
	}
	return candidates, nil
}

// UpsertJavMagnetCandidates persists the objective fields returned by JavDB.
// The unique (jav_id, info_hash) key makes repeated collection idempotent.
func UpsertJavMagnetCandidates(ctx context.Context, javID int64, magnets []jav.JavDBAppMagnet) ([]models.JavMagnetCandidate, error) {
	if javID <= 0 {
		return nil, errors.New("jav id must be positive")
	}
	now := time.Now().UTC()
	err := withSQLiteTransactionRetry(ctx, common.DB, func(tx *gorm.DB) error {
		for _, magnet := range magnets {
			hash := normalizeJavMagnetHash(magnet.Hash)
			if hash == "" {
				continue
			}
			name := strings.TrimSpace(magnet.Name)
			candidate := models.JavMagnetCandidate{
				JavID: javID, InfoHash: hash, URI: buildJavMagnetURI(hash, name), Name: name,
				SizeMiB: magnet.Size, HD: magnet.HD, CNSub: magnet.CNSub, Files: magnet.Files,
				SourceCreatedAt: strings.TrimSpace(magnet.CreatedAt), FirstSeenAt: now, LastSeenAt: now,
				ReviewStatus: models.JavMagnetReviewPending, CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "jav_id"}, {Name: "info_hash"}},
				DoUpdates: clause.Assignments(map[string]any{
					"uri": candidate.URI, "name": candidate.Name, "size_mi_b": candidate.SizeMiB,
					"hd": candidate.HD, "cn_sub": candidate.CNSub, "files": candidate.Files,
					"source_created_at": candidate.SourceCreatedAt, "last_seen_at": now, "updated_at": now,
				}),
			}).Create(&candidate).Error; err != nil {
				return fmt.Errorf("upsert JAV magnet candidate: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := withSQLiteTransactionRetry(ctx, common.DB, func(tx *gorm.DB) error {
		if err := ensureJavAcquisitionTx(tx, javID, now); err != nil {
			return err
		}
		if len(magnets) == 0 {
			return nil
		}
		if err := tx.Model(&models.JavAcquisition{}).
			Where("jav_id = ? AND stage IN ?", javID, []string{models.JavAcquisitionStageMetadataPending, models.JavAcquisitionStageMagnetCollecting}).
			Update("stage", models.JavAcquisitionStageMagnetReview).Error; err != nil {
			return fmt.Errorf("advance JAV acquisition after magnet collection: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return ListJavMagnetCandidates(ctx, javID, true)
}

// MarkJavDownloadAttempt updates the transport-neutral hand-off state. An
// external adapter can call this after accepting or completing a task without
// needing to know any UI details.
func MarkJavDownloadAttempt(ctx context.Context, attemptID int64, status, externalTaskID, errText string) (*models.JavDownloadAttempt, error) {
	if attemptID <= 0 || strings.TrimSpace(status) == "" {
		return nil, errors.New("attempt id and status are required")
	}
	allowed := map[string]struct{}{
		models.JavDownloadAttemptPending: {}, models.JavDownloadAttemptSubmitted: {},
		models.JavDownloadAttemptDownloaded: {}, models.JavDownloadAttemptAwaitingQuality: {},
		models.JavDownloadAttemptAccepted: {}, models.JavDownloadAttemptRejected: {},
		models.JavDownloadAttemptFailed: {}, models.JavDownloadAttemptUncertain: {},
	}
	if _, ok := allowed[status]; !ok {
		return nil, fmt.Errorf("invalid download attempt status %q", status)
	}
	var attempt models.JavDownloadAttempt
	batchID := int64(0)
	err := withSQLiteTransactionRetry(ctx, common.DB, func(tx *gorm.DB) error {
		if err := tx.First(&attempt, attemptID).Error; err != nil {
			return fmt.Errorf("load JAV download attempt: %w", err)
		}
		batchID = attempt.BatchID
		if !javDownloadAttemptTransitionAllowed(attempt.Status, status) {
			// Ignore stale or terminal callbacks idempotently. Returning the
			// current row lets the external service stop retrying the callback.
			return nil
		}
		now := time.Now().UTC()
		updates := map[string]any{"status": status, "external_task_id": strings.TrimSpace(externalTaskID), "error": strings.TrimSpace(errText)}
		if status == models.JavDownloadAttemptSubmitted && attempt.SubmittedAt == nil {
			updates["submitted_at"] = now
		}
		if status == models.JavDownloadAttemptDownloaded || status == models.JavDownloadAttemptAwaitingQuality || status == models.JavDownloadAttemptAccepted || status == models.JavDownloadAttemptRejected || status == models.JavDownloadAttemptFailed {
			updates["completed_at"] = now
		}
		if err := tx.Model(&attempt).Updates(updates).Error; err != nil {
			return fmt.Errorf("update JAV download attempt: %w", err)
		}
		stage := models.JavAcquisitionStageDownloadSubmitted
		if status == models.JavDownloadAttemptPending {
			stage = models.JavAcquisitionStageReadyToDownload
		}
		if status == models.JavDownloadAttemptUncertain {
			stage = models.JavAcquisitionStageReadyToDownload
		}
		if status == models.JavDownloadAttemptDownloaded || status == models.JavDownloadAttemptAwaitingQuality {
			stage = models.JavAcquisitionStageQualityReview
		}
		if status == models.JavDownloadAttemptFailed || status == models.JavDownloadAttemptRejected {
			stage = models.JavAcquisitionStageMagnetReview
		}
		if err := tx.Model(&models.JavAcquisition{}).Where("jav_id = ? AND stage <> ?", attempt.JavID, models.JavAcquisitionStageImported).Update("stage", stage).Error; err != nil {
			return fmt.Errorf("update JAV acquisition after download attempt: %w", err)
		}
		if err := tx.First(&attempt, attemptID).Error; err != nil {
			return err
		}
		return aggregateJavDownloadBatchTx(tx, batchID)
	})
	if err != nil {
		return nil, err
	}
	return &attempt, nil
}

// JavDownloadSubmissionItem is the transport-neutral payload handed to an
// external cloud downloader. The idempotency key must be forwarded unchanged
// so retries cannot create a second task for the same work/candidate.
type JavDownloadSubmissionItem struct {
	AttemptID      int64  `json:"attempt_id"`
	JavID          int64  `json:"jav_id"`
	Code           string `json:"code"`
	CandidateID    int64  `json:"candidate_id"`
	MagnetURI      string `json:"magnet_uri"`
	IdempotencyKey string `json:"idempotency_key"`
}

// ListJavDownloadSubmission returns the exact pending attempts and their
// canonical work/candidate fields for an external downloader adapter.
func ListJavDownloadSubmission(ctx context.Context, batchID int64) ([]JavDownloadSubmissionItem, error) {
	if batchID <= 0 {
		return nil, errors.New("batch id must be positive")
	}
	var items []JavDownloadSubmissionItem
	if err := common.DB.WithContext(ctx).
		Table("jav_download_attempt a").
		Select("a.id AS attempt_id, a.jav_id, j.code, a.candidate_id, c.uri AS magnet_uri, a.idempotency_key").
		Joins("JOIN jav j ON j.id = a.jav_id").
		Joins("JOIN jav_magnet_candidate c ON c.id = a.candidate_id AND c.jav_id = a.jav_id").
		Where("a.batch_id = ?", batchID).
		Order("a.id ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list JAV download submission: %w", err)
	}
	if items == nil {
		items = []JavDownloadSubmissionItem{}
	}
	return items, nil
}

// MarkJavDownloadBatchExternal records the external batch identifier after a
// successful hand-off. Attempt state remains the source of truth for progress.
func MarkJavDownloadBatchExternal(ctx context.Context, batchID int64, externalBatchID string) (*models.JavDownloadBatch, error) {
	if batchID <= 0 {
		return nil, errors.New("batch id must be positive")
	}
	var batch models.JavDownloadBatch
	err := withSQLiteTransactionRetry(ctx, common.DB, func(tx *gorm.DB) error {
		if err := tx.First(&batch, batchID).Error; err != nil {
			return fmt.Errorf("load JAV download batch: %w", err)
		}
		if err := tx.Model(&batch).Updates(map[string]any{
			"external_batch_id": strings.TrimSpace(externalBatchID),
			"updated_at":        time.Now().UTC(),
		}).Error; err != nil {
			return fmt.Errorf("save external JAV download batch id: %w", err)
		}
		return aggregateJavDownloadBatchTx(tx, batchID)
	})
	if err != nil {
		return nil, err
	}
	return GetJavDownloadBatch(ctx, batchID)
}

// aggregateJavDownloadBatchTx derives batch state from all attempts. This is
// intentionally recalculated after every callback, so partial failures and
// retries remain visible without a background reconciler.
func aggregateJavDownloadBatchTx(tx *gorm.DB, batchID int64) error {
	var attempts []models.JavDownloadAttempt
	if err := tx.Where("batch_id = ?", batchID).Find(&attempts).Error; err != nil {
		return fmt.Errorf("load JAV batch attempts: %w", err)
	}
	if len(attempts) == 0 {
		return tx.Model(&models.JavDownloadBatch{}).Where("id = ?", batchID).Updates(map[string]any{
			"status":     models.JavDownloadBatchPending,
			"updated_at": time.Now().UTC(),
		}).Error
	}
	pending, active, failed, accepted := 0, 0, 0, 0
	for _, attempt := range attempts {
		switch attempt.Status {
		case models.JavDownloadAttemptPending:
			pending++
		case models.JavDownloadAttemptSubmitted, models.JavDownloadAttemptDownloaded, models.JavDownloadAttemptAwaitingQuality:
			active++
		case models.JavDownloadAttemptAccepted:
			accepted++
		case models.JavDownloadAttemptFailed, models.JavDownloadAttemptRejected:
			failed++
		case models.JavDownloadAttemptUncertain:
			failed++
		default:
			active++
		}
	}
	status := models.JavDownloadBatchPartial
	switch {
	case accepted == len(attempts):
		status = models.JavDownloadBatchCompleted
	case failed == len(attempts):
		status = models.JavDownloadBatchFailed
	case active == 0 && pending == len(attempts):
		status = models.JavDownloadBatchPending
	case failed == 0 && pending == 0:
		status = models.JavDownloadBatchSubmitted
	}
	updates := map[string]any{"status": status, "updated_at": time.Now().UTC()}
	if status == models.JavDownloadBatchSubmitted || status == models.JavDownloadBatchPartial || status == models.JavDownloadBatchCompleted {
		var batch models.JavDownloadBatch
		if err := tx.Select("submitted_at").First(&batch, batchID).Error; err != nil {
			return err
		}
		if batch.SubmittedAt == nil {
			updates["submitted_at"] = time.Now().UTC()
		}
	}
	return tx.Model(&models.JavDownloadBatch{}).Where("id = ?", batchID).Updates(updates).Error
}

// RejectJavDownloadedWork records a failed quality decision and moves the
// work back to magnet review. File deletion is intentionally a separate,
// explicit video-location operation so an accidental verdict cannot destroy a
// valid mirror that happens to share the same JAV code.
func RejectJavDownloadedWork(ctx context.Context, javID, candidateID, attemptID int64, reasons []string, notes string) (*models.JavMagnetCandidate, error) {
	return RejectJavDownloadedWorkWithReview(ctx, javID, candidateID, attemptID, JavMagnetReviewInput{Reasons: reasons, Notes: notes})
}

func RejectJavDownloadedWorkWithReview(ctx context.Context, javID, candidateID, attemptID int64, input JavMagnetReviewInput) (*models.JavMagnetCandidate, error) {
	if attemptID <= 0 {
		var attempt models.JavDownloadAttempt
		err := common.DB.WithContext(ctx).
			Where("jav_id = ?", javID).
			Where("status IN ?", javDownloadAttemptAwaitingResolutionStatuses()).
			Order("id DESC").First(&attempt).Error
		if err == nil {
			if attempt.CandidateID != candidateID {
				return nil, ErrJavDownloadCandidateMismatch
			}
			attemptID = attempt.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find JAV download attempt: %w", err)
		}
	}
	input.Accepted = false
	candidate, err := ReviewJavMagnet(ctx, javID, candidateID, input)
	if err != nil {
		return nil, err
	}
	if attemptID > 0 {
		if _, err := MarkJavDownloadAttempt(ctx, attemptID, models.JavDownloadAttemptRejected, "", strings.TrimSpace(input.Notes)); err != nil {
			return nil, fmt.Errorf("mark rejected JAV download attempt: %w", err)
		}
	}
	return candidate, nil
}

func normalizeJavMagnetHash(raw string) string {
	hash := strings.TrimSpace(raw)
	hash = strings.TrimPrefix(strings.TrimPrefix(hash, "urn:btih:"), "URN:BTIH:")
	return strings.ToLower(hash)
}

func buildJavMagnetURI(hash, name string) string {
	values := url.Values{}
	values.Set("xt", "urn:btih:"+hash)
	if name != "" {
		values.Set("dn", name)
	}
	return "magnet:?" + values.Encode()
}

// SelectJavMagnet records the single candidate chosen for later submission.
func SelectJavMagnet(ctx context.Context, javID, candidateID int64) (*models.JavMagnetSelection, error) {
	if javID <= 0 || candidateID <= 0 {
		return nil, errors.New("jav id and candidate id are required")
	}
	var result models.JavMagnetSelection
	err := withSQLiteTransactionRetry(ctx, common.DB, func(tx *gorm.DB) error {
		var candidate models.JavMagnetCandidate
		if err := tx.Where("id = ? AND jav_id = ?", candidateID, javID).First(&candidate).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrJavMagnetNotFound
			}
			return fmt.Errorf("load selected JAV magnet: %w", err)
		}
		if candidate.ReviewStatus == models.JavMagnetReviewRejected {
			return ErrJavMagnetAlreadyRejected
		}
		if candidate.ReviewStatus == models.JavMagnetReviewAccepted {
			return ErrJavAlreadyQualityAccepted
		}
		var acceptanceCount int64
		if err := tx.Model(&models.JavQualityAcceptance{}).Where("jav_id = ?", javID).Count(&acceptanceCount).Error; err != nil {
			return fmt.Errorf("check JAV quality acceptance: %w", err)
		}
		if acceptanceCount > 0 {
			return ErrJavAlreadyQualityAccepted
		}
		activeAttempt := false
		var attempt models.JavDownloadAttempt
		if err := tx.Where("jav_id = ? AND status IN ?", javID, javDownloadAttemptActiveStatuses()).Order("id DESC").First(&attempt).Error; err == nil {
			if attempt.CandidateID != candidateID {
				return ErrJavDownloadAlreadyActive
			}
			activeAttempt = true
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("check active JAV download attempt: %w", err)
		}
		now := time.Now().UTC()
		result = models.JavMagnetSelection{JavID: javID, CandidateID: candidateID, SelectedAt: now, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "jav_id"}},
			DoUpdates: clause.Assignments(map[string]any{"candidate_id": candidateID, "selected_at": now, "updated_at": now}),
		}).Create(&result).Error; err != nil {
			return fmt.Errorf("save JAV magnet selection: %w", err)
		}
		if err := ensureJavAcquisitionTx(tx, javID, now); err != nil {
			return err
		}
		if !activeAttempt {
			if err := tx.Model(&models.JavAcquisition{}).Where("jav_id = ? AND stage NOT IN ?", javID, []string{models.JavAcquisitionStageDownloadSubmitted, models.JavAcquisitionStageQualityReview, models.JavAcquisitionStageImported}).Update("stage", models.JavAcquisitionStageReadyToDownload).Error; err != nil {
				return fmt.Errorf("advance JAV acquisition after magnet selection: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateJavDownloadBatch queues the currently selected candidate for every
// requested work. It is deliberately transport-neutral; the external service
// can consume these pending attempts later without changing the UI contract.
func CreateJavDownloadBatch(ctx context.Context, javIDs []int64) (*models.JavDownloadBatch, error) {
	ids := uniqueInt64s(javIDs)
	if len(ids) == 0 {
		return nil, errors.New("at least one JAV id is required")
	}
	var batch models.JavDownloadBatch
	err := withSQLiteTransactionRetry(ctx, common.DB, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		batch = models.JavDownloadBatch{Status: models.JavDownloadBatchPending, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&batch).Error; err != nil {
			return fmt.Errorf("create JAV download batch: %w", err)
		}
		for _, javID := range ids {
			var acceptanceCount int64
			if err := tx.Model(&models.JavQualityAcceptance{}).Where("jav_id = ?", javID).Count(&acceptanceCount).Error; err != nil {
				return fmt.Errorf("check JAV quality acceptance: %w", err)
			}
			if acceptanceCount > 0 {
				return fmt.Errorf("jav %d: %w", javID, ErrJavAlreadyQualityAccepted)
			}
			var activeFileCount int64
			if err := tx.Table("video_location vl_send").Joins("JOIN directory d_send ON d_send.id = vl_send.directory_id").Where("vl_send.jav_id = ?", javID).Where(activeLocationWhereSQL("vl_send", "d_send")).Count(&activeFileCount).Error; err != nil {
				return fmt.Errorf("check active JAV file: %w", err)
			}
			if activeFileCount > 0 {
				return fmt.Errorf("jav %d: %w", javID, ErrJavAlreadyHasFile)
			}
			var selection models.JavMagnetSelection
			if err := tx.Where("jav_id = ?", javID).First(&selection).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("jav %d: %w", javID, ErrJavMagnetNoSelection)
				}
				return fmt.Errorf("load JAV magnet selection: %w", err)
			}
			var candidate models.JavMagnetCandidate
			if err := tx.Where("id = ? AND jav_id = ?", selection.CandidateID, javID).First(&candidate).Error; err != nil {
				return fmt.Errorf("load selected JAV magnet: %w", err)
			}
			if candidate.ReviewStatus == models.JavMagnetReviewRejected {
				return fmt.Errorf("jav %d: %w", javID, ErrJavMagnetAlreadyRejected)
			}
			key := fmt.Sprintf("jav:%d:candidate:%d", javID, candidate.ID)
			attempt := models.JavDownloadAttempt{BatchID: batch.ID, JavID: javID, CandidateID: candidate.ID, IdempotencyKey: key, Status: models.JavDownloadAttemptPending, CreatedAt: now}
			if err := tx.Create(&attempt).Error; err != nil {
				var previous models.JavDownloadAttempt
				if loadErr := tx.Where("idempotency_key = ?", key).First(&previous).Error; loadErr != nil {
					return fmt.Errorf("create JAV download attempt: %w", err)
				}
				if previous.Status != models.JavDownloadAttemptFailed && previous.Status != models.JavDownloadAttemptRejected && previous.Status != models.JavDownloadAttemptUncertain {
					return fmt.Errorf("jav %d: %w", javID, ErrJavDownloadAlreadyActive)
				}
				attempt = previous
				if updateErr := tx.Model(&attempt).Updates(map[string]any{
					"batch_id": batch.ID, "status": models.JavDownloadAttemptPending,
					"error": "", "external_task_id": "", "submitted_at": nil, "completed_at": nil,
				}).Error; updateErr != nil {
					return fmt.Errorf("reset failed JAV download attempt: %w", updateErr)
				}
			}
			if err := tx.Model(&models.JavAcquisition{}).Where("jav_id = ?", javID).Update("stage", models.JavAcquisitionStageReadyToDownload).Error; err != nil {
				return fmt.Errorf("mark JAV download ready for hand-off: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return GetJavDownloadBatch(ctx, batch.ID)
}

func GetJavDownloadBatch(ctx context.Context, batchID int64) (*models.JavDownloadBatch, error) {
	if batchID <= 0 {
		return nil, errors.New("batch id must be positive")
	}
	var batch models.JavDownloadBatch
	if err := common.DB.WithContext(ctx).Preload("Attempts").First(&batch, batchID).Error; err != nil {
		return nil, fmt.Errorf("get JAV download batch: %w", err)
	}
	return &batch, nil
}

// ListJavDownloadQueue lists works with a saved selection and no accepted
// quality result. The latest attempt is included for transparent retry UI.
func ListJavDownloadQueue(ctx context.Context, limit, offset int) ([]JavMagnetQueueItem, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	base := common.DB.WithContext(ctx).
		Table("jav_magnet_selection s").
		Joins("JOIN jav j ON j.id = s.jav_id").
		Joins("JOIN jav_magnet_candidate c ON c.id = s.candidate_id").
		Where("NOT EXISTS (SELECT 1 FROM jav_quality_acceptance qa WHERE qa.jav_id = s.jav_id)").
		Where("NOT EXISTS (SELECT 1 FROM video_location vl_queue JOIN directory d_queue ON d_queue.id = vl_queue.directory_id WHERE vl_queue.jav_id = s.jav_id AND "+activeLocationWhereSQL("vl_queue", "d_queue")+")").
		Where("NOT EXISTS (SELECT 1 FROM jav_download_attempt ja_queue WHERE ja_queue.jav_id = s.jav_id AND ja_queue.candidate_id = s.candidate_id AND ja_queue.status IN ?)", []string{
			models.JavDownloadAttemptPending,
			models.JavDownloadAttemptSubmitted,
			models.JavDownloadAttemptDownloaded,
			models.JavDownloadAttemptAwaitingQuality,
			models.JavDownloadAttemptAccepted,
		})
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count JAV download queue: %w", err)
	}
	type row struct{ JavID, CandidateID int64 }
	var rows []row
	if err := base.Select("s.jav_id, s.candidate_id").Order("s.updated_at DESC, s.jav_id DESC").Limit(limit).Offset(offset).Scan(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list JAV download queue: %w", err)
	}
	items := make([]JavMagnetQueueItem, 0, len(rows))
	for _, row := range rows {
		var item JavMagnetQueueItem
		if err := common.DB.WithContext(ctx).Preload("Studio").Preload("Series").Preload("Idols").First(&item.Jav, row.JavID).Error; err != nil {
			return nil, 0, fmt.Errorf("load queued JAV: %w", err)
		}
		if err := common.DB.WithContext(ctx).Where("id = ?", row.CandidateID).First(&item.Candidate).Error; err != nil {
			return nil, 0, fmt.Errorf("load queued candidate: %w", err)
		}
		if err := common.DB.WithContext(ctx).Where("jav_id = ?", row.JavID).First(&item.Selection).Error; err != nil {
			return nil, 0, fmt.Errorf("load queued selection: %w", err)
		}
		var attempt models.JavDownloadAttempt
		if err := common.DB.WithContext(ctx).Where("jav_id = ? AND candidate_id = ?", row.JavID, row.CandidateID).Order("id DESC").First(&attempt).Error; err == nil {
			item.Attempt = &attempt
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, fmt.Errorf("load queued attempt: %w", err)
		}
		items = append(items, item)
	}
	return items, total, nil
}

type JavMagnetReviewInput struct {
	QualityClear   *bool
	Confirmed1080P *bool
	HasIntroAd     *bool
	HasWatermark   *bool
	HasMarquee     *bool
	IsUncensored   *bool
	Reasons        []string
	Notes          string
	Accepted       bool
}

// ReviewJavMagnet stores the structured quality verdict without deleting the
// candidate. A rejected candidate remains visible in the historical section.
func ReviewJavMagnet(ctx context.Context, javID, candidateID int64, input JavMagnetReviewInput) (*models.JavMagnetCandidate, error) {
	if javID <= 0 || candidateID <= 0 {
		return nil, errors.New("jav id and candidate id are required")
	}
	if input.Accepted {
		return nil, errors.New("formal quality acceptance requires a downloaded file")
	}
	var candidate models.JavMagnetCandidate
	err := withSQLiteTransactionRetry(ctx, common.DB, func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND jav_id = ?", candidateID, javID).First(&candidate).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrJavMagnetNotFound
			}
			return err
		}
		if candidate.ReviewStatus == models.JavMagnetReviewAccepted {
			return ErrJavAlreadyQualityAccepted
		}
		status := models.JavMagnetReviewRejected
		now := time.Now().UTC()
		updates := map[string]any{"review_status": status, "quality_clear": input.QualityClear, "confirmed1080_p": input.Confirmed1080P, "has_intro_ad": input.HasIntroAd, "has_watermark": input.HasWatermark, "has_marquee": input.HasMarquee, "is_uncensored": input.IsUncensored, "review_reasons": strings.Join(uniqueStrings(input.Reasons), ","), "review_notes": strings.TrimSpace(input.Notes), "reviewed_at": now, "updated_at": now}
		if err := tx.Model(&candidate).Updates(updates).Error; err != nil {
			return fmt.Errorf("save JAV magnet review: %w", err)
		}
		if err := tx.Where("jav_id = ? AND candidate_id = ?", javID, candidateID).Delete(&models.JavMagnetSelection{}).Error; err != nil {
			return fmt.Errorf("clear rejected JAV magnet selection: %w", err)
		}
		stage := models.JavAcquisitionStageMagnetReview
		if err := tx.Model(&models.JavAcquisition{}).Where("jav_id = ?", javID).Update("stage", stage).Error; err != nil {
			return fmt.Errorf("update JAV magnet review stage: %w", err)
		}
		return tx.Where("id = ?", candidateID).First(&candidate).Error
	})
	if err != nil {
		return nil, err
	}
	return &candidate, nil
}

// AcceptJavDownloadedWork promotes a physically present file to the formal
// library only after the user confirms quality. The active location is looked
// up server-side so a stale UI cannot accept a missing or replaced file.
func AcceptJavDownloadedWork(ctx context.Context, javID, candidateID, attemptID int64, notes string) (*models.JavQualityAcceptance, error) {
	return AcceptJavDownloadedWorkWithReview(ctx, javID, candidateID, attemptID, JavMagnetReviewInput{Accepted: true, Notes: notes})
}

// AcceptJavDownloadedWorkWithReview records structured quality facts together
// with the formal acceptance. Unlike a candidate review, this operation is
// rejected unless an active file is present.
func AcceptJavDownloadedWorkWithReview(ctx context.Context, javID, candidateID, attemptID int64, input JavMagnetReviewInput) (*models.JavQualityAcceptance, error) {
	if javID <= 0 || candidateID <= 0 {
		return nil, errors.New("jav id and candidate id are required")
	}
	input.Accepted = true
	var acceptance models.JavQualityAcceptance
	err := withSQLiteTransactionRetry(ctx, common.DB, func(tx *gorm.DB) error {
		attemptBatchID := int64(0)
		var existing models.JavQualityAcceptance
		if err := tx.Where("jav_id = ?", javID).First(&existing).Error; err == nil {
			if existing.CandidateID != candidateID {
				return ErrJavAlreadyQualityAccepted
			}
			acceptance = existing
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find existing JAV quality acceptance: %w", err)
		}
		var candidate models.JavMagnetCandidate
		if err := tx.Where("id = ? AND jav_id = ?", candidateID, javID).First(&candidate).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrJavMagnetNotFound
			}
			return fmt.Errorf("find JAV magnet candidate: %w", err)
		}
		var locations []struct {
			ID      int64
			VideoID int64
		}
		if err := tx.Table("video_location vl").Select("vl.id, vl.video_id").Joins("JOIN directory d ON d.id = vl.directory_id").Where("vl.jav_id = ?", javID).Where(activeLocationWhereSQL("vl", "d")).Order("vl.id DESC").Find(&locations).Error; err != nil {
			return fmt.Errorf("find downloaded JAV file: %w", err)
		}
		if len(locations) == 0 {
			return ErrJavDownloadNoFile
		}
		if len(locations) > 1 {
			return ErrJavDownloadAmbiguousFile
		}
		location := locations[0]
		if candidate.ReviewStatus == models.JavMagnetReviewRejected {
			return ErrJavMagnetAlreadyRejected
		}
		now := time.Now().UTC()
		var attempt *int64
		if attemptID <= 0 {
			var latest models.JavDownloadAttempt
			if err := tx.Where("jav_id = ?", javID).
				Where("status IN ?", javDownloadAttemptAwaitingResolutionStatuses()).
				Order("id DESC").First(&latest).Error; err == nil {
				if latest.CandidateID != candidateID {
					return ErrJavDownloadCandidateMismatch
				}
				attemptID = latest.ID
				attemptBatchID = latest.BatchID
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("find JAV download attempt: %w", err)
			}
		}
		if attemptID <= 0 {
			return ErrJavDownloadAttemptRequired
		}
		if attemptID > 0 {
			if attemptBatchID == 0 {
				var attemptRow models.JavDownloadAttempt
				if err := tx.Select("batch_id").Where("id = ? AND jav_id = ? AND candidate_id = ?", attemptID, javID, candidateID).First(&attemptRow).Error; err != nil {
					return fmt.Errorf("find JAV download attempt batch: %w", err)
				}
				attemptBatchID = attemptRow.BatchID
			}
			attempt = &attemptID
		}
		var videoID, locationID *int64
		videoID, locationID = &location.VideoID, &location.ID
		acceptance = models.JavQualityAcceptance{JavID: javID, CandidateID: candidateID, AttemptID: attempt, VideoID: videoID, LocationID: locationID, AcceptedAt: now, Notes: strings.TrimSpace(input.Notes), CreatedAt: now, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "jav_id"}}, DoUpdates: clause.Assignments(map[string]any{"candidate_id": candidateID, "attempt_id": attempt, "video_id": videoID, "location_id": locationID, "accepted_at": now, "notes": acceptance.Notes, "updated_at": now})}).Create(&acceptance).Error; err != nil {
			return fmt.Errorf("save JAV quality acceptance: %w", err)
		}
		if err := tx.Model(&models.JavMagnetCandidate{}).Where("id = ? AND jav_id = ?", candidateID, javID).Updates(map[string]any{
			"review_status":   models.JavMagnetReviewAccepted,
			"quality_clear":   input.QualityClear,
			"confirmed1080_p": input.Confirmed1080P,
			"has_intro_ad":    input.HasIntroAd,
			"has_watermark":   input.HasWatermark,
			"has_marquee":     input.HasMarquee,
			"is_uncensored":   input.IsUncensored,
			"review_reasons":  strings.Join(uniqueStrings(input.Reasons), ","),
			"reviewed_at":     now,
			"review_notes":    acceptance.Notes,
			"updated_at":      now,
		}).Error; err != nil {
			return fmt.Errorf("mark accepted JAV magnet: %w", err)
		}
		if attemptID > 0 {
			if err := tx.Model(&models.JavDownloadAttempt{}).Where("id = ? AND jav_id = ? AND candidate_id = ?", attemptID, javID, candidateID).Updates(map[string]any{"status": models.JavDownloadAttemptAccepted, "completed_at": now}).Error; err != nil {
				return fmt.Errorf("mark JAV download accepted: %w", err)
			}
			if err := aggregateJavDownloadBatchTx(tx, attemptBatchID); err != nil {
				return err
			}
		}
		if err := tx.Model(&models.JavAcquisition{}).Where("jav_id = ?", javID).Updates(map[string]any{"stage": models.JavAcquisitionStageImported, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("mark JAV imported after quality review: %w", err)
		}
		return tx.Where("jav_id = ?", javID).First(&acceptance).Error
	})
	if err != nil {
		return nil, err
	}
	return &acceptance, nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

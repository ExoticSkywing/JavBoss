package db

import (
	"errors"
	"fmt"
	"time"

	"javboss/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// reconcileJavAcquisitionStagesTx keeps the durable workflow stage consistent
// with the file-backed inventory without inventing a second work entity.
//
// An active location is authoritative for imported. When the last active
// location disappears, only an acquisition that was actually imported is
// moved back: scraped works resume magnet collection, while bare codes resume
// metadata collection. Metadata/magnet stages are otherwise deliberately left
// untouched so a normal visibility change cannot rewind unfinished work.
func reconcileJavAcquisitionStagesTx(tx *gorm.DB, javIDs []int64) error {
	if tx == nil {
		return errors.New("tx is nil")
	}
	ids := uniqueInt64s(javIDs)
	if len(ids) == 0 {
		return nil
	}
	// A hidden location can become active again when a directory is restored or
	// a scan re-discovers a path.  Check the complete active set before changing
	// lifecycle state so a previously hidden, different media asset cannot
	// silently join the same canonical work.  Returning the sentinel keeps the
	// caller able to report a media conflict and the surrounding transaction
	// rolls back the attempted reactivation.
	if err := ensureNoActiveJavMediaConflictsTx(tx, ids); err != nil {
		return err
	}
	if err := finalizeAwaitingScanJavImportsTx(tx, ids); err != nil {
		return err
	}

	active := tx.Table("video_location vl_reconcile").
		Select("1").
		Joins("JOIN directory d_reconcile ON d_reconcile.id = vl_reconcile.directory_id").
		Where("vl_reconcile.jav_id = jav_acquisition.jav_id").
		Where(activeLocationWhereSQL("vl_reconcile", "d_reconcile"))

	now := time.Now().UTC()
	// A file created through the cloud-download path is physically present,
	// but it is not a formal import until quality review passes. Keep that
	// distinction durable while legacy/manual scan discoveries still become
	// imported immediately. The latest attempt is authoritative here: a
	// rejected candidate whose file was intentionally retained (or could not
	// be moved to trash) must stay in magnet_review, never become a legacy
	// import merely because the file is still visible to the scanner.
	qualityPending := tx.Table("jav_download_attempt ja_reconcile").
		Select("1").
		Where("ja_reconcile.jav_id = jav_acquisition.jav_id").
		Where("ja_reconcile.id = (SELECT MAX(ja_latest.id) FROM jav_download_attempt ja_latest WHERE ja_latest.jav_id = jav_acquisition.jav_id)").
		Where("ja_reconcile.status IN ?", javDownloadAttemptAwaitingResolutionStatuses())
	formalAcceptance := tx.Table("jav_quality_acceptance qa_reconcile").
		Select("1").
		Where("qa_reconcile.jav_id = jav_acquisition.jav_id")
	latestRejected := tx.Table("jav_download_attempt ja_rejected").
		Select("1").
		Where("ja_rejected.jav_id = jav_acquisition.jav_id").
		Where("ja_rejected.id = (SELECT MAX(ja_rejected_latest.id) FROM jav_download_attempt ja_rejected_latest WHERE ja_rejected_latest.jav_id = jav_acquisition.jav_id)").
		Where("ja_rejected.status = ?", models.JavDownloadAttemptRejected)
	if err := tx.Model(&models.JavAcquisition{}).
		Where("jav_id IN ?", ids).
		Where("stage <> ?", models.JavAcquisitionStageImported).
		Where("EXISTS (?)", active).
		Where("NOT EXISTS (?)", formalAcceptance).
		Where("EXISTS (?)", qualityPending).
		Updates(map[string]any{
			"stage":      models.JavAcquisitionStageQualityReview,
			"updated_at": now,
		}).Error; err != nil {
		return fmt.Errorf("mark downloaded JAV acquisitions awaiting quality review: %w", err)
	}
	if err := tx.Model(&models.JavAcquisition{}).
		Where("jav_id IN ?", ids).
		Where("stage <> ?", models.JavAcquisitionStageImported).
		Where("EXISTS (?)", active).
		Where("NOT EXISTS (?)", formalAcceptance).
		Where("EXISTS (?)", latestRejected).
		Updates(map[string]any{
			"stage":      models.JavAcquisitionStageMagnetReview,
			"updated_at": now,
		}).Error; err != nil {
		return fmt.Errorf("keep rejected JAV acquisitions out of formal imports: %w", err)
	}
	if err := tx.Model(&models.JavAcquisition{}).
		Where("jav_id IN ?", ids).
		Where("stage <> ?", models.JavAcquisitionStageImported).
		Where("EXISTS (?)", active).
		Where("EXISTS (?) OR (NOT EXISTS (?) AND NOT EXISTS (?))", formalAcceptance, qualityPending, latestRejected).
		Updates(map[string]any{
			"stage":      models.JavAcquisitionStageImported,
			"updated_at": now,
		}).Error; err != nil {
		return fmt.Errorf("mark active JAV acquisitions imported: %w", err)
	}

	metadata := tx.Table("jav j_reconcile").
		Select("1").
		Where("j_reconcile.id = jav_acquisition.jav_id").
		Where(`TRIM(COALESCE(j_reconcile.title, '')) <> '' OR
			COALESCE(CAST(strftime('%s', j_reconcile.fetched_at) AS INTEGER), 0) > 0 OR
			(typeof(j_reconcile.fetched_at) IN ('integer', 'real') AND
				CAST(j_reconcile.fetched_at AS REAL) > 0)`)

	if err := tx.Model(&models.JavAcquisition{}).
		Where("jav_id IN ?", ids).
		Where("stage = ?", models.JavAcquisitionStageImported).
		Where("NOT EXISTS (?)", active).
		Where("EXISTS (?)", metadata).
		Updates(map[string]any{
			"stage":      models.JavAcquisitionStageMagnetCollecting,
			"updated_at": now,
		}).Error; err != nil {
		return fmt.Errorf("resume magnet collection for unlinked JAV acquisitions: %w", err)
	}

	if err := tx.Model(&models.JavAcquisition{}).
		Where("jav_id IN ?", ids).
		Where("stage = ?", models.JavAcquisitionStageImported).
		Where("NOT EXISTS (?)", active).
		Where("NOT EXISTS (?)", metadata).
		Updates(map[string]any{
			"stage":      models.JavAcquisitionStageMetadataPending,
			"updated_at": now,
		}).Error; err != nil {
		return fmt.Errorf("resume metadata collection for unlinked JAV acquisitions: %w", err)
	}
	return nil
}

// finalizeAwaitingScanJavImportsTx is the only top-down path that creates the
// formal acceptance record. Human review has already promoted the file out of
// staging; this step runs only after the scanner links a real formal-library
// VideoLocation to the same canonical work.
func finalizeAwaitingScanJavImportsTx(tx *gorm.DB, javIDs []int64) error {
	type awaitingRow struct {
		AttemptID   int64  `gorm:"column:attempt_id"`
		BatchID     int64  `gorm:"column:batch_id"`
		JavID       int64  `gorm:"column:jav_id"`
		CandidateID int64  `gorm:"column:candidate_id"`
		Notes       string `gorm:"column:notes"`
	}
	var attempts []awaitingRow
	if err := tx.Table("jav_download_attempt a_finalize").
		Select("a_finalize.id AS attempt_id, a_finalize.batch_id, a_finalize.jav_id, a_finalize.candidate_id, c_finalize.review_notes AS notes").
		Joins("JOIN jav_magnet_candidate c_finalize ON c_finalize.id = a_finalize.candidate_id AND c_finalize.jav_id = a_finalize.jav_id").
		Where("a_finalize.jav_id IN ?", javIDs).
		Where("a_finalize.status = ?", models.JavDownloadAttemptAwaitingScan).
		Where("c_finalize.review_status = ?", models.JavMagnetReviewAccepted).
		Where("a_finalize.id = (SELECT MAX(a_latest.id) FROM jav_download_attempt a_latest WHERE a_latest.jav_id = a_finalize.jav_id)").
		Find(&attempts).Error; err != nil {
		return fmt.Errorf("list JAV downloads awaiting scan finalization: %w", err)
	}
	for _, attempt := range attempts {
		var location struct {
			ID      int64 `gorm:"column:id"`
			VideoID int64 `gorm:"column:video_id"`
		}
		query := tx.Table("video_location vl_finalize").
			Select("vl_finalize.id, vl_finalize.video_id").
			Joins("JOIN directory d_finalize ON d_finalize.id = vl_finalize.directory_id").
			Where("vl_finalize.jav_id = ?", attempt.JavID).
			Where(activeLocationWhereSQL("vl_finalize", "d_finalize")).
			Order("vl_finalize.id DESC").
			First(&location)
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			continue
		}
		if query.Error != nil {
			return fmt.Errorf("find scanned JAV location for finalization: %w", query.Error)
		}
		now := time.Now().UTC()
		attemptID := attempt.AttemptID
		videoID := location.VideoID
		locationID := location.ID
		acceptance := models.JavQualityAcceptance{
			JavID: attempt.JavID, CandidateID: attempt.CandidateID,
			AttemptID: &attemptID, VideoID: &videoID, LocationID: &locationID,
			AcceptedAt: now, Notes: attempt.Notes, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "jav_id"}}, DoNothing: true}).Create(&acceptance).Error; err != nil {
			return fmt.Errorf("finalize scanned JAV quality acceptance: %w", err)
		}
		if err := tx.Model(&models.JavDownloadAttempt{}).Where("id = ?", attempt.AttemptID).Updates(map[string]any{
			"status": models.JavDownloadAttemptAccepted, "completed_at": now, "error": "",
		}).Error; err != nil {
			return fmt.Errorf("mark scanned JAV download accepted: %w", err)
		}
		if err := tx.Model(&models.JavAcquisition{}).Where("jav_id = ?", attempt.JavID).Updates(map[string]any{
			"stage": models.JavAcquisitionStageImported, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("mark scanned JAV acquisition imported: %w", err)
		}
		if err := aggregateJavDownloadBatchTx(tx, attempt.BatchID); err != nil {
			return err
		}
	}
	return nil
}

// ensureNoActiveJavMediaConflictsTx rejects a canonical work that has more
// than one distinct active video fingerprint/entity.  Mirror locations for
// the same Video are valid; different Video rows are not.
func ensureNoActiveJavMediaConflictsTx(tx *gorm.DB, javIDs []int64) error {
	if tx == nil {
		return errors.New("tx is nil")
	}
	ids := uniqueInt64s(javIDs)
	if len(ids) == 0 {
		return nil
	}
	type conflictRow struct {
		JavID      int64 `gorm:"column:jav_id"`
		MediaCount int64 `gorm:"column:media_count"`
	}
	var conflicts []conflictRow
	if err := tx.
		Table("video_location vl_conflict").
		Select("vl_conflict.jav_id, COUNT(DISTINCT vl_conflict.video_id) AS media_count").
		Joins("JOIN directory d_conflict ON d_conflict.id = vl_conflict.directory_id").
		Where("vl_conflict.jav_id IN ?", ids).
		Where(activeLocationWhereSQL("vl_conflict", "d_conflict")).
		Group("vl_conflict.jav_id").
		Having("COUNT(DISTINCT vl_conflict.video_id) > 1").
		Scan(&conflicts).Error; err != nil {
		return fmt.Errorf("check active JAV media conflicts: %w", err)
	}
	if len(conflicts) == 0 {
		return nil
	}
	return fmt.Errorf("%w: jav_id=%d has %d active media assets", ErrJavMediaConflict, conflicts[0].JavID, conflicts[0].MediaCount)
}

func javIDsForVideoLocationsTx(tx *gorm.DB, where string, args ...any) ([]int64, error) {
	if tx == nil {
		return nil, errors.New("tx is nil")
	}
	var ids []int64
	query := tx.Model(&models.VideoLocation{}).
		Where("jav_id IS NOT NULL").
		Distinct()
	if where != "" {
		query = query.Where(where, args...)
	}
	query = query.Pluck("jav_id", &ids)
	if query.Error != nil {
		return nil, fmt.Errorf("list affected JAV acquisitions: %w", query.Error)
	}
	return uniqueInt64s(ids), nil
}

func javIDsForDirectoryTx(tx *gorm.DB, directoryID int64) ([]int64, error) {
	return javIDsForVideoLocationsTx(tx, "directory_id = ?", directoryID)
}

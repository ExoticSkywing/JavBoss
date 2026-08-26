package db

import (
	"errors"
	"fmt"
	"time"

	"javboss/internal/models"

	"gorm.io/gorm"
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

	active := tx.Table("video_location vl_reconcile").
		Select("1").
		Joins("JOIN directory d_reconcile ON d_reconcile.id = vl_reconcile.directory_id").
		Where("vl_reconcile.jav_id = jav_acquisition.jav_id").
		Where(activeLocationWhereSQL("vl_reconcile", "d_reconcile"))

	now := time.Now().UTC()
	if err := tx.Model(&models.JavAcquisition{}).
		Where("jav_id IN ?", ids).
		Where("stage <> ?", models.JavAcquisitionStageImported).
		Where("EXISTS (?)", active).
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

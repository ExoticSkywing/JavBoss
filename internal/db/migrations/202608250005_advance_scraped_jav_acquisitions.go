package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202608250005_advance_scraped_jav_acquisitions.go",
		advanceScrapedJavAcquisitions,
		irreversibleMigration,
	)
}

// advanceScrapedJavAcquisitions repairs acquisitions that already received
// metadata before the unified lifecycle began advancing this stage at runtime.
// Inventory is derived from active locations, so this migration also repairs
// the inverse drift (an imported stage whose last location disappeared before
// the runtime reconciliation hooks were installed).
func advanceScrapedJavAcquisitions(ctx context.Context, tx *sql.Tx) error {
	const activeLocation = `
		EXISTS (
			SELECT 1
			FROM video_location vl
			JOIN directory d ON d.id = vl.directory_id
			WHERE vl.jav_id = jav_acquisition.jav_id
				AND COALESCE(vl.is_delete, 0) = 0
				AND COALESCE(d.is_delete, 0) = 0
		)`
	const metadata = `
		EXISTS (
			SELECT 1
			FROM jav j
			WHERE j.id = jav_acquisition.jav_id
			AND (
				TRIM(COALESCE(j.title, '')) <> ''
				OR COALESCE(CAST(strftime('%s', j.fetched_at) AS INTEGER), 0) > 0
				OR (typeof(j.fetched_at) IN ('integer', 'real') AND CAST(j.fetched_at AS REAL) > 0)
			)
		)`

	if _, err := tx.ExecContext(ctx, `
		UPDATE jav_acquisition
		SET stage = 'imported',
			updated_at = CURRENT_TIMESTAMP
		WHERE stage <> 'imported'
			AND `+activeLocation+`;`); err != nil {
		return fmt.Errorf("mark active JAV acquisitions imported: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE jav_acquisition
		SET stage = 'magnet_collecting',
			updated_at = CURRENT_TIMESTAMP
		WHERE stage IN ('metadata_pending', 'imported')
			AND NOT (`+activeLocation+`)
			AND `+metadata+`;`); err != nil {
		return fmt.Errorf("advance metadata-backed JAV acquisitions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE jav_acquisition
		SET stage = 'metadata_pending',
			updated_at = CURRENT_TIMESTAMP
		WHERE stage = 'imported'
			AND NOT (`+activeLocation+`)
			AND NOT (`+metadata+`);`); err != nil {
		return fmt.Errorf("reset metadata-less JAV acquisitions: %w", err)
	}
	return nil
}

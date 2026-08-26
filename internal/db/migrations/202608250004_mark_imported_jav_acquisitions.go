package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202608250004_mark_imported_jav_acquisitions.go",
		markImportedJavAcquisitions,
		irreversibleMigration,
	)
}

// markImportedJavAcquisitions reconciles historical accepted input with the
// file-backed inventory. The runtime linker keeps this stage current for new
// file associations; inventory itself remains derived from active locations.
func markImportedJavAcquisitions(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE jav_acquisition
		SET stage = 'imported', updated_at = CURRENT_TIMESTAMP
		WHERE stage <> 'imported'
			AND EXISTS (
				SELECT 1
				FROM video_location vl
				JOIN directory d ON d.id = vl.directory_id
				WHERE vl.jav_id = jav_acquisition.jav_id
					AND COALESCE(vl.is_delete, 0) = 0
					AND COALESCE(d.is_delete, 0) = 0
			)
	`); err != nil {
		return fmt.Errorf("mark file-backed JAV acquisitions imported: %w", err)
	}
	return nil
}

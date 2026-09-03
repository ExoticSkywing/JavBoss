package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202609010001_refresh_official_sample_image_misses.go",
		refreshOfficialSampleImageMisses,
		irreversibleMigration,
	)
}

// refreshOfficialSampleImageMisses retries misses recorded before JavBoss
// learned how to read public galleries from original studio sites.
func refreshOfficialSampleImageMisses(ctx context.Context, tx *sql.Tx) error {
	return execDB(
		ctx,
		tx,
		`UPDATE jav
		 SET sample_images = '[]', updated_at = CURRENT_TIMESTAMP
		 WHERE sample_images = '[{"thumbnail_url":":not_found","detail_url":":not_found"}]'`,
	)
}

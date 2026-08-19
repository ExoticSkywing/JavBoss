package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202608180003_add_clouddrive_local_concurrency.go",
		addCloudDriveLocalConcurrency,
		irreversibleMigration,
	)
}

func addCloudDriveLocalConcurrency(ctx context.Context, tx *sql.Tx) error {
	return addColumnIfMissing(
		ctx,
		tx,
		"cloud_drive_settings",
		"local_concurrency",
		"integer NOT NULL DEFAULT 2",
	)
}

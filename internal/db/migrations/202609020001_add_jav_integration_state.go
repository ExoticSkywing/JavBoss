package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202609020001_add_jav_integration_state.go",
		addJavIntegrationState,
		irreversibleMigration,
	)
}

func addJavIntegrationState(ctx context.Context, tx *sql.Tx) error {
	return execStatements(ctx, tx,
		`ALTER TABLE jav_input_batch ADD COLUMN source text NOT NULL DEFAULT "web"`,
		`ALTER TABLE jav_input_batch ADD COLUMN external_request_id text`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_jav_input_batch_source_external_request ON jav_input_batch(source, external_request_id)`,
		`ALTER TABLE jav_download_attempt ADD COLUMN result_paths text NOT NULL DEFAULT []`,
	)
}

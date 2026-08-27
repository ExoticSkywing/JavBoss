package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext("202608270001_requeue_javdb_idol_names.go", requeueJavDBIdolNames, irreversibleMigration)
}

// Re-run JavDB identity reconciliation once so databases processed before
// JavDB became the authoritative display-name source receive the same policy.
func requeueJavDBIdolNames(ctx context.Context, tx *sql.Tx) error {
	return execDB(ctx, tx, `UPDATE jav SET jav_db_idols_reconciled_at = NULL WHERE jav_db_idols_reconciled_at IS NOT NULL`)
}

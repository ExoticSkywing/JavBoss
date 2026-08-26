package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext("202608260002_add_javdb_idol_reconciliation.go", addJavDBIdolReconciliation, irreversibleMigration)
}

func addJavDBIdolReconciliation(ctx context.Context, tx *sql.Tx) error {
	if err := addColumnIfMissing(ctx, tx, "jav", "jav_db_idols_reconciled_at", "datetime"); err != nil {
		return err
	}
	return execDB(ctx, tx, `CREATE INDEX IF NOT EXISTS idx_jav_jav_db_idols_reconciled_at ON jav(jav_db_idols_reconciled_at)`)
}

package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202608310001_add_jav_code_aliases.go",
		addJavCodeAliases,
		irreversibleMigration,
	)
}

func addJavCodeAliases(ctx context.Context, tx *sql.Tx) error {
	return execStatements(ctx, tx,
		`CREATE TABLE IF NOT EXISTS "jav_code_alias" (
			id integer PRIMARY KEY AUTOINCREMENT,
			jav_id integer NOT NULL,
			normalized_code text NOT NULL,
			code text NOT NULL DEFAULT "",
			created_at datetime,
			CONSTRAINT fk_jav_code_alias_jav FOREIGN KEY (jav_id) REFERENCES jav(id) ON UPDATE CASCADE ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_code_alias_jav_id ON jav_code_alias(jav_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_jav_code_alias_normalized_code ON jav_code_alias(normalized_code)`,
	)
}

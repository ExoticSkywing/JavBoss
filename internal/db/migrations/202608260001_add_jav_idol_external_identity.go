package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext("202608260001_add_jav_idol_external_identity.go", addJavIdolExternalIdentity, irreversibleMigration)
}

func addJavIdolExternalIdentity(ctx context.Context, tx *sql.Tx) error {
	return execStatements(ctx, tx,
		`CREATE TABLE IF NOT EXISTS "jav_idol_external_identity" (
			id integer PRIMARY KEY AUTOINCREMENT,
			jav_idol_id integer NOT NULL,
			provider integer NOT NULL,
			external_id text NOT NULL,
			profile_url text,
			created_at datetime,
			CONSTRAINT fk_jav_idol_external_identity_jav_idol FOREIGN KEY (jav_idol_id) REFERENCES jav_idol(id) ON UPDATE CASCADE ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_idol_external_identity_jav_idol_id ON jav_idol_external_identity(jav_idol_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_jav_idol_external_identity_provider_key ON jav_idol_external_identity(provider, external_id)`,
		`CREATE TABLE IF NOT EXISTS "jav_idol_redirect" (
			source_id integer PRIMARY KEY,
			canonical_id integer NOT NULL,
			created_at datetime
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_idol_redirect_canonical_id ON jav_idol_redirect(canonical_id)`,
	)
}

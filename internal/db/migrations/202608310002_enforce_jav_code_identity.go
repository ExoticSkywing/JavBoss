package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202608310002_enforce_jav_code_identity.go",
		enforceJavCodeIdentity,
		irreversibleMigration,
	)
}

func enforceJavCodeIdentity(ctx context.Context, tx *sql.Tx) error {
	return execStatements(ctx, tx,
		// Canonical identities win over aliases if an older application version
		// managed to create a cross-table collision. Self aliases produced by an
		// A -> B -> A correction are redundant and are removed by the same repair.
		`DELETE FROM jav_code_alias
		 WHERE EXISTS (
			SELECT 1 FROM jav
			WHERE jav.normalized_code = jav_code_alias.normalized_code
		)`,
		`CREATE TRIGGER IF NOT EXISTS trg_jav_code_reject_alias_insert
		 BEFORE INSERT ON jav
		 WHEN EXISTS (
			SELECT 1 FROM jav_code_alias
			WHERE normalized_code = NEW.normalized_code
		 )
		 BEGIN
			SELECT RAISE(ABORT, 'JAV canonical code conflicts with an alias');
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_jav_code_reject_alias_update
		 BEFORE UPDATE OF normalized_code ON jav
		 WHEN NEW.normalized_code <> OLD.normalized_code
		  AND EXISTS (
			SELECT 1 FROM jav_code_alias
			WHERE normalized_code = NEW.normalized_code
		  )
		 BEGIN
			SELECT RAISE(ABORT, 'JAV canonical code conflicts with an alias');
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_jav_alias_reject_code_insert
		 BEFORE INSERT ON jav_code_alias
		 WHEN EXISTS (
			SELECT 1 FROM jav
			WHERE normalized_code = NEW.normalized_code
		 )
		 BEGIN
			SELECT RAISE(ABORT, 'JAV alias conflicts with a canonical code');
		 END`,
		`CREATE TRIGGER IF NOT EXISTS trg_jav_alias_reject_code_update
		 BEFORE UPDATE OF normalized_code ON jav_code_alias
		 WHEN EXISTS (
			SELECT 1 FROM jav
			WHERE normalized_code = NEW.normalized_code
		 )
		 BEGIN
			SELECT RAISE(ABORT, 'JAV alias conflicts with a canonical code');
		 END`,
		`UPDATE jav_acquisition
		 SET stage = 'code_review', updated_at = CURRENT_TIMESTAMP
		 WHERE stage = 'magnet_collecting'
		   AND EXISTS (
			SELECT 1 FROM jav
			WHERE jav.id = jav_acquisition.jav_id
			  AND TRIM(COALESCE(jav.title, '')) = ''
		   )
		   AND NOT EXISTS (
			SELECT 1 FROM jav_magnet_candidate
			WHERE jav_magnet_candidate.jav_id = jav_acquisition.jav_id
		   )
		   AND NOT EXISTS (
			SELECT 1 FROM video_location
			WHERE video_location.jav_id = jav_acquisition.jav_id
		   )`,
	)
}

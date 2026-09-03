package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202608310003_repair_placeholder_jav_metadata.go",
		repairPlaceholderJavMetadata,
		irreversibleMigration,
	)
}

// repairPlaceholderJavMetadata removes provider shell-page values written by
// older builds (for example the JavDB login page title “登入”) and makes those
// works eligible for a fresh metadata pass. User tags and accepted/imported
// workflow state are preserved.
func repairPlaceholderJavMetadata(ctx context.Context, tx *sql.Tx) error {
	const placeholder = `LOWER(TRIM(COALESCE(jav.title, ''))) IN ('登入', '登录', 'login', 'sign in', 'signin', 'register', 'age verification', 'driver verification', 'verification required', 'just a moment', 'access denied', 'captcha')`
	return execStatements(ctx, tx,
		`DELETE FROM jav_idol_map
		 WHERE jav_id IN (SELECT jav.id FROM jav WHERE `+placeholder+`)`,
		`DELETE FROM jav_tag_map
		 WHERE provider <> 3
		   AND jav_id IN (SELECT jav.id FROM jav WHERE `+placeholder+`)`,
		`UPDATE jav_acquisition
		 SET stage = 'metadata_pending', updated_at = CURRENT_TIMESTAMP
		 WHERE jav_id IN (SELECT jav.id FROM jav WHERE `+placeholder+`)
		   AND stage NOT IN ('imported', 'quality_review', 'download_submitted', 'ready_to_download')
		   AND NOT EXISTS (SELECT 1 FROM jav_magnet_candidate mc WHERE mc.jav_id = jav_acquisition.jav_id)
		   AND NOT EXISTS (SELECT 1 FROM jav_download_attempt da WHERE da.jav_id = jav_acquisition.jav_id)
		   AND NOT EXISTS (SELECT 1 FROM jav_quality_acceptance qa WHERE qa.jav_id = jav_acquisition.jav_id)
		   AND NOT EXISTS (SELECT 1 FROM video_location vl WHERE vl.jav_id = jav_acquisition.jav_id)`,
		`UPDATE jav
		 SET title = '', studio_id = NULL, series_id = NULL, series_en_id = NULL,
		     release_unix = 0, duration_min = 0, fetched_at = NULL,
		     is_uncensored = NULL, sample_images = '[]',
		     jav_db_idols_reconciled_at = NULL, updated_at = CURRENT_TIMESTAMP
		 WHERE `+placeholder,
	)
}

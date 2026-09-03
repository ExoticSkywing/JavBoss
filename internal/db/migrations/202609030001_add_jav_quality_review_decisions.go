package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202609030001_add_jav_quality_review_decisions.go",
		addJavQualityReviewDecisions,
		irreversibleMigration,
	)
}

// addJavQualityReviewDecisions stores the human verdict before the physical
// move/delete is executed. The download attempt remains awaiting_quality
// until the batch operation confirms the CloudDrive2 action.
func addJavQualityReviewDecisions(ctx context.Context, tx *sql.Tx) error {
	return execStatements(ctx, tx,
		`ALTER TABLE jav_download_attempt ADD COLUMN review_decision text NOT NULL DEFAULT ""`,
		`ALTER TABLE jav_download_attempt ADD COLUMN review_quality_clear numeric`,
		`ALTER TABLE jav_download_attempt ADD COLUMN review_confirmed_1080p numeric`,
		`ALTER TABLE jav_download_attempt ADD COLUMN review_has_intro_ad numeric`,
		`ALTER TABLE jav_download_attempt ADD COLUMN review_has_watermark numeric`,
		`ALTER TABLE jav_download_attempt ADD COLUMN review_has_marquee numeric`,
		`ALTER TABLE jav_download_attempt ADD COLUMN review_is_uncensored numeric`,
		`ALTER TABLE jav_download_attempt ADD COLUMN review_reasons text NOT NULL DEFAULT ""`,
		`ALTER TABLE jav_download_attempt ADD COLUMN review_notes text NOT NULL DEFAULT ""`,
		`ALTER TABLE jav_download_attempt ADD COLUMN reviewed_at datetime`,
		`CREATE INDEX IF NOT EXISTS idx_jav_download_attempt_review_decision ON jav_download_attempt(review_decision)`,
	)
}

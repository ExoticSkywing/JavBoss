package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202608270002_add_jav_magnet_workflow.go",
		addJavMagnetWorkflow,
		irreversibleMigration,
	)
}

func addJavMagnetWorkflow(ctx context.Context, tx *sql.Tx) error {
	return execStatements(ctx, tx,
		`CREATE TABLE IF NOT EXISTS "jav_magnet_candidate" (
			id integer PRIMARY KEY AUTOINCREMENT,
			jav_id integer NOT NULL,
			info_hash text NOT NULL,
			uri text NOT NULL,
			name text NOT NULL DEFAULT "",
			size_mi_b integer NOT NULL DEFAULT 0,
			hd numeric NOT NULL DEFAULT 0,
			cn_sub numeric NOT NULL DEFAULT 0,
			files integer NOT NULL DEFAULT 0,
			source_created_at text NOT NULL DEFAULT "",
			first_seen_at datetime NOT NULL,
			last_seen_at datetime NOT NULL,
			review_status text NOT NULL DEFAULT "pending",
			quality_clear numeric,
			confirmed1080_p numeric,
			has_intro_ad numeric,
			has_watermark numeric,
			has_marquee numeric,
			is_uncensored numeric,
			review_reasons text NOT NULL DEFAULT "",
			review_notes text NOT NULL DEFAULT "",
			reviewed_at datetime,
			created_at datetime,
			updated_at datetime
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_magnet_candidate_jav_id ON jav_magnet_candidate(jav_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_magnet_candidate_review_status ON jav_magnet_candidate(review_status)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_jav_magnet_candidate_jav_hash ON jav_magnet_candidate(jav_id, info_hash)`,
		`CREATE TABLE IF NOT EXISTS "jav_magnet_selection" (
			jav_id integer PRIMARY KEY,
			candidate_id integer NOT NULL,
			selected_at datetime NOT NULL,
			updated_at datetime NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_magnet_selection_candidate_id ON jav_magnet_selection(candidate_id)`,
		`CREATE TABLE IF NOT EXISTS "jav_download_batch" (
			id integer PRIMARY KEY AUTOINCREMENT,
			status text NOT NULL DEFAULT "pending",
			external_batch_id text NOT NULL DEFAULT "",
			error text NOT NULL DEFAULT "",
			created_at datetime,
			submitted_at datetime,
			updated_at datetime
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_download_batch_status ON jav_download_batch(status)`,
		`CREATE TABLE IF NOT EXISTS "jav_download_attempt" (
			id integer PRIMARY KEY AUTOINCREMENT,
			batch_id integer NOT NULL,
			jav_id integer NOT NULL,
			candidate_id integer NOT NULL,
			idempotency_key text NOT NULL,
			external_task_id text NOT NULL DEFAULT "",
			status text NOT NULL DEFAULT "pending",
			error text NOT NULL DEFAULT "",
			created_at datetime,
			submitted_at datetime,
			completed_at datetime,
			CONSTRAINT fk_jav_download_batch_attempts FOREIGN KEY (batch_id) REFERENCES jav_download_batch(id) ON UPDATE CASCADE ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_download_attempt_batch_id ON jav_download_attempt(batch_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_download_attempt_jav_id ON jav_download_attempt(jav_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_download_attempt_candidate_id ON jav_download_attempt(candidate_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_jav_download_attempt_idempotency_key ON jav_download_attempt(idempotency_key)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_download_attempt_status ON jav_download_attempt(status)`,
		`CREATE TABLE IF NOT EXISTS "jav_quality_acceptance" (
			id integer PRIMARY KEY AUTOINCREMENT,
			jav_id integer NOT NULL,
			candidate_id integer NOT NULL,
			attempt_id integer,
			video_id integer,
			location_id integer,
			accepted_at datetime NOT NULL,
			notes text NOT NULL DEFAULT "",
			created_at datetime,
			updated_at datetime
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_jav_quality_acceptance_jav_id ON jav_quality_acceptance(jav_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_quality_acceptance_candidate_id ON jav_quality_acceptance(candidate_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_quality_acceptance_attempt_id ON jav_quality_acceptance(attempt_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_quality_acceptance_video_id ON jav_quality_acceptance(video_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_quality_acceptance_location_id ON jav_quality_acceptance(location_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_quality_acceptance_accepted_at ON jav_quality_acceptance(accepted_at)`,
	)
}

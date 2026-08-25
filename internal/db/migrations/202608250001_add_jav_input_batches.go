package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202608250001_add_jav_input_batches.go",
		addJavInputBatches,
		irreversibleMigration,
	)
}

func addJavInputBatches(ctx context.Context, tx *sql.Tx) error {
	return execStatements(ctx, tx,
		`CREATE TABLE IF NOT EXISTS "jav_input_batch" (
			id integer PRIMARY KEY AUTOINCREMENT,
			raw_input text NOT NULL,
			input_count integer NOT NULL DEFAULT 0,
			parsed_count integer NOT NULL DEFAULT 0,
			batch_unique_count integer NOT NULL DEFAULT 0,
			batch_duplicate_count integer NOT NULL DEFAULT 0,
			library_duplicate_count integer NOT NULL DEFAULT 0,
			history_duplicate_count integer NOT NULL DEFAULT 0,
			accepted_count integer NOT NULL DEFAULT 0,
			invalid_count integer NOT NULL DEFAULT 0,
			created_at datetime NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS "jav_input_item" (
			id integer PRIMARY KEY AUTOINCREMENT,
			jav_input_batch_id integer NOT NULL,
			line_number integer NOT NULL,
			raw_line text NOT NULL,
			code text NOT NULL DEFAULT "",
			normalized_code text NOT NULL DEFAULT "",
			status text NOT NULL,
			duplicate_of_line integer NOT NULL DEFAULT 0,
			existing_batch_id integer,
			existing_jav_id integer,
			created_at datetime NOT NULL,
			CONSTRAINT fk_jav_input_batch_items FOREIGN KEY (jav_input_batch_id) REFERENCES jav_input_batch(id) ON UPDATE CASCADE ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_input_item_batch_id ON jav_input_item(jav_input_batch_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_input_item_normalized_code ON jav_input_item(normalized_code)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_jav_input_item_accepted_code ON jav_input_item(normalized_code) WHERE status = 'accepted'`,
		`CREATE INDEX IF NOT EXISTS idx_jav_input_item_status ON jav_input_item(status)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_input_item_existing_batch_id ON jav_input_item(existing_batch_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_input_item_existing_jav_id ON jav_input_item(existing_jav_id)`,
	)
}

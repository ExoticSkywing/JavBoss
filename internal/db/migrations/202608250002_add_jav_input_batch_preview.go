package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202608250002_add_jav_input_batch_preview.go",
		addJavInputBatchPreview,
		irreversibleMigration,
	)
}

func addJavInputBatchPreview(ctx context.Context, tx *sql.Tx) error {
	if err := addColumnIfMissing(ctx, tx, "jav_input_batch", "preview", `text NOT NULL DEFAULT ""`); err != nil {
		return err
	}

	// Strip leading blank lines, select the first non-empty line, and retain a
	// compact 80-character identifier for history cards created before this
	// column existed.
	return execDB(ctx, tx, `
		WITH source AS (
			SELECT
				id,
				ltrim(replace(raw_input, char(13), ''), char(9) || char(10) || ' ') AS content
			FROM jav_input_batch
		)
		UPDATE jav_input_batch
		SET preview = substr(trim(CASE
			WHEN instr((SELECT content FROM source WHERE source.id = jav_input_batch.id), char(10)) > 0
			THEN substr(
				(SELECT content FROM source WHERE source.id = jav_input_batch.id),
				1,
				instr((SELECT content FROM source WHERE source.id = jav_input_batch.id), char(10)) - 1
			)
			ELSE (SELECT content FROM source WHERE source.id = jav_input_batch.id)
		END), 1, 80)
		WHERE preview = ''
	`)
}

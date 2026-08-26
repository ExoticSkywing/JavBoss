package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"

	"github.com/pressly/goose/v3"
)

const javAcquisitionStageMetadataPending = "metadata_pending"

func init() {
	goose.AddNamedMigrationContext(
		"202608250003_unify_jav_work_lifecycle.go",
		unifyJavWorkLifecycle,
		irreversibleMigration,
	)
}

func unifyJavWorkLifecycle(ctx context.Context, tx *sql.Tx) error {
	// rebuildJavWithCanonicalCode replaces the parent table.  SQLite executes
	// ON DELETE actions when a parent is dropped while foreign keys are enabled,
	// which would cascade-delete existing tag/idol/acquisition mappings.  The
	// production migration runner deliberately disables foreign keys around the
	// whole migration; fail closed if this function is ever invoked through a
	// different runner instead of risking silent data loss.
	var foreignKeysEnabled int
	if err := tx.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeysEnabled); err != nil {
		return fmt.Errorf("inspect SQLite foreign-key mode: %w", err)
	}
	if foreignKeysEnabled != 0 {
		return fmt.Errorf("unify JAV lifecycle requires PRAGMA foreign_keys=OFF while rebuilding jav")
	}
	if err := addColumnIfMissing(ctx, tx, "jav", "normalized_code", `text NOT NULL DEFAULT ""`); err != nil {
		return err
	}

	if err := backfillCanonicalJavCodes(ctx, tx); err != nil {
		return err
	}
	if err := rejectCanonicalJavCodeCollisions(ctx, tx); err != nil {
		return err
	}
	if err := rebuildJavWithCanonicalCode(ctx, tx); err != nil {
		return err
	}
	return addJavAcquisitionLifecycle(ctx, tx)
}

func backfillCanonicalJavCodes(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, COALESCE(code, '') FROM jav ORDER BY id`)
	if err != nil {
		return fmt.Errorf("list JAV codes for canonical backfill: %w", err)
	}
	type javCodeRow struct {
		id         int64
		normalized string
	}
	var updates []javCodeRow
	for rows.Next() {
		var id int64
		var code string
		if err := rows.Scan(&id, &code); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan JAV code for canonical backfill: %w", err)
		}
		normalized := normalizeJavCodeForMigration(code)
		if normalized == "" {
			_ = rows.Close()
			return fmt.Errorf("cannot migrate JAV id=%d code=%q: canonical code is empty", id, code)
		}
		updates = append(updates, javCodeRow{id: id, normalized: normalized})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate JAV codes for canonical backfill: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close JAV code backfill rows: %w", err)
	}
	for _, update := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE jav SET normalized_code = ? WHERE id = ?`, update.normalized, update.id); err != nil {
			return fmt.Errorf("backfill canonical JAV code id=%d: %w", update.id, err)
		}
	}
	return nil
}

func rejectCanonicalJavCodeCollisions(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT normalized_code, GROUP_CONCAT(id || ':' || code, ', ')
		FROM jav
		GROUP BY normalized_code
		HAVING COUNT(*) > 1
		ORDER BY normalized_code
	`)
	if err != nil {
		return fmt.Errorf("check canonical JAV code collisions: %w", err)
	}
	defer rows.Close()
	var collisions []string
	for rows.Next() {
		var normalized, members string
		if err := rows.Scan(&normalized, &members); err != nil {
			return fmt.Errorf("scan canonical JAV code collision: %w", err)
		}
		collisions = append(collisions, fmt.Sprintf("%s => [%s]", normalized, members))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate canonical JAV code collisions: %w", err)
	}
	if len(collisions) > 0 {
		return fmt.Errorf("canonical JAV code collisions must be resolved before migration: %s", strings.Join(collisions, "; "))
	}
	return nil
}

func rebuildJavWithCanonicalCode(ctx context.Context, tx *sql.Tx) error {
	const columns = `"id", "code", "normalized_code", "title", "studio_id", "series_id", "series_en_id", "release_unix", "duration_min", "fetched_at", "created_at", "updated_at", "is_uncensored", "sample_images", "favorite_rating"`
	return execStatements(ctx, tx,
		`DROP TABLE IF EXISTS "__new_jav"`,
		`CREATE TABLE "__new_jav" (
			id integer PRIMARY KEY AUTOINCREMENT,
			code text,
			normalized_code text NOT NULL,
			title text,
			studio_id integer,
			series_id integer,
			series_en_id integer,
			release_unix integer,
			duration_min integer,
			fetched_at datetime,
			created_at datetime,
			updated_at datetime,
			is_uncensored numeric,
			sample_images text NOT NULL DEFAULT "[]",
			favorite_rating real NOT NULL DEFAULT 0,
			CONSTRAINT fk_jav_studio FOREIGN KEY (studio_id) REFERENCES jav_studio(id) ON UPDATE CASCADE ON DELETE SET NULL,
			CONSTRAINT fk_jav_series FOREIGN KEY (series_id) REFERENCES jav_series(id) ON UPDATE CASCADE ON DELETE SET NULL,
			CONSTRAINT fk_jav_series_en FOREIGN KEY (series_en_id) REFERENCES jav_series(id) ON UPDATE CASCADE ON DELETE SET NULL
		)`,
		`INSERT INTO "__new_jav" (`+columns+`) SELECT `+columns+` FROM "jav"`,
		`DROP TABLE "jav"`,
		`ALTER TABLE "__new_jav" RENAME TO "jav"`,
		`CREATE UNIQUE INDEX idx_jav_code ON jav(code)`,
		`CREATE UNIQUE INDEX idx_jav_normalized_code ON jav(normalized_code)`,
		`CREATE INDEX idx_jav_studio_id ON jav(studio_id)`,
		`CREATE INDEX idx_jav_series_id ON jav(series_id)`,
		`CREATE INDEX idx_jav_series_en_id ON jav(series_en_id)`,
	)
}

func addJavAcquisitionLifecycle(ctx context.Context, tx *sql.Tx) error {
	// The first input migration used a partial unique index on the old display
	// normalization.  It must be removed before canonicalizing historical rows:
	// aliases such as FC2-PPV-123 and FC2-123 (or separator/case variants) are
	// intentionally allowed to collapse onto one canonical Jav during this
	// migration.  The replacement table below installs only the non-unique
	// lookup index, while jav.normalized_code remains the durable unique key.
	if err := execStatements(ctx, tx,
		`DROP INDEX IF EXISTS idx_jav_input_item_accepted_code`,
	); err != nil {
		return err
	}
	if err := execStatements(ctx, tx,
		`CREATE TABLE IF NOT EXISTS "jav_acquisition" (
			jav_id integer PRIMARY KEY,
			stage text NOT NULL DEFAULT "metadata_pending",
			created_at datetime NOT NULL,
			updated_at datetime NOT NULL,
			CONSTRAINT fk_jav_acquisition_jav FOREIGN KEY (jav_id) REFERENCES jav(id) ON UPDATE CASCADE ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_acquisition_stage ON jav_acquisition(stage)`,
	); err != nil {
		return err
	}
	if err := addColumnIfMissing(ctx, tx, "jav_input_item", "jav_id", "integer"); err != nil {
		return err
	}

	if err := materializeAcceptedJavInputs(ctx, tx); err != nil {
		return err
	}
	return rebuildJavInputItemWithJavReference(ctx, tx)
}

func rebuildJavInputItemWithJavReference(ctx context.Context, tx *sql.Tx) error {
	const columns = `"id", "jav_input_batch_id", "line_number", "raw_line", "code", "normalized_code", "jav_id", "status", "duplicate_of_line", "existing_batch_id", "existing_jav_id", "created_at"`
	return execStatements(ctx, tx,
		`DROP TABLE IF EXISTS "__new_jav_input_item"`,
		`CREATE TABLE "__new_jav_input_item" (
			id integer PRIMARY KEY AUTOINCREMENT,
			jav_input_batch_id integer NOT NULL,
			line_number integer NOT NULL,
			raw_line text NOT NULL,
			code text NOT NULL DEFAULT "",
			normalized_code text NOT NULL DEFAULT "",
			jav_id integer,
			status text NOT NULL,
			duplicate_of_line integer NOT NULL DEFAULT 0,
			existing_batch_id integer,
			existing_jav_id integer,
			created_at datetime NOT NULL,
			CONSTRAINT fk_jav_input_batch_items FOREIGN KEY (jav_input_batch_id) REFERENCES jav_input_batch(id) ON UPDATE CASCADE ON DELETE CASCADE,
			CONSTRAINT fk_jav_input_item_jav FOREIGN KEY (jav_id) REFERENCES jav(id) ON UPDATE CASCADE ON DELETE SET NULL
		)`,
		`INSERT INTO "__new_jav_input_item" (`+columns+`) SELECT `+columns+` FROM "jav_input_item"`,
		`DROP TABLE "jav_input_item"`,
		`ALTER TABLE "__new_jav_input_item" RENAME TO "jav_input_item"`,
		`CREATE INDEX idx_jav_input_item_batch_id ON jav_input_item(jav_input_batch_id)`,
		`CREATE INDEX idx_jav_input_item_normalized_code ON jav_input_item(normalized_code)`,
		`CREATE INDEX idx_jav_input_item_jav_id ON jav_input_item(jav_id)`,
		`CREATE INDEX idx_jav_input_item_status ON jav_input_item(status)`,
		`CREATE INDEX idx_jav_input_item_existing_batch_id ON jav_input_item(existing_batch_id)`,
		`CREATE INDEX idx_jav_input_item_existing_jav_id ON jav_input_item(existing_jav_id)`,
	)
}

func materializeAcceptedJavInputs(ctx context.Context, tx *sql.Tx) error {
	if err := canonicalizeHistoricalJavInputItems(ctx, tx); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, COALESCE(code, ''), COALESCE(normalized_code, ''), created_at
		FROM jav_input_item
		WHERE status = 'accepted'
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("list accepted JAV inputs for materialization: %w", err)
	}
	type acceptedInput struct {
		id, javID           int64
		code, oldNormalized string
		normalized          string
		createdAt           any
	}
	var inputs []acceptedInput
	for rows.Next() {
		var input acceptedInput
		if err := rows.Scan(&input.id, &input.code, &input.oldNormalized, &input.createdAt); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan accepted JAV input for materialization: %w", err)
		}
		input.normalized = normalizeJavCodeForMigration(input.code)
		if input.normalized == "" {
			input.normalized = normalizeJavCodeForMigration(input.oldNormalized)
		}
		if input.normalized == "" {
			_ = rows.Close()
			return fmt.Errorf("cannot materialize accepted JAV input id=%d code=%q: canonical code is empty", input.id, input.code)
		}
		inputs = append(inputs, input)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate accepted JAV inputs for materialization: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close accepted JAV input rows: %w", err)
	}

	for index := range inputs {
		input := &inputs[index]
		if err := tx.QueryRowContext(ctx, `SELECT id FROM jav WHERE normalized_code = ?`, input.normalized).Scan(&input.javID); err != nil {
			if err != sql.ErrNoRows {
				return fmt.Errorf("find canonical JAV for accepted input id=%d: %w", input.id, err)
			}
			result, insertErr := tx.ExecContext(ctx, `
				INSERT INTO jav (code, normalized_code, sample_images, favorite_rating, created_at, updated_at)
				VALUES (?, ?, '[]', 0, ?, ?)
			`, input.code, input.normalized, input.createdAt, input.createdAt)
			if insertErr != nil {
				return fmt.Errorf("materialize canonical JAV for accepted input id=%d: %w", input.id, insertErr)
			}
			input.javID, err = result.LastInsertId()
			if err != nil {
				return fmt.Errorf("read materialized JAV id for accepted input id=%d: %w", input.id, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO jav_acquisition (jav_id, stage, created_at, updated_at)
			VALUES (?, ?, ?, ?)
		`, input.javID, javAcquisitionStageMetadataPending, input.createdAt, input.createdAt); err != nil {
			return fmt.Errorf("create acquisition for accepted input id=%d: %w", input.id, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE jav_input_item SET jav_id = ?, normalized_code = ? WHERE id = ?`, input.javID, input.normalized, input.id); err != nil {
			return fmt.Errorf("link accepted input id=%d to canonical JAV: %w", input.id, err)
		}
	}

	// Link every historical occurrence to the same canonical work when possible.
	if _, err := tx.ExecContext(ctx, `
		UPDATE jav_input_item
		SET jav_id = (
			SELECT j.id FROM jav j
			WHERE j.normalized_code = jav_input_item.normalized_code
		)
		WHERE jav_id IS NULL AND COALESCE(normalized_code, '') <> ''
	`); err != nil {
		return fmt.Errorf("link historical JAV inputs to canonical works: %w", err)
	}
	return nil
}

func canonicalizeHistoricalJavInputItems(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, COALESCE(code, ''), COALESCE(normalized_code, '')
		FROM jav_input_item
		WHERE COALESCE(code, '') <> '' OR COALESCE(normalized_code, '') <> ''
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("list historical JAV input codes for canonicalization: %w", err)
	}
	type inputCode struct {
		id         int64
		code       string
		normalized string
	}
	var items []inputCode
	for rows.Next() {
		var item inputCode
		if err := rows.Scan(&item.id, &item.code, &item.normalized); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan historical JAV input code for canonicalization: %w", err)
		}
		canonical := normalizeJavCodeForMigration(item.code)
		if canonical == "" {
			canonical = normalizeJavCodeForMigration(item.normalized)
		}
		if canonical == "" {
			_ = rows.Close()
			return fmt.Errorf("cannot canonicalize JAV input item id=%d code=%q normalized_code=%q", item.id, item.code, item.normalized)
		}
		item.normalized = canonical
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate historical JAV input codes for canonicalization: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close historical JAV input code rows: %w", err)
	}
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `UPDATE jav_input_item SET normalized_code = ? WHERE id = ?`, item.normalized, item.id); err != nil {
			return fmt.Errorf("canonicalize historical JAV input item id=%d: %w", item.id, err)
		}
	}
	return nil
}

func normalizeJavCodeForMigration(value string) string {
	var result strings.Builder
	for _, char := range strings.ToUpper(strings.TrimSpace(value)) {
		if char <= unicode.MaxASCII && ((char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			result.WriteRune(char)
		}
	}
	normalized := result.String()
	if strings.HasPrefix(normalized, "FC2PPV") {
		normalized = "FC2" + strings.TrimPrefix(normalized, "FC2PPV")
	}
	return normalized
}

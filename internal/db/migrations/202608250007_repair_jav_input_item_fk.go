package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/pressly/goose/v3"
)

// The first version of the lifecycle migration added jav_input_item.jav_id
// with ALTER TABLE.  That left older databases without the foreign-key action
// that the canonical model relies on.  This migration repairs those databases
// without changing the canonical Jav rows or the input receipt data.
func init() {
	goose.AddNamedMigrationContext(
		"202608250007_repair_jav_input_item_fk.go",
		repairJavInputItemForeignKey,
		irreversibleMigration,
	)
}

const javInputItemTable = "jav_input_item"

var javInputItemColumns = []string{
	"id",
	"jav_input_batch_id",
	"line_number",
	"raw_line",
	"code",
	"normalized_code",
	"jav_id",
	"status",
	"duplicate_of_line",
	"existing_batch_id",
	"existing_jav_id",
	"created_at",
}

var javInputItemIndexes = []string{
	"idx_jav_input_item_batch_id",
	"idx_jav_input_item_normalized_code",
	"idx_jav_input_item_jav_id",
	"idx_jav_input_item_status",
	"idx_jav_input_item_existing_batch_id",
	"idx_jav_input_item_existing_jav_id",
}

func repairJavInputItemForeignKey(ctx context.Context, tx *sql.Tx) error {
	exists, err := migrationTableExists(ctx, tx, javInputItemTable)
	if err != nil {
		return fmt.Errorf("check %s table: %w", javInputItemTable, err)
	}
	if !exists {
		return fmt.Errorf("cannot repair %s: table does not exist", javInputItemTable)
	}

	columns, err := migrationTableColumns(ctx, tx, javInputItemTable)
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", javInputItemTable, err)
	}
	if err := validateJavInputItemColumns(columns); err != nil {
		return err
	}
	if err := validateJavInputItemReferences(ctx, tx, columns); err != nil {
		return err
	}

	hasJavForeignKey, err := javInputItemHasJavForeignKey(ctx, tx)
	if err != nil {
		return fmt.Errorf("inspect %s foreign keys: %w", javInputItemTable, err)
	}
	if hasJavForeignKey && sameColumnOrder(columns, javInputItemColumns) {
		// The migration is intentionally idempotent.  A database that already
		// has the repaired shape only needs missing lookup indexes restored.
		return ensureJavInputItemIndexes(ctx, tx)
	}

	indexSQL, err := captureJavInputItemIndexes(ctx, tx)
	if err != nil {
		return err
	}
	if err := rebuildJavInputItem(ctx, tx, columns); err != nil {
		return err
	}
	if err := ensureJavInputItemIndexes(ctx, tx); err != nil {
		return err
	}
	for _, statement := range indexSQL {
		if err := execDB(ctx, tx, statement); err != nil {
			return fmt.Errorf("restore JAV input item index: %w", err)
		}
	}
	return nil
}

func migrationTableExists(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`,
		table,
	).Scan(&exists)
	return exists != 0, err
}

func migrationTableColumns(ctx context.Context, tx *sql.Tx, table string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+quoteIdentifier(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultV   any
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultV, &primaryKey); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

func validateJavInputItemColumns(columns []string) error {
	wanted := make(map[string]struct{}, len(javInputItemColumns))
	for _, column := range javInputItemColumns {
		wanted[column] = struct{}{}
	}
	for _, column := range columns {
		if _, ok := wanted[strings.ToLower(column)]; !ok {
			return fmt.Errorf("cannot repair %s: unexpected column %q would be discarded", javInputItemTable, column)
		}
	}
	for _, column := range javInputItemColumns {
		found := false
		for _, actual := range columns {
			if strings.EqualFold(actual, column) {
				found = true
				break
			}
		}
		if !found && column != "jav_id" {
			return fmt.Errorf("cannot repair %s: required column %q is missing", javInputItemTable, column)
		}
	}
	return nil
}

func sameColumnOrder(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range want {
		if !strings.EqualFold(got[index], want[index]) {
			return false
		}
	}
	return true
}

func validateJavInputItemReferences(ctx context.Context, tx *sql.Tx, columns []string) error {
	checks := []struct {
		column string
		table  string
	}{
		{column: "jav_input_batch_id", table: "jav_input_batch"},
	}
	if containsColumn(columns, "jav_id") {
		checks = append(checks, struct {
			column string
			table  string
		}{column: "jav_id", table: "jav"})
	}
	for _, check := range checks {
		query := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM %s item
			LEFT JOIN %s parent ON parent.id = item.%s
			WHERE item.%s IS NOT NULL AND parent.id IS NULL
		`, quoteIdentifier(javInputItemTable), quoteIdentifier(check.table), quoteIdentifier(check.column), quoteIdentifier(check.column))
		var orphanCount int64
		if err := tx.QueryRowContext(ctx, query).Scan(&orphanCount); err != nil {
			return fmt.Errorf("check orphan %s references: %w", check.column, err)
		}
		if orphanCount > 0 {
			return fmt.Errorf("cannot repair %s: %d %s reference(s) point to missing %s rows", javInputItemTable, orphanCount, check.column, check.table)
		}
	}
	return nil
}

func containsColumn(columns []string, wanted string) bool {
	for _, column := range columns {
		if strings.EqualFold(column, wanted) {
			return true
		}
	}
	return false
}

func javInputItemHasJavForeignKey(ctx context.Context, tx *sql.Tx) (bool, error) {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_list("+quoteIdentifier(javInputItemTable)+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, sequence                                   int
			tableName, from, to, onUpdate, onDelete, match sql.NullString
		)
		if err := rows.Scan(&id, &sequence, &tableName, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return false, err
		}
		if strings.EqualFold(tableName.String, "jav") &&
			strings.EqualFold(from.String, "jav_id") &&
			strings.EqualFold(to.String, "id") &&
			strings.EqualFold(onDelete.String, "SET NULL") {
			return true, nil
		}
	}
	return false, rows.Err()
}

func captureJavInputItemIndexes(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT name, sql
		FROM sqlite_master
		WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL
		ORDER BY name
	`, javInputItemTable)
	if err != nil {
		return nil, fmt.Errorf("capture %s indexes: %w", javInputItemTable, err)
	}
	defer rows.Close()

	known := make(map[string]struct{}, len(javInputItemIndexes))
	for _, name := range javInputItemIndexes {
		known[name] = struct{}{}
	}
	var statements []string
	for rows.Next() {
		var name, statement string
		if err := rows.Scan(&name, &statement); err != nil {
			return nil, fmt.Errorf("scan %s index: %w", javInputItemTable, err)
		}
		// This index enforced the pre-canonical "accepted" identity and must
		// not be restored: aliases are now unified by jav.normalized_code.
		if strings.EqualFold(name, "idx_jav_input_item_accepted_code") {
			continue
		}
		if _, isKnown := known[name]; isKnown {
			continue
		}
		statements = append(statements, statement)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s indexes: %w", javInputItemTable, err)
	}
	return statements, nil
}

func rebuildJavInputItem(ctx context.Context, tx *sql.Tx, columns []string) error {
	columnSet := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		columnSet[strings.ToLower(column)] = struct{}{}
	}
	selectColumns := make([]string, 0, len(javInputItemColumns))
	for _, column := range javInputItemColumns {
		if _, ok := columnSet[strings.ToLower(column)]; ok {
			selectColumns = append(selectColumns, quoteIdentifier(column))
		} else {
			selectColumns = append(selectColumns, "NULL")
		}
	}
	const createTable = `CREATE TABLE "__new_jav_input_item" (
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
		)`
	if err := execStatements(ctx, tx,
		`DROP TABLE IF EXISTS "__new_jav_input_item"`,
		createTable,
		`INSERT INTO "__new_jav_input_item" ("id", "jav_input_batch_id", "line_number", "raw_line", "code", "normalized_code", "jav_id", "status", "duplicate_of_line", "existing_batch_id", "existing_jav_id", "created_at") SELECT `+strings.Join(selectColumns, ", ")+` FROM "jav_input_item"`,
		`DROP TABLE "jav_input_item"`,
		`ALTER TABLE "__new_jav_input_item" RENAME TO "jav_input_item"`,
	); err != nil {
		return fmt.Errorf("rebuild %s with canonical foreign key: %w", javInputItemTable, err)
	}
	return nil
}

func ensureJavInputItemIndexes(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_jav_input_item_batch_id ON jav_input_item(jav_input_batch_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_input_item_normalized_code ON jav_input_item(normalized_code)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_input_item_jav_id ON jav_input_item(jav_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_input_item_status ON jav_input_item(status)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_input_item_existing_batch_id ON jav_input_item(existing_batch_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_input_item_existing_jav_id ON jav_input_item(existing_jav_id)`,
		`DROP INDEX IF EXISTS idx_jav_input_item_accepted_code`,
	}
	if err := execStatements(ctx, tx, statements...); err != nil {
		return fmt.Errorf("ensure %s indexes: %w", javInputItemTable, err)
	}
	return nil
}

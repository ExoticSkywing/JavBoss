package migrations

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestRepairJavInputItemForeignKeyRebuildsLegacyTableAndIsIdempotent(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`
		PRAGMA foreign_keys = ON;
		CREATE TABLE jav (id INTEGER PRIMARY KEY AUTOINCREMENT, code TEXT NOT NULL);
		CREATE TABLE jav_input_batch (id INTEGER PRIMARY KEY AUTOINCREMENT, raw_input TEXT NOT NULL);
		CREATE TABLE jav_input_item (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			jav_input_batch_id INTEGER NOT NULL,
			line_number INTEGER NOT NULL,
			raw_line TEXT NOT NULL,
			code TEXT NOT NULL DEFAULT '',
			normalized_code TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			duplicate_of_line INTEGER NOT NULL DEFAULT 0,
			existing_batch_id INTEGER,
			existing_jav_id INTEGER,
			created_at DATETIME NOT NULL,
			jav_id INTEGER,
			CONSTRAINT fk_jav_input_batch_items FOREIGN KEY (jav_input_batch_id) REFERENCES jav_input_batch(id) ON UPDATE CASCADE ON DELETE CASCADE
		);
		CREATE INDEX idx_jav_input_item_batch_id ON jav_input_item(jav_input_batch_id);
		CREATE INDEX idx_jav_input_item_normalized_code ON jav_input_item(normalized_code);
		CREATE UNIQUE INDEX idx_jav_input_item_accepted_code ON jav_input_item(normalized_code) WHERE status = 'accepted';
		CREATE INDEX idx_jav_input_item_status ON jav_input_item(status);
		CREATE INDEX idx_jav_input_item_existing_batch_id ON jav_input_item(existing_batch_id);
		CREATE INDEX idx_jav_input_item_existing_jav_id ON jav_input_item(existing_jav_id);
		CREATE INDEX idx_jav_input_item_custom ON jav_input_item(line_number);
		INSERT INTO jav (id, code) VALUES (7, 'AAA-001'), (8, 'BBB-002');
		INSERT INTO jav_input_batch (id, raw_input) VALUES (3, 'AAA-001');
		INSERT INTO jav_input_item (id, jav_input_batch_id, line_number, raw_line, code, normalized_code, status, created_at, jav_id)
		VALUES (11, 3, 1, 'AAA-001', 'AAA-001', 'AAA001', 'accepted', '2026-08-25 12:00:00', 7),
		       (12, 3, 2, 'BBB-002', 'BBB-002', 'BBB002', 'duplicate_history', '2026-08-25 12:00:01', 8)
	`); err != nil {
		t.Fatalf("seed legacy JAV input schema: %v", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		tx, err := database.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatalf("begin repair attempt %d: %v", attempt+1, err)
		}
		if err := repairJavInputItemForeignKey(context.Background(), tx); err != nil {
			_ = tx.Rollback()
			t.Fatalf("repair attempt %d: %v", attempt+1, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit repair attempt %d: %v", attempt+1, err)
		}
	}

	inspectionTx := mustBeginTx(t, database)
	columns, err := migrationTableColumns(context.Background(), inspectionTx, javInputItemTable)
	_ = inspectionTx.Rollback()
	if err != nil {
		t.Fatalf("inspect repaired columns: %v", err)
	}
	if !reflect.DeepEqual(columns, javInputItemColumns) {
		t.Fatalf("repaired columns = %#v, want %#v", columns, javInputItemColumns)
	}

	var linkedCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM jav_input_item WHERE jav_id IS NOT NULL`).Scan(&linkedCount); err != nil {
		t.Fatalf("count linked input items: %v", err)
	}
	if linkedCount != 2 {
		t.Fatalf("linked input item count = %d, want 2", linkedCount)
	}
	var customIndexCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_index_list('jav_input_item') WHERE name = 'idx_jav_input_item_custom'`).Scan(&customIndexCount); err != nil {
		t.Fatalf("inspect custom index: %v", err)
	}
	if customIndexCount != 1 {
		t.Fatalf("custom index count = %d, want 1", customIndexCount)
	}
	var obsoleteIndexCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_index_list('jav_input_item') WHERE name = 'idx_jav_input_item_accepted_code'`).Scan(&obsoleteIndexCount); err != nil {
		t.Fatalf("inspect obsolete index: %v", err)
	}
	if obsoleteIndexCount != 0 {
		t.Fatalf("obsolete accepted index still exists")
	}

	var foreignKeyCount int
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM pragma_foreign_key_list('jav_input_item')
		WHERE "table" = 'jav' AND "from" = 'jav_id' AND "to" = 'id' AND upper(on_delete) = 'SET NULL'
	`).Scan(&foreignKeyCount); err != nil {
		t.Fatalf("inspect repaired JAV foreign key: %v", err)
	}
	if foreignKeyCount != 1 {
		t.Fatalf("repaired JAV foreign key count = %d, want 1", foreignKeyCount)
	}

	if _, err := database.Exec(`DELETE FROM jav WHERE id = 7`); err != nil {
		t.Fatalf("delete referenced JAV: %v", err)
	}
	var cleared sql.NullInt64
	if err := database.QueryRow(`SELECT jav_id FROM jav_input_item WHERE id = 11`).Scan(&cleared); err != nil {
		t.Fatalf("load cleared input reference: %v", err)
	}
	if cleared.Valid {
		t.Fatalf("deleted JAV reference = %d, want NULL", cleared.Int64)
	}

	var nextID int64
	if _, err := database.Exec(`INSERT INTO jav_input_item (jav_input_batch_id, line_number, raw_line, status, created_at) VALUES (3, 3, 'CCC-003', 'accepted', '2026-08-25 12:00:02')`); err != nil {
		t.Fatalf("insert after repair: %v", err)
	}
	if err := database.QueryRow(`SELECT id FROM jav_input_item WHERE raw_line = 'CCC-003'`).Scan(&nextID); err != nil {
		t.Fatalf("load inserted input item: %v", err)
	}
	if nextID <= 12 {
		t.Fatalf("new input item id = %d, want greater than preserved max id 12", nextID)
	}
}

func TestRepairJavInputItemForeignKeyRejectsOrphanReferences(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`
		PRAGMA foreign_keys = OFF;
		CREATE TABLE jav (id INTEGER PRIMARY KEY);
		CREATE TABLE jav_input_batch (id INTEGER PRIMARY KEY);
		CREATE TABLE jav_input_item (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			jav_input_batch_id INTEGER NOT NULL,
			line_number INTEGER NOT NULL,
			raw_line TEXT NOT NULL,
			code TEXT NOT NULL DEFAULT '',
			normalized_code TEXT NOT NULL DEFAULT '',
			jav_id INTEGER,
			status TEXT NOT NULL,
			duplicate_of_line INTEGER NOT NULL DEFAULT 0,
			existing_batch_id INTEGER,
			existing_jav_id INTEGER,
			created_at DATETIME NOT NULL
		);
		INSERT INTO jav_input_batch (id) VALUES (1);
		INSERT INTO jav_input_item (jav_input_batch_id, line_number, raw_line, status, created_at, jav_id)
		VALUES (1, 1, 'ORPHAN-001', 'accepted', '2026-08-25 12:00:00', 999)
	`); err != nil {
		t.Fatalf("seed orphan input reference: %v", err)
	}
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin orphan repair: %v", err)
	}
	err = repairJavInputItemForeignKey(context.Background(), tx)
	_ = tx.Rollback()
	if err == nil || !strings.Contains(err.Error(), "point to missing jav rows") {
		t.Fatalf("orphan repair error = %v, want missing jav reference error", err)
	}
}

func mustBeginTx(t *testing.T, database *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin inspection transaction: %v", err)
	}
	return tx
}

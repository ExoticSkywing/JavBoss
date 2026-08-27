package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestRequeueJavDBIdolNames(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`
		CREATE TABLE jav (id integer PRIMARY KEY, jav_db_idols_reconciled_at datetime);
		INSERT INTO jav (id, jav_db_idols_reconciled_at) VALUES
			(1, '2026-08-26 12:00:00'),
			(2, NULL);
	`); err != nil {
		t.Fatalf("seed jav reconciliation state: %v", err)
	}

	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin migration: %v", err)
	}
	if err := requeueJavDBIdolNames(context.Background(), tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("requeue JavDB idol names: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit migration: %v", err)
	}

	var remaining int
	if err := database.QueryRow(`SELECT COUNT(*) FROM jav WHERE jav_db_idols_reconciled_at IS NOT NULL`).Scan(&remaining); err != nil {
		t.Fatalf("count reconciled JAV rows: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("reconciled row count = %d, want 0", remaining)
	}
}

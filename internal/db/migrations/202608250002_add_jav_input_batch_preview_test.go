package migrations

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestAddJavInputBatchPreviewBackfillsExistingHistory(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer database.Close()

	if _, err := database.Exec(`CREATE TABLE jav_input_batch (
		id integer PRIMARY KEY AUTOINCREMENT,
		raw_input text NOT NULL
	)`); err != nil {
		t.Fatalf("create old jav_input_batch: %v", err)
	}
	longLine := "LONG-001 " + strings.Repeat("中", 100)
	if _, err := database.Exec(
		`INSERT INTO jav_input_batch (raw_input) VALUES (?), (?)`,
		"  \r\n\n\tVRTM-073 美脚 1:27:09 黑丝美脚  \nCFNM-001",
		longLine,
	); err != nil {
		t.Fatalf("seed old input history: %v", err)
	}

	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin preview migration: %v", err)
	}
	if err := addJavInputBatchPreview(context.Background(), tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("add input preview: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit preview migration: %v", err)
	}

	var first, second string
	if err := database.QueryRow(`SELECT preview FROM jav_input_batch WHERE id = 1`).Scan(&first); err != nil {
		t.Fatalf("load first preview: %v", err)
	}
	if err := database.QueryRow(`SELECT preview FROM jav_input_batch WHERE id = 2`).Scan(&second); err != nil {
		t.Fatalf("load second preview: %v", err)
	}
	if first != "VRTM-073 美脚 1:27:09 黑丝美脚" {
		t.Fatalf("first preview = %q", first)
	}
	if len([]rune(second)) != 80 || !strings.HasPrefix(second, "LONG-001 ") {
		t.Fatalf("long preview was not truncated to 80 runes: %q", second)
	}
}

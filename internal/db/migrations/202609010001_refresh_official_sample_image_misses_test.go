package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestRefreshOfficialSampleImageMisses(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	if _, err := database.Exec(`
		CREATE TABLE jav (
			id integer PRIMARY KEY,
			sample_images text NOT NULL DEFAULT '[]',
			updated_at datetime
		);
		INSERT INTO jav (id, sample_images) VALUES
			(1, '[{"thumbnail_url":":not_found","detail_url":":not_found"}]'),
			(2, '[{"thumbnail_url":"thumb","detail_url":"detail"}]');
	`); err != nil {
		t.Fatalf("prepare database: %v", err)
	}

	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	if err := refreshOfficialSampleImageMisses(context.Background(), tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("refresh sample image misses: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}

	var missing string
	var existing string
	if err := database.QueryRow(`SELECT sample_images FROM jav WHERE id = 1`).Scan(&missing); err != nil {
		t.Fatalf("read refreshed row: %v", err)
	}
	if err := database.QueryRow(`SELECT sample_images FROM jav WHERE id = 2`).Scan(&existing); err != nil {
		t.Fatalf("read existing row: %v", err)
	}
	if missing != "[]" {
		t.Fatalf("refreshed sample images = %q, want []", missing)
	}
	if existing != `[{"thumbnail_url":"thumb","detail_url":"detail"}]` {
		t.Fatalf("existing sample images changed: %q", existing)
	}
}

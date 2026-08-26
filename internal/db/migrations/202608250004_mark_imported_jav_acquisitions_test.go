package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestMarkImportedJavAcquisitionsUsesActiveLocations(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`
		CREATE TABLE jav_acquisition (jav_id integer PRIMARY KEY, stage text NOT NULL, updated_at datetime NOT NULL);
		CREATE TABLE directory (id integer PRIMARY KEY, is_delete numeric NOT NULL DEFAULT 0);
		CREATE TABLE video_location (id integer PRIMARY KEY, directory_id integer NOT NULL, jav_id integer, is_delete numeric NOT NULL DEFAULT 0);
		INSERT INTO jav_acquisition (jav_id, stage, updated_at) VALUES
			(1, 'metadata_pending', '2026-08-25 01:00:00'),
			(2, 'magnet_collecting', '2026-08-25 01:00:00'),
			(3, 'metadata_pending', '2026-08-25 01:00:00');
		INSERT INTO directory (id, is_delete) VALUES (1, 0), (2, 1);
		INSERT INTO video_location (id, directory_id, jav_id, is_delete) VALUES
			(1, 1, 1, 0),
			(2, 2, 2, 0),
			(3, 1, 3, 1);
	`); err != nil {
		t.Fatalf("seed lifecycle schema: %v", err)
	}
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin migration: %v", err)
	}
	if err := markImportedJavAcquisitions(context.Background(), tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("mark imported acquisitions: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit migration: %v", err)
	}

	rows, err := database.Query(`SELECT jav_id, stage FROM jav_acquisition ORDER BY jav_id`)
	if err != nil {
		t.Fatalf("load stages: %v", err)
	}
	defer rows.Close()
	want := map[int64]string{1: "imported", 2: "magnet_collecting", 3: "metadata_pending"}
	for rows.Next() {
		var javID int64
		var stage string
		if err := rows.Scan(&javID, &stage); err != nil {
			t.Fatalf("scan stage: %v", err)
		}
		if stage != want[javID] {
			t.Fatalf("jav %d stage = %q, want %q", javID, stage, want[javID])
		}
	}
}

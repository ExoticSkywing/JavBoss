package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestAdvanceScrapedJavAcquisitionsUsesMetadataAndActiveInventory(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`
		CREATE TABLE jav (id integer PRIMARY KEY, title text, fetched_at datetime);
		CREATE TABLE jav_acquisition (jav_id integer PRIMARY KEY, stage text NOT NULL, updated_at datetime NOT NULL);
		CREATE TABLE directory (id integer PRIMARY KEY, is_delete numeric NOT NULL DEFAULT 0, missing numeric NOT NULL DEFAULT 0);
		CREATE TABLE video_location (id integer PRIMARY KEY, directory_id integer NOT NULL, jav_id integer, is_delete numeric NOT NULL DEFAULT 0);
		INSERT INTO jav (id, title, fetched_at) VALUES
			(1, 'Scraped title', NULL),
			(2, '', '2026-08-25 01:00:00'),
			(3, '', NULL),
			(4, 'Already collecting', NULL),
			(5, 'Already on disk', NULL),
			(6, 'Lost imported file', NULL),
			(7, '', NULL),
			(8, 'Hidden file', NULL),
			(9, 'Missing directory file', NULL),
			(10, 'Deleted directory file', NULL),
			(11, '', 1714000000);
		INSERT INTO jav_acquisition (jav_id, stage, updated_at) VALUES
			(1, 'metadata_pending', '2026-08-25 01:00:00'),
			(2, 'metadata_pending', '2026-08-25 01:00:00'),
			(3, 'metadata_pending', '2026-08-25 01:00:00'),
			(4, 'magnet_collecting', '2026-08-25 01:00:00'),
			(5, 'metadata_pending', '2026-08-25 01:00:00'),
			(6, 'imported', '2026-08-25 01:00:00'),
			(7, 'imported', '2026-08-25 01:00:00'),
			(8, 'imported', '2026-08-25 01:00:00'),
			(9, 'metadata_pending', '2026-08-25 01:00:00'),
			(10, 'imported', '2026-08-25 01:00:00'),
			(11, 'metadata_pending', '2026-08-25 01:00:00');
		INSERT INTO directory (id, is_delete, missing) VALUES
			(1, 0, 0), (2, 0, 0), (3, 1, 0), (4, 0, 1);
		INSERT INTO video_location (id, directory_id, jav_id, is_delete) VALUES
			(1, 1, 5, 0),
			(2, 2, 8, 1),
			(3, 4, 9, 0),
			(4, 3, 10, 0);
	`); err != nil {
		t.Fatalf("seed lifecycle schema: %v", err)
	}

	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin migration: %v", err)
	}
	if err := advanceScrapedJavAcquisitions(context.Background(), tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("advance scraped acquisitions: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit migration: %v", err)
	}

	rows, err := database.Query(`SELECT jav_id, stage FROM jav_acquisition ORDER BY jav_id`)
	if err != nil {
		t.Fatalf("load stages: %v", err)
	}
	want := map[int64]string{
		1:  "magnet_collecting",
		2:  "magnet_collecting",
		3:  "metadata_pending",
		4:  "magnet_collecting",
		5:  "imported",
		6:  "magnet_collecting",
		7:  "metadata_pending",
		8:  "magnet_collecting",
		9:  "imported",
		10: "magnet_collecting",
		11: "magnet_collecting",
	}
	for rows.Next() {
		var javID int64
		var stage string
		if err := rows.Scan(&javID, &stage); err != nil {
			t.Fatalf("scan stage: %v", err)
		}
		if stage != want[javID] {
			t.Fatalf("JAV %d stage = %q, want %q", javID, stage, want[javID])
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate stages: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close stages: %v", err)
	}

	// A second pass must be a true no-op, including updated_at. Startup may
	// safely retry a migration transaction after an interrupted deployment.
	if _, err := database.Exec(`UPDATE jav_acquisition SET updated_at = 'idempotence-sentinel'`); err != nil {
		t.Fatalf("set timestamp sentinel: %v", err)
	}
	retryTx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin idempotence pass: %v", err)
	}
	if err := advanceScrapedJavAcquisitions(context.Background(), retryTx); err != nil {
		_ = retryTx.Rollback()
		t.Fatalf("repeat acquisition reconciliation: %v", err)
	}
	if err := retryTx.Commit(); err != nil {
		t.Fatalf("commit idempotence pass: %v", err)
	}
	var changedTimestamps int
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM jav_acquisition
		WHERE updated_at <> 'idempotence-sentinel'
	`).Scan(&changedTimestamps); err != nil {
		t.Fatalf("count timestamp changes: %v", err)
	}
	if changedTimestamps != 0 {
		t.Fatalf("idempotent pass changed %d acquisition timestamps", changedTimestamps)
	}
}

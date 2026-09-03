package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestRepairPlaceholderJavMetadata(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`
		CREATE TABLE jav (id integer PRIMARY KEY, title text, studio_id integer, series_id integer, series_en_id integer, release_unix integer, duration_min integer, fetched_at datetime, is_uncensored numeric, sample_images text, jav_db_idols_reconciled_at datetime, updated_at datetime);
		CREATE TABLE jav_idol_map (jav_id integer, jav_idol_id integer);
		CREATE TABLE jav_tag_map (jav_id integer, jav_tag_id integer, provider integer);
		CREATE TABLE jav_acquisition (jav_id integer PRIMARY KEY, stage text NOT NULL, updated_at datetime NOT NULL);
		CREATE TABLE jav_magnet_candidate (jav_id integer);
		CREATE TABLE jav_download_attempt (jav_id integer);
		CREATE TABLE jav_quality_acceptance (jav_id integer);
		CREATE TABLE video_location (jav_id integer);
		INSERT INTO jav (id, title, studio_id, series_id, series_en_id, release_unix, duration_min, fetched_at, is_uncensored, sample_images, jav_db_idols_reconciled_at, updated_at)
		VALUES (1, '登入', 2, 3, 4, 10, 20, '2026-08-30', 1, '[1]', '2026-08-30', '2026-08-30'),
		       (2, '正常作品', 5, 6, 7, 11, 21, '2026-08-30', 0, '[2]', '2026-08-30', '2026-08-30');
		INSERT INTO jav_idol_map (jav_id, jav_idol_id) VALUES (1, 10), (2, 20);
		INSERT INTO jav_tag_map (jav_id, jav_tag_id, provider) VALUES (1, 11, 1), (1, 12, 3), (2, 21, 1);
		INSERT INTO jav_acquisition (jav_id, stage, updated_at) VALUES (1, 'magnet_collecting', '2026-08-30'), (2, 'magnet_collecting', '2026-08-30');
	`); err != nil {
		t.Fatalf("seed placeholder schema: %v", err)
	}

	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin migration: %v", err)
	}
	if err := repairPlaceholderJavMetadata(context.Background(), tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("repair placeholder metadata: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit migration: %v", err)
	}

	var title, stage, images string
	var studioID, seriesID, seriesEnID, releaseUnix, duration sql.NullInt64
	var uncensored sql.NullInt64
	if err := database.QueryRow(`SELECT title, stage, sample_images, studio_id, series_id, series_en_id, release_unix, duration_min, is_uncensored FROM jav JOIN jav_acquisition ON jav_acquisition.jav_id = jav.id WHERE jav.id = 1`).Scan(&title, &stage, &images, &studioID, &seriesID, &seriesEnID, &releaseUnix, &duration, &uncensored); err != nil {
		t.Fatalf("load repaired JAV: %v", err)
	}
	if title != "" || stage != "metadata_pending" || images != "[]" || studioID.Valid || seriesID.Valid || seriesEnID.Valid || !releaseUnix.Valid || releaseUnix.Int64 != 0 || !duration.Valid || duration.Int64 != 0 || uncensored.Valid {
		t.Fatalf("repaired JAV = title=%q stage=%q images=%q studio=%v series=%v series_en=%v release=%v duration=%v uncensored=%v", title, stage, images, studioID, seriesID, seriesEnID, releaseUnix, duration, uncensored)
	}
	var scrapedTags, userTags, idols int
	if err := database.QueryRow(`SELECT COUNT(*) FROM jav_tag_map WHERE jav_id = 1 AND provider <> 3`).Scan(&scrapedTags); err != nil {
		t.Fatalf("count scraped tags: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM jav_tag_map WHERE jav_id = 1 AND provider = 3`).Scan(&userTags); err != nil {
		t.Fatalf("count user tags: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM jav_idol_map WHERE jav_id = 1`).Scan(&idols); err != nil {
		t.Fatalf("count idols: %v", err)
	}
	if scrapedTags != 0 || userTags != 1 || idols != 0 {
		t.Fatalf("placeholder relationships scraped_tags=%d user_tags=%d idols=%d", scrapedTags, userTags, idols)
	}

	var untouched string
	if err := database.QueryRow(`SELECT title FROM jav WHERE id = 2`).Scan(&untouched); err != nil {
		t.Fatalf("load untouched JAV: %v", err)
	}
	if untouched != "正常作品" {
		t.Fatalf("normal title = %q", untouched)
	}
}

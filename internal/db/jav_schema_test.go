package db

import (
	"testing"

	"gorm.io/gorm"
)

func TestJavSchemaOmitsFrontendEnglishMetadataColumns(t *testing.T) {
	db := openTestDB(t)

	assertTableColumns(t, db, "jav", []string{
		"id", "code", "normalized_code", "title", "studio_id", "series_id", "series_en_id", "release_unix",
		"duration_min", "fetched_at", "created_at", "updated_at", "is_uncensored",
		"sample_images", "favorite_rating", "jav_db_idols_reconciled_at",
	})
	assertTableColumns(t, db, "jav_series", []string{
		"id", "name", "is_english", "studio_id", "created_at", "updated_at",
	})
	assertTableColumns(t, db, "jav_studio_alias", []string{
		"id", "jav_studio_id", "alias", "created_at",
	})
	assertTableColumns(t, db, "jav_idol", []string{
		"id", "name", "roman_name", "japanese_name", "chinese_name", "height_cm",
		"birth_date", "bust", "waist", "hips", "cup", "created_at", "updated_at",
		"cover_jav_id", "cover_crop_left",
	})
	assertTableColumns(t, db, "jav_idol_alias", []string{
		"id", "jav_idol_id", "alias", "created_at",
	})
	assertTableColumns(t, db, "jav_magnet_candidate", []string{
		"id", "jav_id", "info_hash", "uri", "name", "size_mi_b", "hd", "cn_sub", "files",
		"source_created_at", "first_seen_at", "last_seen_at", "review_status", "quality_clear",
		"confirmed1080_p", "has_intro_ad", "has_watermark", "has_marquee", "is_uncensored",
		"review_reasons", "review_notes", "reviewed_at", "created_at", "updated_at",
	})
	assertTableColumns(t, db, "jav_magnet_selection", []string{
		"jav_id", "candidate_id", "selected_at", "updated_at",
	})
	assertTableColumns(t, db, "jav_download_batch", []string{
		"id", "status", "external_batch_id", "error", "created_at", "submitted_at", "updated_at",
	})
	assertTableColumns(t, db, "jav_download_attempt", []string{
		"id", "batch_id", "jav_id", "candidate_id", "idempotency_key", "external_task_id", "status",
		"error", "created_at", "submitted_at", "completed_at",
	})
	assertTableColumns(t, db, "jav_quality_acceptance", []string{
		"id", "jav_id", "candidate_id", "attempt_id", "video_id", "location_id", "accepted_at",
		"notes", "created_at", "updated_at",
	})
}

func assertTableColumns(t *testing.T, db *gorm.DB, table string, want []string) {
	t.Helper()

	rows, err := db.Raw("PRAGMA table_info(" + table + ")").Rows()
	if err != nil {
		t.Fatalf("load %s columns: %v", table, err)
	}
	defer rows.Close()

	var got []string
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
			t.Fatalf("scan %s columns: %v", table, err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s columns: %v", table, err)
	}
	if len(got) != len(want) {
		t.Fatalf("%s columns = %#v, want %#v", table, got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("%s columns = %#v, want %#v", table, got, want)
		}
	}
}

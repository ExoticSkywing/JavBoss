package migrations

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestUnifyJavWorkLifecycleRejectsCanonicalCollisions(t *testing.T) {
	database := openJavLifecycleMigrationDB(t)
	if _, err := database.Exec(`INSERT INTO jav (code) VALUES ('IPX-001'), ('ipx_001')`); err != nil {
		t.Fatalf("seed JAV collision: %v", err)
	}

	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin migration: %v", err)
	}
	err = unifyJavWorkLifecycle(context.Background(), tx)
	_ = tx.Rollback()
	if err == nil || !strings.Contains(err.Error(), "canonical JAV code collisions") || !strings.Contains(err.Error(), "IPX001") {
		t.Fatalf("migration collision error = %v", err)
	}
}

func TestUnifyJavWorkLifecycleRefusesForeignKeysEnabled(t *testing.T) {
	database := openJavLifecycleMigrationDB(t)
	if _, err := database.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin migration: %v", err)
	}
	err = unifyJavWorkLifecycle(context.Background(), tx)
	_ = tx.Rollback()
	if err == nil || !strings.Contains(err.Error(), "requires PRAGMA foreign_keys=OFF") {
		t.Fatalf("foreign-key guard error = %v", err)
	}
}

func TestUnifyJavWorkLifecycleMaterializesAcceptedAndCanonicalizesHistory(t *testing.T) {
	database := openJavLifecycleMigrationDB(t)
	if _, err := database.Exec(`
		INSERT INTO jav_input_batch (raw_input, created_at) VALUES ('FC2 inputs', '2026-08-25 01:00:00');
		INSERT INTO jav_input_item (
			jav_input_batch_id, line_number, raw_line, code, normalized_code, status, created_at
		) VALUES
			(1, 1, 'FC2-PPV-1579280', 'FC2-PPV-1579280', 'FC2PPV1579280', 'accepted', '2026-08-25 01:00:00'),
			(1, 2, 'fc2_1579280 duplicate', 'FC2-1579280', 'FC21579280', 'duplicate_history', '2026-08-25 01:00:00'),
			(1, 3, 'FC2-1579280 accepted alias', 'FC2-1579280', 'FC21579280', 'accepted', '2026-08-25 01:00:00')
	`); err != nil {
		t.Fatalf("seed accepted input: %v", err)
	}

	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin migration: %v", err)
	}
	if err := unifyJavWorkLifecycle(context.Background(), tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("unify JAV lifecycle: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit migration: %v", err)
	}

	var javID int64
	var code, normalized, stage string
	if err := database.QueryRow(`
		SELECT j.id, j.code, j.normalized_code, ja.stage
		FROM jav j JOIN jav_acquisition ja ON ja.jav_id = j.id
	`).Scan(&javID, &code, &normalized, &stage); err != nil {
		t.Fatalf("load materialized work: %v", err)
	}
	if code != "FC2-PPV-1579280" || normalized != "FC21579280" || stage != javAcquisitionStageMetadataPending {
		t.Fatalf("materialized work = id=%d code=%q normalized=%q stage=%q", javID, code, normalized, stage)
	}
	rows, err := database.Query(`SELECT jav_id, normalized_code FROM jav_input_item ORDER BY id`)
	if err != nil {
		t.Fatalf("load linked input items: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var gotID int64
		var gotNormalized string
		if err := rows.Scan(&gotID, &gotNormalized); err != nil {
			t.Fatalf("scan linked input: %v", err)
		}
		if gotID != javID || gotNormalized != "FC21579280" {
			t.Fatalf("linked input = jav_id=%d normalized=%q", gotID, gotNormalized)
		}
	}
}

func TestUnifyJavWorkLifecyclePreservesJavChildRows(t *testing.T) {
	database := openJavLifecycleMigrationDB(t)
	if _, err := database.Exec(`
		CREATE TABLE jav_tag_map (
			jav_id integer NOT NULL,
			jav_tag_id integer NOT NULL,
			FOREIGN KEY (jav_id) REFERENCES jav(id) ON DELETE CASCADE
		);
		CREATE TABLE jav_idol_map (
			jav_id integer NOT NULL,
			jav_idol_id integer NOT NULL,
			FOREIGN KEY (jav_id) REFERENCES jav(id) ON DELETE CASCADE
		);
		INSERT INTO jav (id, code) VALUES (1, 'IPX-001');
		INSERT INTO jav_tag_map (jav_id, jav_tag_id) VALUES (1, 10);
		INSERT INTO jav_idol_map (jav_id, jav_idol_id) VALUES (1, 20);
	`); err != nil {
		t.Fatalf("seed JAV child rows: %v", err)
	}
	tx, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin migration: %v", err)
	}
	if err := unifyJavWorkLifecycle(context.Background(), tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("unify JAV lifecycle: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit migration: %v", err)
	}
	var tagRows, idolRows int
	if err := database.QueryRow(`SELECT COUNT(*) FROM jav_tag_map`).Scan(&tagRows); err != nil {
		t.Fatalf("count tag mappings: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM jav_idol_map`).Scan(&idolRows); err != nil {
		t.Fatalf("count idol mappings: %v", err)
	}
	if tagRows != 1 || idolRows != 1 {
		t.Fatalf("child rows after migration = tags:%d idols:%d, want 1/1", tagRows, idolRows)
	}
	if _, err := database.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys after migration: %v", err)
	}
	rows, err := database.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign key check: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatalf("foreign key check reported an invalid child row")
	}
}

func openJavLifecycleMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`
		PRAGMA foreign_keys = OFF;
		CREATE TABLE jav_studio (id integer PRIMARY KEY);
		CREATE TABLE jav_series (id integer PRIMARY KEY);
		CREATE TABLE jav (
			id integer PRIMARY KEY AUTOINCREMENT,
			code text,
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
			sample_images text NOT NULL DEFAULT '[]',
			favorite_rating real NOT NULL DEFAULT 0
		);
		CREATE UNIQUE INDEX idx_jav_code ON jav(code);
		CREATE TABLE jav_input_batch (
			id integer PRIMARY KEY AUTOINCREMENT,
			raw_input text NOT NULL,
			created_at datetime NOT NULL
		);
		CREATE TABLE jav_input_item (
			id integer PRIMARY KEY AUTOINCREMENT,
			jav_input_batch_id integer NOT NULL,
			line_number integer NOT NULL,
			raw_line text NOT NULL,
			code text NOT NULL DEFAULT '',
			normalized_code text NOT NULL DEFAULT '',
			status text NOT NULL,
			duplicate_of_line integer NOT NULL DEFAULT 0,
			existing_batch_id integer,
			existing_jav_id integer,
			created_at datetime NOT NULL,
			FOREIGN KEY (jav_input_batch_id) REFERENCES jav_input_batch(id) ON UPDATE CASCADE ON DELETE CASCADE
		);
		CREATE UNIQUE INDEX idx_jav_input_item_accepted_code ON jav_input_item(normalized_code) WHERE status = 'accepted';
	`); err != nil {
		t.Fatalf("create lifecycle migration schema: %v", err)
	}
	return database
}

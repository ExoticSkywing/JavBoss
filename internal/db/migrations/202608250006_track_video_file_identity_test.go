package migrations

import (
	"context"
	"database/sql"
	"testing"
)

func TestTrackVideoFileIdentityAddsColumnIdempotently(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`CREATE TABLE video_location (id INTEGER PRIMARY KEY, relative_path TEXT)`); err != nil {
		t.Fatalf("create video location table: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		tx, err := database.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatalf("begin migration attempt %d: %v", attempt+1, err)
		}
		if err := trackVideoFileIdentity(context.Background(), tx); err != nil {
			_ = tx.Rollback()
			t.Fatalf("track file identity attempt %d: %v", attempt+1, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit migration attempt %d: %v", attempt+1, err)
		}
	}
	var columnCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('video_location') WHERE name = 'file_identity'`).Scan(&columnCount); err != nil {
		t.Fatalf("inspect file identity column: %v", err)
	}
	if columnCount != 1 {
		t.Fatalf("file identity column count = %d, want 1", columnCount)
	}
}

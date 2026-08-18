package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

const javDiscoveryMagnetMigrationVersion = int64(202608180001)

func TestJavDiscoveryMagnetMigrationUpgradesExistingItems(t *testing.T) {
	driverName := registerSQLiteFunctions()
	sqlDB, err := sql.Open(driverName, filepath.Join(t.TempDir(), "jav-discovery-magnets.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	sqlDB.SetMaxOpenConns(1)

	ctx := context.Background()
	if err := goose.UpToContext(ctx, sqlDB, migrationDir, javDiscoveryMigrationVersion); err != nil {
		t.Fatalf("migrate to JAV discovery schema: %v", err)
	}
	if _, err := sqlDB.ExecContext(
		ctx,
		`INSERT INTO jav_discovery_item (code) VALUES (?)`,
		"ABC-001",
	); err != nil {
		t.Fatalf("insert existing discovery item: %v", err)
	}
	if err := goose.UpToContext(ctx, sqlDB, migrationDir, javDiscoveryMagnetMigrationVersion); err != nil {
		t.Fatalf("apply magnet links migration: %v", err)
	}
	assertMigrationVersion(t, sqlDB, javDiscoveryMagnetMigrationVersion)

	var magnetLinksJSON string
	if err := sqlDB.QueryRowContext(
		ctx,
		`SELECT magnet_links_json FROM jav_discovery_item WHERE code = ?`,
		"ABC-001",
	).Scan(&magnetLinksJSON); err != nil {
		t.Fatalf("read migrated magnet links: %v", err)
	}
	if magnetLinksJSON != "null" {
		t.Fatalf("migrated magnet links = %q, want JSON null", magnetLinksJSON)
	}
}

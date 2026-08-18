package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202608180001_add_jav_discovery_magnet_links.go",
		addJavDiscoveryMagnetLinks,
		irreversibleMigration,
	)
}

func addJavDiscoveryMagnetLinks(ctx context.Context, tx *sql.Tx) error {
	return execStatements(ctx, tx,
		`ALTER TABLE jav_discovery_item ADD COLUMN magnet_links_json text NOT NULL DEFAULT "null"`,
	)
}

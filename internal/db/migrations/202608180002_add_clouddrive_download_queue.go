package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202608180002_add_clouddrive_download_queue.go",
		addCloudDriveDownloadQueue,
		irreversibleMigration,
	)
}

func addCloudDriveDownloadQueue(ctx context.Context, tx *sql.Tx) error {
	return execStatements(ctx, tx,
		`CREATE TABLE IF NOT EXISTS "cloud_drive_settings" (
			id integer PRIMARY KEY AUTOINCREMENT,
			address text NOT NULL DEFAULT "",
			api_token text NOT NULL DEFAULT "",
			remote_folder text NOT NULL DEFAULT "",
			directory_id integer,
			enabled numeric NOT NULL DEFAULT 0,
			created_at datetime,
			updated_at datetime
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cloud_drive_settings_directory_id ON cloud_drive_settings(directory_id)`,
		`CREATE TABLE IF NOT EXISTS "jav_discovery_download" (
			id integer PRIMARY KEY AUTOINCREMENT,
			jav_discovery_item_id integer NOT NULL,
			directory_id integer NOT NULL,
			info_hash text NOT NULL,
			magnet_url text NOT NULL,
			magnet_name text NOT NULL DEFAULT "",
			remote_folder text NOT NULL DEFAULT "",
			status text NOT NULL DEFAULT "queued",
			progress real NOT NULL DEFAULT 0,
			bytes_total integer NOT NULL DEFAULT 0,
			bytes_downloaded integer NOT NULL DEFAULT 0,
			local_files_json text NOT NULL DEFAULT "[]",
			error_message text NOT NULL DEFAULT "",
			created_at datetime,
			updated_at datetime,
			completed_at datetime,
			CONSTRAINT fk_jav_discovery_download_jav_discovery_item FOREIGN KEY (jav_discovery_item_id) REFERENCES jav_discovery_item(id) ON UPDATE CASCADE ON DELETE CASCADE,
			CONSTRAINT fk_jav_discovery_download_directory FOREIGN KEY (directory_id) REFERENCES directory(id) ON UPDATE CASCADE ON DELETE RESTRICT
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_jav_discovery_download_target_hash ON jav_discovery_download(jav_discovery_item_id, directory_id, info_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_discovery_download_jav_discovery_item_id ON jav_discovery_download(jav_discovery_item_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_discovery_download_directory_id ON jav_discovery_download(directory_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jav_discovery_download_status ON jav_discovery_download(status)`,
	)
}

package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext(
		"202608250006_track_video_file_identity.go",
		trackVideoFileIdentity,
		irreversibleMigration,
	)
}

// trackVideoFileIdentity adds a scanner-only stat identity. Existing rows are
// left empty deliberately; their next successful scan probes once and fills
// the identity before returning to the cheap size/mtime fast path.
func trackVideoFileIdentity(ctx context.Context, tx *sql.Tx) error {
	return addColumnIfMissing(ctx, tx, "video_location", "file_identity", `text NOT NULL DEFAULT ""`)
}

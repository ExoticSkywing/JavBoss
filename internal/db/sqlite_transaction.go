package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

// sqliteTransactionRetryDelays keeps stale-snapshot recovery well below the
// connection-level busy_timeout. A retry always reruns the complete
// transaction so reads and writes use the same fresh SQLite snapshot.
var sqliteTransactionRetryDelays = [...]time.Duration{
	5 * time.Millisecond,
	10 * time.Millisecond,
	20 * time.Millisecond,
	40 * time.Millisecond,
	80 * time.Millisecond,
}

func withSQLiteTransactionRetry(ctx context.Context, database *gorm.DB, fn func(*gorm.DB) error) error {
	if database == nil {
		return errors.New("database is not initialized")
	}
	if ctx == nil {
		return errors.New("context is nil")
	}
	if fn == nil {
		return errors.New("transaction callback is nil")
	}

	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := database.WithContext(ctx).Transaction(fn)
		if err == nil {
			return nil
		}
		retryLimit := sqliteTransactionRetryLimit(err)
		if retryLimit == 0 || attempt >= retryLimit {
			return err
		}

		timer := time.NewTimer(sqliteTransactionRetryDelays[attempt])
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func sqliteTransactionRetryLimit(err error) int {
	if err == nil {
		return 0
	}
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		if sqliteErr.ExtendedCode == sqlite3.ErrBusySnapshot ||
			sqliteErr.ExtendedCode == sqlite3.ErrLockedSharedCache ||
			sqliteErr.Code == sqlite3.ErrLocked {
			return len(sqliteTransactionRetryDelays)
		}
		// A plain SQLITE_BUSY already waited for the connection's busy_timeout.
		// Repeating it here would turn one bounded 5-second wait into ~30 seconds.
		return 0
	}

	// Keep compatibility with errors wrapped by database/sql or GORM versions
	// that do not retain the concrete sqlite3 error value. A generic database
	// lock gets only one extra attempt because it may already have consumed the
	// connection-level busy timeout.
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "database table is locked") {
		return len(sqliteTransactionRetryDelays)
	}
	if strings.Contains(message, "database is locked") {
		return 1
	}
	return 0
}

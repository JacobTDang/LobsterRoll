// Package sqlitex opens a CGO-free SQLite database with the project's standard
// single-writer settings, shared by every service that persists state. Callers
// run their own CREATE TABLE / migrations on the returned *sql.DB.
package sqlitex

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // CGO-free SQLite driver, registered as "sqlite".
)

// Open opens (creating if needed) the database at path and applies the standard
// pragmas. modernc's driver is safe for concurrent use, but a single open
// connection avoids "database is locked" under concurrent writes; WAL +
// busy_timeout improve durability and turn transient lock contention into a
// short wait rather than an immediate error (synchronous=NORMAL is safe in WAL).
func Open(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA busy_timeout=5000",
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("set %q: %w", pragma, err)
		}
	}
	return db, nil
}

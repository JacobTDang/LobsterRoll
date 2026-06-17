// Package store persists the watcher's last-processed block so it can resume
// gap-free after a restart.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	_ "modernc.org/sqlite" // CGO-free SQLite driver, registered as "sqlite".
)

const metaLastBlock = "last_processed_block"

// Store is a SQLite-backed key/value for watcher cursors.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and ensures the schema.
func Open(ctx context.Context, path string) (*Store, error) {
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
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// LastProcessedBlock returns the last processed block and whether one is set.
func (s *Store) LastProcessedBlock(ctx context.Context) (block uint64, ok bool, err error) {
	var v string
	err = s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaLastBlock).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get last block: %w", err)
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse last block %q: %w", v, err)
	}
	return n, true, nil
}

// SetLastProcessedBlock records the last processed block.
func (s *Store) SetLastProcessedBlock(ctx context.Context, block uint64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		metaLastBlock, strconv.FormatUint(block, 10))
	if err != nil {
		return fmt.Errorf("set last block: %w", err)
	}
	return nil
}

// Package store persists periodic market-price snapshots so Closing Line Value
// can be computed later. Polymarket's /prices-history only returns coarse (>=12h)
// granularity for RESOLVED markets, so a fine-grained closing line cannot be
// backfilled — it must be captured live while the market trades and kept here.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/JacobTDang/LobsterRoll/pkg/sqlitex"
)

// Store is a SQLite-backed price-snapshot log.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the snapshot DB and ensures the schema.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sqlitex.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS price_snapshots (
			token_id TEXT    NOT NULL,
			ts       INTEGER NOT NULL,
			mid      REAL    NOT NULL,
			PRIMARY KEY (token_id, ts)
		);
		CREATE INDEX IF NOT EXISTS idx_snapshots_token_ts ON price_snapshots (token_id, ts);`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Put records a midprice snapshot for tokenID at ts (unix seconds). Re-recording
// the same (token, ts) overwrites — captures are idempotent.
func (s *Store) Put(ctx context.Context, tokenID string, ts int64, mid float64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO price_snapshots (token_id, ts, mid) VALUES (?, ?, ?)
		 ON CONFLICT(token_id, ts) DO UPDATE SET mid = excluded.mid`,
		tokenID, ts, mid)
	if err != nil {
		return fmt.Errorf("put snapshot %s@%d: %w", tokenID, ts, err)
	}
	return nil
}

// Snapshot is a recorded midprice at a point in time.
type Snapshot struct {
	TS  int64
	Mid float64
}

// ErrNoSnapshot is returned when no snapshot exists for the query.
var ErrNoSnapshot = errors.New("no snapshot")

// Nearest returns the snapshot whose timestamp is closest to targetTS — used to
// read the price near a market's close (e.g. resolution time minus a few hours)
// for the closing-line comparison. Returns ErrNoSnapshot if the token has none.
func (s *Store) Nearest(ctx context.Context, tokenID string, targetTS int64) (Snapshot, error) {
	// The closest row is whichever of the nearest-at-or-after and nearest-before
	// has the smaller absolute time delta.
	var snap Snapshot
	err := s.db.QueryRowContext(ctx,
		`SELECT ts, mid FROM price_snapshots
		 WHERE token_id = ?
		 ORDER BY ABS(ts - ?) ASC, ts DESC
		 LIMIT 1`, tokenID, targetTS).Scan(&snap.TS, &snap.Mid)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, ErrNoSnapshot
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("nearest snapshot %s: %w", tokenID, err)
	}
	return snap, nil
}

// Prune deletes snapshots older than cutoff (unix seconds) to bound growth once
// CLV for those markets has been computed.
func (s *Store) Prune(ctx context.Context, cutoff int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM price_snapshots WHERE ts < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("prune snapshots: %w", err)
	}
	return nil
}

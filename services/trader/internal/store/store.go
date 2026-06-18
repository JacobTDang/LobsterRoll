// Package store persists placement idempotency: a proposal is claimed before
// placement so it can never be placed twice (at-most-once execution), surviving
// restarts.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // CGO-free driver.
)

// Store records placement attempts.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the DB and ensures the schema.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	for _, p := range []string{"PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL"} {
		if _, err := db.ExecContext(ctx, p); err != nil {
			db.Close()
			return nil, fmt.Errorf("set %q: %w", p, err)
		}
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS placements (
			proposal_id TEXT PRIMARY KEY,
			order_id    TEXT NOT NULL DEFAULT '',
			status      TEXT NOT NULL,
			claimed_at  INTEGER NOT NULL
		)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the DB.
func (s *Store) Close() error { return s.db.Close() }

// Claim atomically reserves a proposal for placement. It returns true only on
// the first call for a proposal id; subsequent calls return false (already
// claimed) so the order is never placed twice.
func (s *Store) Claim(ctx context.Context, proposalID string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO placements (proposal_id, status, claimed_at) VALUES (?, 'claimed', ?)
		 ON CONFLICT(proposal_id) DO NOTHING`,
		proposalID, time.Now().Unix())
	if err != nil {
		return false, fmt.Errorf("claim %s: %w", proposalID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim rows %s: %w", proposalID, err)
	}
	return n == 1, nil
}

// MarkResult records the outcome of a placement.
func (s *Store) MarkResult(ctx context.Context, proposalID, orderID, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE placements SET order_id = ?, status = ? WHERE proposal_id = ?`,
		orderID, status, proposalID)
	if err != nil {
		return fmt.Errorf("mark result %s: %w", proposalID, err)
	}
	return nil
}

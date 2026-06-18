// Package store persists placement idempotency: a proposal is claimed before
// placement so it can never be placed twice (at-most-once execution), surviving
// restarts.
package store

import (
	"context"
	"database/sql"
	"errors"
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
		);
		CREATE TABLE IF NOT EXISTS caps_ledger (
			id            INTEGER PRIMARY KEY CHECK (id = 1),
			day_key       TEXT NOT NULL,
			day_spent     REAL NOT NULL,
			open_exposure REAL NOT NULL
		);`); err != nil {
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

// Unclaim removes a still-'claimed' placement so a definitely-not-sent proposal
// (e.g. a pre-network sign failure) can be retried on redelivery.
func (s *Store) Unclaim(ctx context.Context, proposalID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM placements WHERE proposal_id = ? AND status = 'claimed'`, proposalID)
	if err != nil {
		return fmt.Errorf("unclaim %s: %w", proposalID, err)
	}
	return nil
}

// LoadCaps reads the persisted cap ledger. ok=false if none has been saved.
func (s *Store) LoadCaps(ctx context.Context) (dayKey string, daySpent, openExposure float64, ok bool, err error) {
	row := s.db.QueryRowContext(ctx, `SELECT day_key, day_spent, open_exposure FROM caps_ledger WHERE id = 1`)
	err = row.Scan(&dayKey, &daySpent, &openExposure)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, 0, false, nil
	}
	if err != nil {
		return "", 0, 0, false, fmt.Errorf("load caps: %w", err)
	}
	return dayKey, daySpent, openExposure, true, nil
}

// SaveCaps persists the cap ledger (single row).
func (s *Store) SaveCaps(ctx context.Context, dayKey string, daySpent, openExposure float64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO caps_ledger (id, day_key, day_spent, open_exposure) VALUES (1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET day_key=excluded.day_key, day_spent=excluded.day_spent, open_exposure=excluded.open_exposure`,
		dayKey, daySpent, openExposure)
	if err != nil {
		return fmt.Errorf("save caps: %w", err)
	}
	return nil
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

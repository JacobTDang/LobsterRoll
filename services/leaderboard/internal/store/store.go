// Package store persists the watchset (the set of wallets to copy-trade) in a
// CGO-free SQLite database and computes the diff between successive syncs.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	_ "modernc.org/sqlite" // CGO-free SQLite driver, registered as "sqlite".
)

// Delta describes how a watchset changed: wallets newly added and wallets
// removed, each sorted ascending.
type Delta struct {
	Added   []string
	Removed []string
}

// Empty reports whether nothing changed.
func (d Delta) Empty() bool { return len(d.Added) == 0 && len(d.Removed) == 0 }

// Diff computes the change from old to new. Order of inputs is irrelevant; the
// returned slices are sorted and nil when empty.
func Diff(old, new []string) Delta {
	oldSet := make(map[string]struct{}, len(old))
	for _, w := range old {
		oldSet[w] = struct{}{}
	}
	newSet := make(map[string]struct{}, len(new))
	for _, w := range new {
		newSet[w] = struct{}{}
	}

	var d Delta
	for w := range newSet {
		if _, ok := oldSet[w]; !ok {
			d.Added = append(d.Added, w)
		}
	}
	for w := range oldSet {
		if _, ok := newSet[w]; !ok {
			d.Removed = append(d.Removed, w)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Removed)
	return d
}

// Store is a SQLite-backed watchset.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the watchset database at path and ensures the
// schema exists.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	// modernc's driver is safe for concurrent use, but a single writer avoids
	// "database is locked" under concurrent writes.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS watchset (
			wallet   TEXT PRIMARY KEY,
			added_at INTEGER NOT NULL
		)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// List returns the current watchset, sorted ascending.
func (s *Store) List(ctx context.Context) ([]string, error) {
	return listQuerier(ctx, s.db)
}

// Replace sets the watchset to exactly wallets, applying the change atomically,
// and returns the diff versus the previous contents.
func (s *Store) Replace(ctx context.Context, wallets []string) (Delta, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Delta{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit.

	current, err := listQuerier(ctx, tx)
	if err != nil {
		return Delta{}, err
	}
	d := Diff(current, wallets)
	if d.Empty() {
		return d, nil // nothing to write; leave added_at timestamps intact.
	}

	now := time.Now().Unix()
	for _, w := range d.Added {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO watchset (wallet, added_at) VALUES (?, ?)`, w, now); err != nil {
			return Delta{}, fmt.Errorf("insert %s: %w", w, err)
		}
	}
	for _, w := range d.Removed {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM watchset WHERE wallet = ?`, w); err != nil {
			return Delta{}, fmt.Errorf("delete %s: %w", w, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Delta{}, fmt.Errorf("commit: %w", err)
	}
	return d, nil
}

// querier is satisfied by both *sql.DB and *sql.Tx.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func listQuerier(ctx context.Context, q querier) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT wallet FROM watchset ORDER BY wallet`)
	if err != nil {
		return nil, fmt.Errorf("list watchset: %w", err)
	}
	defer rows.Close()

	var wallets []string
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			return nil, fmt.Errorf("scan wallet: %w", err)
		}
		wallets = append(wallets, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate watchset: %w", err)
	}
	return wallets, nil
}

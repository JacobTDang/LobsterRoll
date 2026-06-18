// Package window persists recent detected trades in a rolling time window and
// reports when distinct tracked wallets converge on the same outcome token and
// side — the consensus copy signal. It is backed by SQLite (CGO-free) so the
// window survives restarts.
package window

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	_ "modernc.org/sqlite" // CGO-free SQLite driver, registered as "sqlite".
)

// Store records trade events and reports the distinct-wallet cohort per
// (token_id, side) within a rolling window. It tracks which cohort sizes have
// already fired so a signal is emitted only when the cohort first reaches the
// threshold and again each time it grows.
type Store struct {
	db     *sql.DB
	window time.Duration
	now    func() time.Time
}

// Cohort is the distinct-wallet set for a (token_id, side) within the window.
type Cohort struct {
	Wallets     []string // distinct, lowercased, sorted
	CombinedUSD float64  // summed size*price across the window's rows
}

// Count is the number of distinct wallets in the cohort.
func (c Cohort) Count() int { return len(c.Wallets) }

// Open opens (creating if needed) the database at path and ensures the schema.
// window is the rolling retention/aggregation window; now supplies the clock
// (pass nil for time.Now).
func Open(ctx context.Context, path string, window time.Duration, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
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
		CREATE TABLE IF NOT EXISTS trade_events (
			token_id      TEXT    NOT NULL,
			side          TEXT    NOT NULL,
			wallet        TEXT    NOT NULL,
			usdc          REAL    NOT NULL,
			observed_unix INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_trade_events_key
			ON trade_events (token_id, side, observed_unix);
		CREATE TABLE IF NOT EXISTS fired (
			token_id   TEXT    NOT NULL,
			side       TEXT    NOT NULL,
			count      INTEGER NOT NULL,
			fired_unix INTEGER NOT NULL,
			PRIMARY KEY (token_id, side)
		)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db, window: window, now: now}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Record ingests a detected trade: it inserts the event, prunes rows older than
// the window, and returns the resulting distinct-wallet cohort for the event's
// (token_id, side) within the window. Distinct wallets are de-duplicated by
// lowercased address. The trade's USDC value is size*price (unparseable => 0).
func (s *Store) Record(ctx context.Context, ev bus.TradeDetected) (Cohort, error) {
	now := s.now()
	nowUnix := now.Unix()
	cutoff := now.Add(-s.window).Unix()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Cohort{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	token := ev.TokenID
	side := strings.ToLower(ev.Side)
	wallet := strings.ToLower(strings.TrimSpace(ev.Wallet))
	usdc := usdcValue(ev.Size, ev.Price)

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO trade_events (token_id, side, wallet, usdc, observed_unix)
		 VALUES (?, ?, ?, ?, ?)`,
		token, side, wallet, usdc, nowUnix); err != nil {
		return Cohort{}, fmt.Errorf("insert event: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM trade_events WHERE observed_unix < ?`, cutoff); err != nil {
		return Cohort{}, fmt.Errorf("prune: %w", err)
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT wallet, SUM(usdc) FROM trade_events
		 WHERE token_id = ? AND side = ? AND observed_unix >= ?
		 GROUP BY wallet`,
		token, side, cutoff)
	if err != nil {
		return Cohort{}, fmt.Errorf("aggregate: %w", err)
	}
	defer rows.Close()

	var c Cohort
	for rows.Next() {
		var w string
		var sum float64
		if err := rows.Scan(&w, &sum); err != nil {
			return Cohort{}, fmt.Errorf("scan: %w", err)
		}
		c.Wallets = append(c.Wallets, w)
		c.CombinedUSD += sum
	}
	if err := rows.Err(); err != nil {
		return Cohort{}, fmt.Errorf("rows: %w", err)
	}
	sort.Strings(c.Wallets)

	if err := tx.Commit(); err != nil {
		return Cohort{}, fmt.Errorf("commit: %w", err)
	}
	return c, nil
}

// ShouldFire reports whether a signal should be emitted for this (token_id,
// side) at the given cohort count: it returns true the first time count reaches
// or exceeds the threshold and again only when count grows beyond the largest
// count already fired. It records the new high-water count when it returns true.
// A repeat at the same (or smaller) cohort size returns false.
func (s *Store) ShouldFire(ctx context.Context, tokenID, side string, count int) (bool, error) {
	side = strings.ToLower(side)
	var prev int
	err := s.db.QueryRowContext(ctx,
		`SELECT count FROM fired WHERE token_id = ? AND side = ?`,
		tokenID, side).Scan(&prev)
	switch {
	case err == sql.ErrNoRows:
		prev = 0
	case err != nil:
		return false, fmt.Errorf("read fired: %w", err)
	}
	if count <= prev {
		return false, nil
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO fired (token_id, side, count, fired_unix) VALUES (?, ?, ?, ?)
		 ON CONFLICT(token_id, side) DO UPDATE SET count = excluded.count, fired_unix = excluded.fired_unix`,
		tokenID, side, count, s.now().Unix()); err != nil {
		return false, fmt.Errorf("update fired: %w", err)
	}
	return true, nil
}

// usdcValue returns size*price as a float, or 0 if either is unparseable.
func usdcValue(size, price string) float64 {
	sz, ok := new(big.Float).SetString(strings.TrimSpace(size))
	if !ok {
		return 0
	}
	pr, ok := new(big.Float).SetString(strings.TrimSpace(price))
	if !ok {
		return 0
	}
	v, _ := new(big.Float).Mul(sz, pr).Float64()
	return v
}

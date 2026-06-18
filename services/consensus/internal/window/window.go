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
	"github.com/JacobTDang/LobsterRoll/pkg/sqlitex"
)

// Store records trade events and reports the distinct-wallet cohort per
// (token_id, side) within a rolling window. It tracks which cohort sizes have
// already fired so a signal is emitted only when the cohort first reaches the
// threshold and again each time it grows.
type Store struct {
	db         *sql.DB
	window     time.Duration
	minWallets int
	now        func() time.Time
}

// Cohort is the distinct-wallet set for a (token_id, side) within the window.
type Cohort struct {
	Wallets     []string // distinct, lowercased, sorted
	CombinedUSD float64  // summed size*price across the window's rows
}

// Count is the number of distinct wallets in the cohort.
func (c Cohort) Count() int { return len(c.Wallets) }

// Open opens (creating if needed) the database at path and ensures the schema.
// window is the rolling retention/aggregation window; minWallets is the distinct-
// cohort size that triggers a signal; now supplies the clock (pass nil for time.Now).
func Open(ctx context.Context, path string, window time.Duration, minWallets int, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	db, err := sqlitex.Open(ctx, path)
	if err != nil {
		return nil, err
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
	return &Store{db: db, window: window, minWallets: minWallets, now: now}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Record ingests a detected trade and, in one transaction, inserts the event,
// prunes rows older than the window, computes the distinct-wallet cohort for the
// event's (token_id, side), and decides whether to fire a consensus signal.
//
// Fire semantics (high-water per token+side): fire when the cohort first reaches
// minWallets and again each time it grows to a NEW maximum. When the live cohort
// falls BELOW minWallets the high-water is reset to 0, so a cohort that fully
// dissipates and later re-forms fires again. A repeat at the same size does not
// fire. Distinct wallets are de-duplicated by lowercased address; the trade's
// USDC value is size*price (unparseable => 0).
func (s *Store) Record(ctx context.Context, ev bus.TradeDetected) (Cohort, bool, error) {
	now := s.now()
	nowUnix := now.Unix()
	cutoff := now.Add(-s.window).Unix()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Cohort{}, false, fmt.Errorf("begin: %w", err)
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
		return Cohort{}, false, fmt.Errorf("insert event: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM trade_events WHERE observed_unix < ?`, cutoff); err != nil {
		return Cohort{}, false, fmt.Errorf("prune: %w", err)
	}

	// Reap fired rows whose cohort has fully dissipated (no events left in the
	// window). Without this, a token+side that fires once and never trades again
	// leaks its row forever. Keyed on "no in-window events" (NOT fired_unix,
	// which tracks last growth, not last activity — an active-but-not-growing
	// cohort must keep its high-water). The current event's token+side always has
	// the just-inserted row, so it is never reaped here; decideFire handles it.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM fired WHERE NOT EXISTS (
			SELECT 1 FROM trade_events te
			WHERE te.token_id = fired.token_id AND te.side = fired.side
			  AND te.observed_unix >= ?)`, cutoff); err != nil {
		return Cohort{}, false, fmt.Errorf("reap fired: %w", err)
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT wallet, SUM(usdc) FROM trade_events
		 WHERE token_id = ? AND side = ? AND observed_unix >= ?
		 GROUP BY wallet`,
		token, side, cutoff)
	if err != nil {
		return Cohort{}, false, fmt.Errorf("aggregate: %w", err)
	}
	var c Cohort
	for rows.Next() {
		var w string
		var sum float64
		if err := rows.Scan(&w, &sum); err != nil {
			rows.Close()
			return Cohort{}, false, fmt.Errorf("scan: %w", err)
		}
		c.Wallets = append(c.Wallets, w)
		c.CombinedUSD += sum
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Cohort{}, false, fmt.Errorf("rows: %w", err)
	}
	rows.Close()
	sort.Strings(c.Wallets)

	fire, err := s.decideFire(ctx, tx, token, side, c.Count(), nowUnix)
	if err != nil {
		return Cohort{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return Cohort{}, false, fmt.Errorf("commit: %w", err)
	}
	return c, fire, nil
}

// decideFire applies the high-water fire policy inside the caller's transaction.
func (s *Store) decideFire(ctx context.Context, tx *sql.Tx, token, side string, count int, nowUnix int64) (bool, error) {
	var prev int
	err := tx.QueryRowContext(ctx,
		`SELECT count FROM fired WHERE token_id = ? AND side = ?`, token, side).Scan(&prev)
	switch {
	case err == sql.ErrNoRows:
		prev = 0
	case err != nil:
		return false, fmt.Errorf("read fired: %w", err)
	}

	switch {
	case count < s.minWallets:
		// Cohort dissipated below threshold: reset so a fresh cohort can re-fire.
		if prev != 0 {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM fired WHERE token_id = ? AND side = ?`, token, side); err != nil {
				return false, fmt.Errorf("reset fired: %w", err)
			}
		}
		return false, nil
	case count > prev:
		// First reach of the threshold, or growth to a new maximum: fire.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO fired (token_id, side, count, fired_unix) VALUES (?, ?, ?, ?)
			 ON CONFLICT(token_id, side) DO UPDATE SET count = excluded.count, fired_unix = excluded.fired_unix`,
			token, side, count, nowUnix); err != nil {
			return false, fmt.Errorf("update fired: %w", err)
		}
		return true, nil
	default:
		// At or below the high-water (but still >= threshold): already fired.
		return false, nil
	}
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

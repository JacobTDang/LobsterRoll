package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// StatsRecord is a persisted per-wallet consistency stats row.
type StatsRecord struct {
	Wallet          string
	WinRate         float64
	ResolvedMarkets int64
	RealizedPnL     float64
	ROI             float64
	SkillScore      int64 // 0–100 percentile by shrunk ROI within the population
	Fresh           bool  // false = cooling off (recent downward regime)
	Profit30D       float64
	PortfolioValue  float64
	TradedMarkets   int64
	ComputedUnix    int64
}

// ensureStatsSchema creates the wallet_stats table. It is called lazily from
// the stats methods so the existing Open() schema is unchanged and old DBs
// migrate forward on first stats use.
func (s *Store) ensureStatsSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS wallet_stats (
			proxy_wallet     TEXT PRIMARY KEY,
			win_rate         REAL    NOT NULL,
			resolved_markets INTEGER NOT NULL,
			realized_pnl     REAL    NOT NULL,
			profit_30d       REAL    NOT NULL,
			portfolio_value  REAL    NOT NULL,
			traded_markets   INTEGER NOT NULL,
			computed_unix    INTEGER NOT NULL,
			roi              REAL    NOT NULL DEFAULT 0
		)`)
	if err != nil {
		return fmt.Errorf("create wallet_stats schema: %w", err)
	}
	// Migrate older stats DBs forward; ignore "duplicate column".
	for _, col := range []string{
		`ALTER TABLE wallet_stats ADD COLUMN roi REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE wallet_stats ADD COLUMN skill_score INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE wallet_stats ADD COLUMN fresh INTEGER NOT NULL DEFAULT 1`,
	} {
		if _, err := s.db.ExecContext(ctx, col); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("migrate wallet_stats: %w", err)
		}
	}
	return nil
}

// b2i maps a bool to SQLite's 0/1 integer.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SetSkillScore updates only the skill score for wallet (computed population-wide
// after the per-wallet stats are upserted). No-op if the row is absent.
func (s *Store) SetSkillScore(ctx context.Context, wallet string, score int64) error {
	if err := s.ensureStatsSchema(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE wallet_stats SET skill_score = ? WHERE proxy_wallet = ?`, score, wallet)
	if err != nil {
		return fmt.Errorf("set skill score %s: %w", wallet, err)
	}
	return nil
}

// UpsertStats inserts or replaces the stats row for r.Wallet.
func (s *Store) UpsertStats(ctx context.Context, r StatsRecord) error {
	if err := s.ensureStatsSchema(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO wallet_stats
		   (proxy_wallet, win_rate, resolved_markets, realized_pnl, profit_30d, portfolio_value, traded_markets, computed_unix, roi, fresh)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(proxy_wallet) DO UPDATE SET
		   win_rate=excluded.win_rate,
		   resolved_markets=excluded.resolved_markets,
		   realized_pnl=excluded.realized_pnl,
		   profit_30d=excluded.profit_30d,
		   portfolio_value=excluded.portfolio_value,
		   traded_markets=excluded.traded_markets,
		   computed_unix=excluded.computed_unix,
		   roi=excluded.roi,
		   fresh=excluded.fresh`,
		r.Wallet, r.WinRate, r.ResolvedMarkets, r.RealizedPnL, r.Profit30D,
		r.PortfolioValue, r.TradedMarkets, r.ComputedUnix, r.ROI, b2i(r.Fresh))
	if err != nil {
		return fmt.Errorf("upsert stats %s: %w", r.Wallet, err)
	}
	return nil
}

// GetStats returns the stats row for wallet; found=false if absent.
func (s *Store) GetStats(ctx context.Context, wallet string) (StatsRecord, bool, error) {
	if err := s.ensureStatsSchema(ctx); err != nil {
		return StatsRecord{}, false, err
	}
	r := StatsRecord{Wallet: wallet}
	var freshInt int
	err := s.db.QueryRowContext(ctx,
		`SELECT win_rate, resolved_markets, realized_pnl, profit_30d, portfolio_value, traded_markets, computed_unix, roi, skill_score, fresh
		   FROM wallet_stats WHERE proxy_wallet = ?`, wallet).
		Scan(&r.WinRate, &r.ResolvedMarkets, &r.RealizedPnL, &r.Profit30D,
			&r.PortfolioValue, &r.TradedMarkets, &r.ComputedUnix, &r.ROI, &r.SkillScore, &freshInt)
	if errors.Is(err, sql.ErrNoRows) {
		return StatsRecord{}, false, nil
	}
	r.Fresh = freshInt != 0
	if err != nil {
		return StatsRecord{}, false, fmt.Errorf("get stats %s: %w", wallet, err)
	}
	return r, true, nil
}

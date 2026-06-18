package store

import (
	"context"
	"fmt"
)

// Trade is an observed tracked-wallet trade whose Closing Line Value will be
// computed once its market resolves.
type Trade struct {
	Wallet       string
	TokenID      string
	Tx           string
	LogIndex     uint64
	Entry        float64 // entry price 0..1
	Buy          bool
	ObservedUnix int64
}

// CLVAgg is a wallet's settled-CLV aggregate.
type CLVAgg struct {
	AvgCLV float64
	N      int
}

// ensureTradesSchema creates the clv_trades table (lazy, like the snapshot table).
func (s *Store) ensureTradesSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS clv_trades (
			wallet        TEXT    NOT NULL,
			token_id      TEXT    NOT NULL,
			tx            TEXT    NOT NULL,
			log_index     INTEGER NOT NULL,
			entry         REAL    NOT NULL,
			buy           INTEGER NOT NULL,
			observed_unix INTEGER NOT NULL,
			clv           REAL    NOT NULL DEFAULT 0,
			settled       INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (tx, log_index, wallet)
		);
		CREATE INDEX IF NOT EXISTS idx_clv_unsettled ON clv_trades (settled);
		CREATE INDEX IF NOT EXISTS idx_clv_wallet ON clv_trades (wallet, settled);`)
	if err != nil {
		return fmt.Errorf("create clv_trades schema: %w", err)
	}
	return nil
}

// RecordTrade stores an observed trade (idempotent on tx+logIndex+wallet, since
// the bus is at-least-once).
func (s *Store) RecordTrade(ctx context.Context, t Trade) error {
	if err := s.ensureTradesSchema(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO clv_trades (wallet, token_id, tx, log_index, entry, buy, observed_unix)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(tx, log_index, wallet) DO NOTHING`,
		t.Wallet, t.TokenID, t.Tx, t.LogIndex, t.Entry, b2i(t.Buy), t.ObservedUnix)
	if err != nil {
		return fmt.Errorf("record trade %s/%d: %w", t.Tx, t.LogIndex, err)
	}
	return nil
}

// UnsettledTrades returns trades whose CLV has not yet been computed.
func (s *Store) UnsettledTrades(ctx context.Context) ([]Trade, error) {
	if err := s.ensureTradesSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT wallet, token_id, tx, log_index, entry, buy, observed_unix
		   FROM clv_trades WHERE settled = 0`)
	if err != nil {
		return nil, fmt.Errorf("query unsettled: %w", err)
	}
	defer rows.Close()
	var out []Trade
	for rows.Next() {
		var t Trade
		var buy int
		if err := rows.Scan(&t.Wallet, &t.TokenID, &t.Tx, &t.LogIndex, &t.Entry, &buy, &t.ObservedUnix); err != nil {
			return nil, fmt.Errorf("scan unsettled: %w", err)
		}
		t.Buy = buy != 0
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetTradeCLV records the computed CLV and marks the trade settled.
func (s *Store) SetTradeCLV(ctx context.Context, tx string, logIndex uint64, wallet string, clv float64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE clv_trades SET clv = ?, settled = 1 WHERE tx = ? AND log_index = ? AND wallet = ?`,
		clv, tx, logIndex, wallet)
	if err != nil {
		return fmt.Errorf("set clv %s/%d: %w", tx, logIndex, err)
	}
	return nil
}

// WalletCLV returns the settled-CLV aggregate (mean CLV + sample count) for each
// requested wallet that has at least one settled trade.
func (s *Store) WalletCLV(ctx context.Context, wallets []string) (map[string]CLVAgg, error) {
	if err := s.ensureTradesSchema(ctx); err != nil {
		return nil, err
	}
	out := make(map[string]CLVAgg, len(wallets))
	for _, w := range wallets {
		var avg float64
		var n int
		err := s.db.QueryRowContext(ctx,
			`SELECT COALESCE(AVG(clv), 0), COUNT(*) FROM clv_trades WHERE wallet = ? AND settled = 1`, w).
			Scan(&avg, &n)
		if err != nil {
			return nil, fmt.Errorf("aggregate clv %s: %w", w, err)
		}
		if n > 0 {
			out[w] = CLVAgg{AvgCLV: avg, N: n}
		}
	}
	return out, nil
}

// b2i maps a bool to SQLite's 0/1 integer.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Package cache is a CGO-free SQLite cache of resolved tokenId enrichments.
package cache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // CGO-free driver, registered as "sqlite".

	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/client"
)

// Cache stores tokenId -> enrichment.
type Cache struct {
	db *sql.DB
}

// Open opens (creating if needed) the cache DB and ensures the schema.
func Open(ctx context.Context, path string) (*Cache, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	for _, p := range []string{
		"PRAGMA busy_timeout=5000",
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.ExecContext(ctx, p); err != nil {
			db.Close()
			return nil, fmt.Errorf("set %q: %w", p, err)
		}
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS enrichment (
			token_id     TEXT PRIMARY KEY,
			question     TEXT NOT NULL,
			outcome      TEXT NOT NULL,
			slug         TEXT NOT NULL,
			condition_id TEXT NOT NULL,
			cached_at    INTEGER NOT NULL
		)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Cache{db: db}, nil
}

// Close closes the database.
func (c *Cache) Close() error { return c.db.Close() }

// Get returns the cached enrichment for tokenID; hit=false if absent.
func (c *Cache) Get(ctx context.Context, tokenID string) (client.Enrichment, bool, error) {
	var e client.Enrichment
	err := c.db.QueryRowContext(ctx,
		`SELECT question, outcome, slug, condition_id FROM enrichment WHERE token_id = ?`, tokenID).
		Scan(&e.MarketQuestion, &e.Outcome, &e.MarketSlug, &e.ConditionID)
	if errors.Is(err, sql.ErrNoRows) {
		return client.Enrichment{}, false, nil
	}
	if err != nil {
		return client.Enrichment{}, false, fmt.Errorf("cache get %s: %w", tokenID, err)
	}
	return e, true, nil
}

// Put stores (or replaces) the enrichment for tokenID.
func (c *Cache) Put(ctx context.Context, tokenID string, e client.Enrichment) error {
	_, err := c.db.ExecContext(ctx,
		`INSERT INTO enrichment (token_id, question, outcome, slug, condition_id, cached_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(token_id) DO UPDATE SET
		   question=excluded.question, outcome=excluded.outcome,
		   slug=excluded.slug, condition_id=excluded.condition_id, cached_at=excluded.cached_at`,
		tokenID, e.MarketQuestion, e.Outcome, e.MarketSlug, e.ConditionID, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("cache put %s: %w", tokenID, err)
	}
	return nil
}

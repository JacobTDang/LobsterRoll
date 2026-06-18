// Package cache is a CGO-free SQLite cache of resolved tokenId enrichments.
package cache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/sqlitex"
	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/client"
)

// Cache stores tokenId -> enrichment. Entries older than ttl are treated as
// misses so market metadata (slug, end date) can't go stale forever; ttl<=0
// disables expiry (entries are then immutable once cached).
type Cache struct {
	db  *sql.DB
	ttl time.Duration
	now func() time.Time
}

// Open opens (creating if needed) the cache DB and ensures the schema. Cached
// rows older than ttl are served as misses (re-fetched); pass ttl<=0 to never
// expire.
func Open(ctx context.Context, path string, ttl time.Duration) (*Cache, error) {
	db, err := sqlitex.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS enrichment (
			token_id      TEXT PRIMARY KEY,
			question      TEXT NOT NULL,
			outcome       TEXT NOT NULL,
			slug          TEXT NOT NULL,
			condition_id  TEXT NOT NULL,
			end_date_unix INTEGER NOT NULL DEFAULT 0,
			cached_at     INTEGER NOT NULL
		)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	// Migrate older caches that predate end_date_unix; ignore "duplicate column".
	if _, err := db.ExecContext(ctx,
		`ALTER TABLE enrichment ADD COLUMN end_date_unix INTEGER NOT NULL DEFAULT 0`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		db.Close()
		return nil, fmt.Errorf("migrate end_date_unix: %w", err)
	}
	return &Cache{db: db, ttl: ttl, now: time.Now}, nil
}

// Close closes the database.
func (c *Cache) Close() error { return c.db.Close() }

// Get returns the cached enrichment for tokenID; hit=false if absent or expired
// (older than ttl), so the caller re-fetches and overwrites the stale row.
func (c *Cache) Get(ctx context.Context, tokenID string) (client.Enrichment, bool, error) {
	var e client.Enrichment
	var cachedAt int64
	err := c.db.QueryRowContext(ctx,
		`SELECT question, outcome, slug, condition_id, end_date_unix, cached_at FROM enrichment WHERE token_id = ?`, tokenID).
		Scan(&e.MarketQuestion, &e.Outcome, &e.MarketSlug, &e.ConditionID, &e.EndDateUnix, &cachedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return client.Enrichment{}, false, nil
	}
	if err != nil {
		return client.Enrichment{}, false, fmt.Errorf("cache get %s: %w", tokenID, err)
	}
	if c.ttl > 0 && c.now().Unix()-cachedAt > int64(c.ttl.Seconds()) {
		return client.Enrichment{}, false, nil // expired -> treat as miss
	}
	return e, true, nil
}

// Put stores (or replaces) the enrichment for tokenID.
func (c *Cache) Put(ctx context.Context, tokenID string, e client.Enrichment) error {
	_, err := c.db.ExecContext(ctx,
		`INSERT INTO enrichment (token_id, question, outcome, slug, condition_id, end_date_unix, cached_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(token_id) DO UPDATE SET
		   question=excluded.question, outcome=excluded.outcome,
		   slug=excluded.slug, condition_id=excluded.condition_id,
		   end_date_unix=excluded.end_date_unix, cached_at=excluded.cached_at`,
		tokenID, e.MarketQuestion, e.Outcome, e.MarketSlug, e.ConditionID, e.EndDateUnix, c.now().Unix())
	if err != nil {
		return fmt.Errorf("cache put %s: %w", tokenID, err)
	}
	return nil
}

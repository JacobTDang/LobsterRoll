// Package caps enforces the trader's independent hard caps — the last safety net
// before real money moves, applied regardless of what strategy proposed.
//
// Reserve atomically checks AND commits a placement against all caps, so under
// concurrency the committed total can never exceed a cap. If placement then
// fails, Release rolls the reservation back. The daily-spend and open-exposure
// ledger is persisted (via Ledger) and reloaded on startup, so a restart cannot
// reset the cumulative caps to zero.
package caps

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Ledger persists the cumulative cap ledger so it survives restarts.
type Ledger interface {
	LoadCaps(ctx context.Context) (dayKey string, daySpent, openExposure float64, ok bool, err error)
	SaveCaps(ctx context.Context, dayKey string, daySpent, openExposure float64) error
}

// Caps holds the configured limits and the running ledger.
type Caps struct {
	maxPerTrade     float64
	maxPerDay       float64
	maxOpenExposure float64

	mu           sync.Mutex
	daySpent     float64
	dayKey       string // UTC date of the current day window
	openExposure float64 // signed net long exposure (buys add, sells subtract)
	ledger       Ledger
	log          *slog.Logger
	now          func() time.Time
}

// Decision is the result of a Reserve.
type Decision struct {
	Allowed bool
	Reason  string
}

// New constructs Caps. ledger (optional) persists/reloads the cumulative ledger.
func New(maxPerTrade, maxPerDay, maxOpenExposure float64, ledger Ledger, log *slog.Logger) *Caps {
	c := &Caps{
		maxPerTrade: maxPerTrade, maxPerDay: maxPerDay, maxOpenExposure: maxOpenExposure,
		ledger: ledger, log: log, now: time.Now,
	}
	if ledger != nil {
		if dk, ds, oe, ok, err := ledger.LoadCaps(context.Background()); err != nil {
			if log != nil {
				log.Error("load caps ledger failed; starting from zero", "err", err)
			}
		} else if ok {
			c.dayKey, c.daySpent, c.openExposure = dk, ds, oe
		}
	}
	return c
}

// rollDay resets the daily spend only when crossing forward to a later UTC day.
// Using a lexical > guard means a backward clock step never re-opens the budget.
func (c *Caps) rollDay() {
	key := c.now().UTC().Format("2006-01-02")
	if key > c.dayKey {
		c.dayKey = key
		c.daySpent = 0
	}
}

// Reserve atomically authorizes and commits a sizeUSD placement. buy adds to net
// exposure, a sell subtracts. Returns Allowed=false (nothing committed) if any
// cap would be exceeded.
func (c *Caps) Reserve(sizeUSD float64, buy bool) Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollDay()

	if sizeUSD > c.maxPerTrade {
		return Decision{false, fmt.Sprintf("per-trade cap: $%.2f > $%.2f", sizeUSD, c.maxPerTrade)}
	}
	if c.daySpent+sizeUSD > c.maxPerDay {
		return Decision{false, fmt.Sprintf("per-day cap: $%.2f + $%.2f > $%.2f", c.daySpent, sizeUSD, c.maxPerDay)}
	}
	if buy && c.openExposure+sizeUSD > c.maxOpenExposure {
		return Decision{false, fmt.Sprintf("open-exposure cap: $%.2f + $%.2f > $%.2f", c.openExposure, sizeUSD, c.maxOpenExposure)}
	}

	c.daySpent += sizeUSD
	c.openExposure += signed(sizeUSD, buy)
	c.persist()
	return Decision{Allowed: true}
}

// Release rolls back a reservation exactly (symmetric with Reserve) when the
// placement failed.
func (c *Caps) Release(sizeUSD float64, buy bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.daySpent -= sizeUSD
	if c.daySpent < 0 {
		c.daySpent = 0
	}
	c.openExposure -= signed(sizeUSD, buy)
	c.persist()
}

func signed(sizeUSD float64, buy bool) float64 {
	if buy {
		return sizeUSD
	}
	return -sizeUSD
}

func (c *Caps) persist() {
	if c.ledger == nil {
		return
	}
	if err := c.ledger.SaveCaps(context.Background(), c.dayKey, c.daySpent, c.openExposure); err != nil && c.log != nil {
		c.log.Error("persist caps ledger failed", "err", err)
	}
}

// Snapshot returns the current daily spend and net open exposure.
func (c *Caps) Snapshot() (daySpent, openExposure float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollDay()
	return c.daySpent, c.openExposure
}

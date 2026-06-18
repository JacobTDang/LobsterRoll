// Package caps enforces the trader's independent hard caps — the last safety net
// before real money moves, applied regardless of what strategy proposed.
//
// Reserve atomically checks AND commits a placement against all caps, so under
// concurrency the committed total can never exceed a cap. If placement then
// fails, Release rolls the reservation back.
package caps

import (
	"fmt"
	"sync"
	"time"
)

// Caps holds the configured limits and the running ledger.
type Caps struct {
	maxPerTrade     float64
	maxPerDay       float64
	maxOpenExposure float64

	mu           sync.Mutex
	daySpent     float64
	dayKey       string // UTC date of the current day window
	openExposure float64
	now          func() time.Time
}

// Decision is the result of a Reserve.
type Decision struct {
	Allowed bool
	Reason  string
}

// New constructs Caps with the given USD limits.
func New(maxPerTrade, maxPerDay, maxOpenExposure float64) *Caps {
	return &Caps{
		maxPerTrade: maxPerTrade, maxPerDay: maxPerDay, maxOpenExposure: maxOpenExposure,
		now: time.Now,
	}
}

func (c *Caps) rollDay() {
	key := c.now().UTC().Format("2006-01-02")
	if key != c.dayKey {
		c.dayKey = key
		c.daySpent = 0
	}
}

// Reserve atomically authorizes and commits a sizeUSD placement. buy increases
// open exposure; a sell decreases it (floored at zero). Returns Allowed=false
// with a reason if any cap would be exceeded (nothing is committed then).
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
	if buy {
		c.openExposure += sizeUSD
	} else {
		c.openExposure -= sizeUSD
		if c.openExposure < 0 {
			c.openExposure = 0
		}
	}
	return Decision{Allowed: true}
}

// Release rolls back a reservation when the placement failed.
func (c *Caps) Release(sizeUSD float64, buy bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.daySpent -= sizeUSD
	if c.daySpent < 0 {
		c.daySpent = 0
	}
	if buy {
		c.openExposure -= sizeUSD
		if c.openExposure < 0 {
			c.openExposure = 0
		}
	} else {
		c.openExposure += sizeUSD
	}
}

// Snapshot returns the current daily spend and open exposure (for logging/tests).
func (c *Caps) Snapshot() (daySpent, openExposure float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollDay()
	return c.daySpent, c.openExposure
}

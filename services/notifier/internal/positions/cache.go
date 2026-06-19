package positions

import (
	"strings"
	"sync"
)

// MatchKind is how a whale's traded token relates to a held position.
type MatchKind int

const (
	None     MatchKind = iota
	Exact              // the whale traded the exact outcome the user holds
	Opposite           // the whale traded the opposite outcome of a market the user holds
)

// Holding is a user-held position (the fields an alert needs).
type Holding struct {
	TokenID      string
	ConditionID  string
	Outcome      string
	Title        string
	Slug         string
	Size         float64
	CurrentValue float64
}

type snapshot struct {
	byToken    map[string]Holding
	byOpposite map[string]Holding
	ok         bool // false until the first successful load
}

// Cache holds an atomically-swappable snapshot of the user's positions, indexed
// for O(1) lookup by both the held token and its opposite.
type Cache struct {
	mu   sync.RWMutex
	snap snapshot
	self string // lowercased user wallet (to ignore the user's own trades)
}

// NewCache returns a Cache for the given user wallet.
func NewCache(selfWallet string) *Cache {
	return &Cache{self: strings.ToLower(strings.TrimSpace(selfWallet))}
}

// Replace rebuilds the snapshot from a fresh positions list. Dust (size<=0) and
// resolved-and-claimable positions (redeemable with no live price) are dropped —
// nothing to act on there.
func (c *Cache) Replace(ps []Position) {
	byToken := make(map[string]Holding, len(ps))
	byOpposite := make(map[string]Holding, len(ps))
	for _, p := range ps {
		if p.Size <= 0 || (p.Redeemable && p.CurPrice == 0) {
			continue
		}
		h := Holding{
			TokenID: p.Asset, ConditionID: p.ConditionID, Outcome: p.Outcome,
			Title: p.Title, Slug: p.Slug, Size: p.Size, CurrentValue: p.CurrentValue,
		}
		byToken[p.Asset] = h
		if p.OppositeAsset != "" {
			byOpposite[p.OppositeAsset] = h
		}
	}
	c.mu.Lock()
	c.snap = snapshot{byToken: byToken, byOpposite: byOpposite, ok: true}
	c.mu.Unlock()
}

// Match decides whether a whale's trade (token + side + wallet) touches a held
// position worth a priority alert. Fires on the high-signal cases only (phase 1):
// the whale EXITS the exact outcome you hold (sell same token), or BUYS the
// opposite outcome (betting against you). The user's own trades never match.
func (c *Cache) Match(tokenID, side, wallet string) (Holding, MatchKind, bool) {
	if strings.EqualFold(wallet, c.self) {
		return Holding{}, None, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.snap.ok {
		return Holding{}, None, false
	}
	sell := strings.EqualFold(side, "sell")
	if h, ok := c.snap.byToken[tokenID]; ok {
		return h, Exact, sell // whale exiting your outcome
	}
	if h, ok := c.snap.byOpposite[tokenID]; ok {
		return h, Opposite, !sell // whale buying the other side = betting against you
	}
	return Holding{}, None, false
}

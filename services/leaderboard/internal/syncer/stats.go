package syncer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/client"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/dataapi"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/selection"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/skill"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/stats"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/store"
)

// CandidateFetcher pulls a leaderboard page (wallet + metric amount) for a
// metric/window. *client.Client satisfies it via FetchEntries.
type CandidateFetcher interface {
	FetchEntries(ctx context.Context, metric client.Metric, window client.Window, topN int) ([]client.Entry, error)
}

// WalletCrawler fetches per-wallet history and portfolio figures from data-api.
// *dataapi.Client satisfies it.
type WalletCrawler interface {
	Activity(ctx context.Context, wallet string, maxRows int) ([]dataapi.Activity, error)
	Value(ctx context.Context, wallet string) (float64, error)
}

// Broadcaster publishes a watchset change to gRPC stream subscribers.
// *server.Server satisfies it.
type Broadcaster interface {
	Broadcast(added, removed []string)
}

// StatsStorer persists computed stats and atomically replaces the watchset.
// *store.Store satisfies it.
type StatsStorer interface {
	UpsertStats(ctx context.Context, r store.StatsRecord) error
	SetSkillScore(ctx context.Context, wallet string, score int64) error
	Replace(ctx context.Context, wallets []string) (store.Delta, error)
	SetLastSync(ctx context.Context, unix int64) error
}

// StatsConfig bounds the stats crawl and tunes selection.
type StatsConfig struct {
	Metric          client.Metric // metric used to size candidate amounts (e.g. pnl)
	CandidateTopK   int           // top-K per window pulled into the candidate pool
	MaxCandidates   int           // cap on candidates crawled per refresh
	MaxActivity     int           // cap on activity rows fetched per wallet
	MinResolved     int           // selection gate: min resolved markets
	MinWinRate      float64       // selection gate: min win rate (0..1)
	MinPortfolioUSD float64       // selection gate: min portfolio value
	MinRealizedPnL  float64       // selection gate: min realized PnL
	ShrinkK         float64       // skill shrinkage prior strength (equiv. resolved markets)
	TopN            int           // selection: max watchset size
	Interval        time.Duration // how often to rebuild
	Concurrency     int           // max concurrent wallet crawls (<1 = serial)
}

// errAllWindowsFailed is returned when every candidate-window fetch failed, so
// the pool is empty for transient reasons (not because the leaderboard is
// genuinely empty). Treated as a failed refresh that must not wipe the watchset.
var errAllWindowsFailed = errors.New("all candidate windows failed")

// candidateWindows are the leaderboard windows unioned into the candidate pool.
// 7d/30d/all favors consistent performers over single-day spikes.
var candidateWindows = []client.Window{"7d", "30d", "all"}

// StatsSyncer periodically builds a candidate pool from the leaderboard, crawls
// each candidate's data-api history into consistency stats, persists them, and
// replaces the watchset with the top-N most consistent wallets.
type StatsSyncer struct {
	cand    CandidateFetcher
	crawler WalletCrawler
	store   StatsStorer
	bc      Broadcaster
	cfg     StatsConfig
	log     *slog.Logger
}

// NewStats constructs a StatsSyncer.
func NewStats(cand CandidateFetcher, crawler WalletCrawler, st StatsStorer, bc Broadcaster, cfg StatsConfig, log *slog.Logger) *StatsSyncer {
	return &StatsSyncer{cand: cand, crawler: crawler, store: st, bc: bc, cfg: cfg, log: log}
}

// Run performs an immediate refresh, then refreshes every cfg.Interval until
// ctx is cancelled. Transient errors are logged, not fatal.
func (s *StatsSyncer) Run(ctx context.Context) error {
	if err := s.refresh(ctx); err != nil {
		s.log.Warn("initial stats refresh failed", "err", err)
	}
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.refresh(ctx); err != nil {
				s.log.Warn("stats refresh failed", "err", err)
			}
		}
	}
}

// candidatePool builds the deduped union of the top-K wallets across windows.
// The candidate's Profit30D is taken from the 30d window when available. A
// candidate appearing in no window leaves Profit30D zero. The whole pool fetch
// failing (every window errored) returns an error so we never crawl an empty
// pool and wipe the watchset.
func (s *StatsSyncer) candidatePool(ctx context.Context) ([]selection.Candidate, error) {
	order := make([]string, 0)
	profit30 := make(map[string]float64)
	seen := make(map[string]struct{})
	var anyOK bool

	for _, w := range candidateWindows {
		entries, err := s.cand.FetchEntries(ctx, s.cfg.Metric, w, s.cfg.CandidateTopK)
		if err != nil {
			s.log.Warn("candidate window fetch failed", "window", w, "err", err)
			continue
		}
		anyOK = true
		for _, e := range entries {
			if w == "30d" {
				profit30[e.Wallet] = e.Amount
			}
			if _, dup := seen[e.Wallet]; dup {
				continue
			}
			seen[e.Wallet] = struct{}{}
			order = append(order, e.Wallet)
		}
	}
	if !anyOK {
		return nil, errAllWindowsFailed
	}

	cands := make([]selection.Candidate, 0, len(order))
	for _, w := range order {
		cands = append(cands, selection.Candidate{Wallet: w, Profit30D: profit30[w]})
	}
	if len(cands) > s.cfg.MaxCandidates {
		cands = cands[:s.cfg.MaxCandidates]
	}
	return cands, nil
}

// refresh runs the full pipeline once.
func (s *StatsSyncer) refresh(ctx context.Context) error {
	cands, err := s.candidatePool(ctx)
	if err != nil {
		return err
	}

	now := time.Now().Unix()
	var mu sync.Mutex
	statsByWallet := make(map[string]selection.Stats, len(cands))

	// Crawl candidates concurrently with a bounded worker pool — the data-api
	// round-trips dominate, so this is where the wall-clock is. The limit keeps
	// us from opening dozens of sockets at once (data-api would 429). A transient
	// per-wallet crawl failure is logged and skipped; only a store error aborts.
	conc := s.cfg.Concurrency
	if conc < 1 {
		conc = 1
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(conc)
	for _, c := range cands {
		c := c
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}
			acts, err := s.crawler.Activity(gctx, c.Wallet, s.cfg.MaxActivity)
			if err != nil {
				s.log.Warn("activity crawl failed; skipping candidate", "wallet", c.Wallet, "err", err)
				return nil
			}
			st := stats.Compute(acts)

			// Portfolio value is best-effort: a failure here must not drop the
			// candidate, since win-rate/PnL are the selection signal.
			value, err := s.crawler.Value(gctx, c.Wallet)
			if err != nil {
				s.log.Warn("portfolio value fetch failed; using 0", "wallet", c.Wallet, "err", err)
				value = 0
			}

			if err := s.store.UpsertStats(gctx, store.StatsRecord{
				Wallet:          c.Wallet,
				WinRate:         st.WinRate,
				ResolvedMarkets: int64(st.ResolvedMarkets),
				RealizedPnL:     st.RealizedPnL,
				ROI:             st.ROI,
				Profit30D:       c.Profit30D,
				PortfolioValue:  value,
				TradedMarkets:   int64(st.TradedMarkets),
				ComputedUnix:    now,
			}); err != nil {
				return err
			}
			mu.Lock()
			statsByWallet[c.Wallet] = selection.Stats{
				WinRate:         st.WinRate,
				ResolvedMarkets: st.ResolvedMarkets,
				RealizedPnL:     st.RealizedPnL,
				PortfolioUSD:    value,
				ROI:             st.ROI,
			}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// Population-wide skill: shrink each wallet's ROI toward the population mean
	// (small samples pulled hardest), then rank selection by the shrunk ROI and
	// persist the 0–100 skill score for the alert.
	pop := make([]skill.Input, 0, len(statsByWallet))
	for w, st := range statsByWallet {
		pop = append(pop, skill.Input{Wallet: w, ROI: st.ROI, N: st.ResolvedMarkets})
	}
	for _, r := range skill.Shrink(pop, s.cfg.ShrinkK) {
		st := statsByWallet[r.Wallet]
		st.ShrunkROI = r.ShrunkROI
		statsByWallet[r.Wallet] = st
		if err := s.store.SetSkillScore(ctx, r.Wallet, int64(r.Score)); err != nil {
			return err
		}
	}

	watchset := selection.Select(cands, statsByWallet, selection.Criteria{
		MinResolved:     s.cfg.MinResolved,
		MinWinRate:      s.cfg.MinWinRate,
		MinPortfolioUSD: s.cfg.MinPortfolioUSD,
		MinRealizedPnL:  s.cfg.MinRealizedPnL,
	}, s.cfg.TopN)
	// Empty-replace guard (defense-in-depth): never wipe the watchset from an
	// empty selection (e.g. all crawls failed, or none cleared min-resolved).
	// Skip replace/broadcast and do not advance last-sync so staleness surfaces.
	if len(watchset) == 0 {
		s.log.Warn("stats selection empty; skipping replace to avoid wiping watchset",
			"candidates", len(cands), "withStats", len(statsByWallet))
		return nil
	}

	d, err := s.store.Replace(ctx, watchset)
	if err != nil {
		return err
	}
	if !d.Empty() {
		s.bc.Broadcast(d.Added, d.Removed)
		s.log.Info("watchset changed", "added", len(d.Added), "removed", len(d.Removed), "size", len(watchset))
	}
	if err := s.store.SetLastSync(ctx, time.Now().Unix()); err != nil {
		return err
	}
	return nil
}

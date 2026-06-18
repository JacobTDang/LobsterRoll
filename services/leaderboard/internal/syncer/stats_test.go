package syncer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/client"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/dataapi"
)

// fakeCandidates serves a per-window entry list.
type fakeCandidates struct {
	byWindow map[client.Window][]client.Entry
	err      error
}

func (f *fakeCandidates) FetchEntries(_ context.Context, _ client.Metric, w client.Window, _ int) ([]client.Entry, error) {
	if f.err != nil {
		return nil, f.err
	}
	e, ok := f.byWindow[w]
	if !ok {
		return nil, errors.New("no data for window")
	}
	return e, nil
}

// fakeCrawler serves activity/value per wallet.
type fakeCrawler struct {
	mu       sync.Mutex
	activity map[string][]dataapi.Activity
	value    map[string]float64
	actErr   map[string]error
	calls    int
}

func (f *fakeCrawler) Activity(_ context.Context, wallet string, _ int) ([]dataapi.Activity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if e := f.actErr[wallet]; e != nil {
		return nil, e
	}
	return f.activity[wallet], nil
}

func (f *fakeCrawler) Value(_ context.Context, wallet string) (float64, error) {
	return f.value[wallet], nil
}

func act(typ, side string, size float64, cond string) dataapi.Activity {
	return dataapi.Activity{Type: typ, Side: side, USDCSize: size, ConditionID: cond}
}

// makeResolved builds n resolved winning markets for a wallet so it clears a
// min-resolved filter. Each market: buy 1, redeem 2 -> +1 net (win).
func makeResolved(n int) []dataapi.Activity {
	var acts []dataapi.Activity
	for i := 0; i < n; i++ {
		c := "m" + itoa(i)
		acts = append(acts, act("TRADE", "BUY", 1, c), act("REDEEM", "", 2, c))
	}
	return acts
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestStatsRefresh_BuildsWatchsetFromSelection(t *testing.T) {
	ctx := context.Background()
	cand := &fakeCandidates{byWindow: map[client.Window][]client.Entry{
		"7d":  {{Wallet: "0xpro", Amount: 100}, {Wallet: "0xnoob", Amount: 50}},
		"30d": {{Wallet: "0xpro", Amount: 1000}},
		"all": {{Wallet: "0xpro", Amount: 5000}},
	}}
	crawl := &fakeCrawler{
		activity: map[string][]dataapi.Activity{
			"0xpro":  makeResolved(25), // clears minResolved=20
			"0xnoob": makeResolved(2),  // below minResolved -> excluded
		},
		value: map[string]float64{"0xpro": 12345},
	}
	st := newStore(t)
	bc := &recordingBroadcaster{}
	cfg := StatsConfig{
		Metric: client.MetricPNL, CandidateTopK: 50, MaxCandidates: 60,
		MaxActivity: 3000, MinResolved: 20, TopN: 25, Interval: time.Hour,
	}
	s := NewStats(cand, crawl, st, bc, cfg, quietLogger())

	if err := s.refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	got, _ := st.List(ctx)
	if len(got) != 1 || got[0] != "0xpro" {
		t.Fatalf("watchset = %v, want [0xpro] (noob excluded by minResolved)", got)
	}
	// Stats persisted for the crawled pro.
	rec, found, err := st.GetStats(ctx, "0xpro")
	if err != nil || !found {
		t.Fatalf("GetStats(0xpro) = (found=%v, err=%v)", found, err)
	}
	if rec.ResolvedMarkets != 25 || rec.WinRate != 1.0 {
		t.Errorf("stats = %+v, want 25 resolved at win-rate 1.0", rec)
	}
	if rec.PortfolioValue != 12345 {
		t.Errorf("PortfolioValue = %v, want 12345", rec.PortfolioValue)
	}
	if rec.Profit30D != 1000 {
		t.Errorf("Profit30D = %v, want 1000 (from 30d window)", rec.Profit30D)
	}
	// Skill score was computed population-wide and persisted (a valid percentile).
	if rec.SkillScore < 0 || rec.SkillScore > 100 {
		t.Errorf("SkillScore = %d, want a 0-100 percentile", rec.SkillScore)
	}
	if bc.count() != 1 {
		t.Errorf("broadcasts = %d, want 1", bc.count())
	}
}

func TestStatsRefresh_EmptySelectionDoesNotWipe(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	if _, err := st.Replace(ctx, []string{"0xexisting"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cand := &fakeCandidates{byWindow: map[client.Window][]client.Entry{
		"7d": {{Wallet: "0xnoob", Amount: 1}},
	}}
	crawl := &fakeCrawler{activity: map[string][]dataapi.Activity{"0xnoob": makeResolved(2)}}
	cfg := StatsConfig{Metric: client.MetricPNL, CandidateTopK: 50, MaxCandidates: 60, MaxActivity: 3000, MinResolved: 20, TopN: 25, Interval: time.Hour}
	s := NewStats(cand, crawl, st, &recordingBroadcaster{}, cfg, quietLogger())

	if err := s.refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	got, _ := st.List(ctx)
	if len(got) != 1 || got[0] != "0xexisting" {
		t.Fatalf("watchset = %v, want [0xexisting] (empty selection must not wipe)", got)
	}
	// last-sync must not advance on an empty/unhealthy refresh.
	if ls, _ := st.LastSync(ctx); ls != 0 {
		t.Errorf("LastSync = %d, want 0", ls)
	}
}

func TestStatsRefresh_AllWindowsFailErrors(t *testing.T) {
	cand := &fakeCandidates{err: errors.New("boom")}
	cfg := StatsConfig{Metric: client.MetricPNL, CandidateTopK: 50, MaxCandidates: 60, MaxActivity: 3000, MinResolved: 20, TopN: 25, Interval: time.Hour}
	s := NewStats(cand, &fakeCrawler{}, newStore(t), &recordingBroadcaster{}, cfg, quietLogger())
	if err := s.refresh(context.Background()); err == nil {
		t.Fatal("expected error when all candidate windows fail")
	}
}

func TestStatsRefresh_SkipsCandidateOnCrawlError(t *testing.T) {
	ctx := context.Background()
	cand := &fakeCandidates{byWindow: map[client.Window][]client.Entry{
		"7d": {{Wallet: "0xgood", Amount: 100}, {Wallet: "0xbad", Amount: 90}},
	}}
	crawl := &fakeCrawler{
		activity: map[string][]dataapi.Activity{"0xgood": makeResolved(25)},
		actErr:   map[string]error{"0xbad": errors.New("crawl boom")},
	}
	cfg := StatsConfig{Metric: client.MetricPNL, CandidateTopK: 50, MaxCandidates: 60, MaxActivity: 3000, MinResolved: 20, TopN: 25, Interval: time.Hour}
	s := NewStats(cand, crawl, newStore(t), &recordingBroadcaster{}, cfg, quietLogger())
	if err := s.refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	// 0xbad crawl failure must not abort the whole refresh; 0xgood still selected.
	got, _ := s.store.(interface {
		List(context.Context) ([]string, error)
	}).List(ctx)
	if len(got) != 1 || got[0] != "0xgood" {
		t.Fatalf("watchset = %v, want [0xgood]", got)
	}
}

// concCrawler proves the crawl runs concurrently: each Activity call records the
// peak in-flight count, and the first `limit` callers block on a gate that only
// opens once `limit` of them have arrived simultaneously (with a timeout so a
// serial regression fails the assertion instead of hanging forever).
type concCrawler struct {
	limit       int
	gate        chan struct{}
	mu          sync.Mutex
	inflight    int
	maxInflight int
	arrived     int
}

func (c *concCrawler) Activity(_ context.Context, _ string, _ int) ([]dataapi.Activity, error) {
	c.mu.Lock()
	c.inflight++
	if c.inflight > c.maxInflight {
		c.maxInflight = c.inflight
	}
	c.arrived++
	if c.arrived == c.limit {
		close(c.gate) // the limit-th concurrent caller releases the wave
	}
	c.mu.Unlock()

	select {
	case <-c.gate:
	case <-time.After(3 * time.Second): // serial regression -> times out, maxInflight stays low
	}

	c.mu.Lock()
	c.inflight--
	c.mu.Unlock()
	return makeResolved(25), nil
}

func (c *concCrawler) Value(_ context.Context, _ string) (float64, error) { return 0, nil }

func TestStatsRefresh_CrawlsConcurrently(t *testing.T) {
	const limit = 4
	entries := make([]client.Entry, 0, 8)
	for i := 0; i < 8; i++ {
		entries = append(entries, client.Entry{Wallet: "0xw" + itoa(i), Amount: float64(8 - i)})
	}
	cand := &fakeCandidates{byWindow: map[client.Window][]client.Entry{"7d": entries}}
	crawl := &concCrawler{limit: limit, gate: make(chan struct{})}
	cfg := StatsConfig{
		Metric: client.MetricPNL, CandidateTopK: 50, MaxCandidates: 60, MaxActivity: 3000,
		MinResolved: 20, TopN: 25, Interval: time.Hour, Concurrency: limit,
	}
	s := NewStats(cand, crawl, newStore(t), &recordingBroadcaster{}, cfg, quietLogger())
	if err := s.refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	crawl.mu.Lock()
	peak := crawl.maxInflight
	crawl.mu.Unlock()
	if peak != limit {
		t.Fatalf("peak concurrent crawls = %d, want %d (crawl is not running concurrently)", peak, limit)
	}
}

func TestStatsRefresh_RespectsMaxCandidates(t *testing.T) {
	ctx := context.Background()
	cand := &fakeCandidates{byWindow: map[client.Window][]client.Entry{
		"7d": {
			{Wallet: "0xa", Amount: 3}, {Wallet: "0xb", Amount: 2}, {Wallet: "0xc", Amount: 1},
		},
	}}
	crawl := &fakeCrawler{activity: map[string][]dataapi.Activity{
		"0xa": makeResolved(25), "0xb": makeResolved(25), "0xc": makeResolved(25),
	}}
	cfg := StatsConfig{Metric: client.MetricPNL, CandidateTopK: 50, MaxCandidates: 2, MaxActivity: 3000, MinResolved: 20, TopN: 25, Interval: time.Hour}
	s := NewStats(cand, crawl, newStore(t), &recordingBroadcaster{}, cfg, quietLogger())
	if err := s.refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	// Only the first 2 candidates should have been crawled.
	crawl.mu.Lock()
	calls := crawl.calls
	crawl.mu.Unlock()
	if calls != 2 {
		t.Fatalf("activity crawls = %d, want 2 (MaxCandidates cap)", calls)
	}
}

func TestStatsRefresh_CtxCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	cand := &fakeCandidates{byWindow: map[client.Window][]client.Entry{
		"7d": {{Wallet: "0xa", Amount: 1}},
	}}
	crawl := &fakeCrawler{activity: map[string][]dataapi.Activity{"0xa": makeResolved(25)}}
	cfg := StatsConfig{Metric: client.MetricPNL, CandidateTopK: 50, MaxCandidates: 60, MaxActivity: 3000, MinResolved: 20, TopN: 25, Interval: time.Hour}
	s := NewStats(cand, crawl, newStore(t), &recordingBroadcaster{}, cfg, quietLogger())
	// candidatePool fetch uses fake (ignores ctx) but the per-candidate loop
	// checks ctx.Err() and should bail.
	err := s.refresh(ctx)
	if err == nil {
		t.Fatal("expected ctx cancellation error")
	}
}

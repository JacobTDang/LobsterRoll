package chainwatch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/dedup"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/watchset"
)

const aggressor = "0xa6d24a207011c9a5d54fa3a04f3e87365d2e12f4"

func goldenLogs(t *testing.T) []types.Log {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "orderfilled_logs.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var m map[string]struct {
		Address, BlockNumber, TxHash, LogIndex, Data string
		Topics                                       []string
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var out []types.Log
	for _, name := range []string{"buy_side0", "sell_side1"} {
		r := m[name]
		topics := make([]common.Hash, len(r.Topics))
		for i, tpc := range r.Topics {
			topics[i] = common.HexToHash(tpc)
		}
		data, _ := hexutil.Decode(r.Data)
		blk, _ := hexutil.DecodeUint64(r.BlockNumber)
		idx, _ := hexutil.DecodeUint64(r.LogIndex)
		out = append(out, types.Log{
			Address: common.HexToAddress(r.Address), Topics: topics, Data: data,
			TxHash: common.HexToHash(r.TxHash), Index: uint(idx), BlockNumber: blk,
		})
	}
	return out
}

type fakePub struct {
	mu     sync.Mutex
	trades []bus.TradeDetected
	err    error
}

func (p *fakePub) PublishTrade(t bus.TradeDetected) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.trades = append(p.trades, t)
	return nil
}
func (p *fakePub) count() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.trades) }
func (p *fakePub) first() bus.TradeDetected {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.trades[0]
}

type fakeCursor struct {
	mu    sync.Mutex
	block uint64
	set   bool
}

func (c *fakeCursor) LastProcessedBlock(context.Context) (uint64, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.block, c.set, nil
}
func (c *fakeCursor) SetLastProcessedBlock(_ context.Context, b uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.block, c.set = b, true
	return nil
}
func (c *fakeCursor) last() uint64 { c.mu.Lock(); defer c.mu.Unlock(); return c.block }

type fakeSub struct{ errc chan error }

func (s fakeSub) Err() <-chan error { return s.errc }
func (s fakeSub) Unsubscribe()      {}

type fakeChain struct {
	head        uint64
	rangeLogs   func(from, to uint64) []types.Log
	filterCalls int
	subLogs     []types.Log
	subErrc     chan error
}

func (f *fakeChain) BlockNumber(context.Context) (uint64, error) { return f.head, nil }
func (f *fakeChain) FilterLogs(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	f.filterCalls++
	if f.rangeLogs == nil {
		return nil, nil
	}
	return f.rangeLogs(q.FromBlock.Uint64(), q.ToBlock.Uint64()), nil
}
func (f *fakeChain) SubscribeFilterLogs(_ context.Context, _ ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error) {
	go func() {
		for _, l := range f.subLogs {
			ch <- l
		}
	}()
	return fakeSub{errc: f.subErrc}, nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newWatcher(t *testing.T, fc *fakeChain) (*Watcher, *fakePub, *fakeCursor) {
	t.Helper()
	set := watchset.New()
	set.Apply([]string{aggressor}, nil)
	pub := &fakePub{}
	cur := &fakeCursor{}
	w := New(fc, set, dedup.New(), cur, pub, quiet())
	w.chunk = 1                             // force multiple chunks in backfill tests
	w.flushInterval = 20 * time.Millisecond // fast settle for live tests
	return w, pub, cur
}

// liveLoop creates the subscription the way cycle() does, then runs the live loop
// — letting the live-path tests exercise runLive with the fake chain's stream.
func liveLoop(ctx context.Context, w *Watcher, fc *fakeChain) error {
	ch := make(chan types.Log, 1024)
	sub, err := fc.SubscribeFilterLogs(ctx, w.baseQuery(), ch)
	if err != nil {
		return err
	}
	return w.runLive(ctx, ch, sub)
}

func TestBackfill_PublishesAndAdvances(t *testing.T) {
	logs := goldenLogs(t)
	block := logs[0].BlockNumber
	fc := &fakeChain{
		head: block,
		rangeLogs: func(from, to uint64) []types.Log {
			if from <= block && block <= to {
				return logs
			}
			return nil
		},
	}
	w, pub, cur := newWatcher(t, fc)

	if err := w.backfill(context.Background(), block, block, true); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if pub.count() != 1 {
		t.Fatalf("published %d trades, want 1 (aggregated)", pub.count())
	}
	tr := pub.first()
	if tr.Side != "sell" || tr.Size != "11.52" || tr.Price != "0.95" {
		t.Errorf("trade = %+v, want sell/11.52/0.95", tr)
	}
	if cur.last() != block {
		t.Errorf("cursor = %d, want %d", cur.last(), block)
	}
}

func TestBackfill_Chunks(t *testing.T) {
	fc := &fakeChain{head: 10, rangeLogs: func(from, to uint64) []types.Log { return nil }}
	w, _, cur := newWatcher(t, fc) // chunk=1
	if err := w.backfill(context.Background(), 1, 5, true); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if fc.filterCalls != 5 {
		t.Errorf("FilterLogs calls = %d, want 5 (one per chunk)", fc.filterCalls)
	}
	if cur.last() != 5 {
		t.Errorf("cursor = %d, want 5", cur.last())
	}
}

// higherLog is a minimal log in a later block, used to force a boundary flush
// of the preceding (now-complete) block deterministically.
func higherLog(block uint64) types.Log {
	return types.Log{BlockNumber: block, TxHash: common.HexToHash("0xfeed")}
}

func TestSubscribe_LivePublishes(t *testing.T) {
	logs := goldenLogs(t)
	block := logs[0].BlockNumber
	// A trailing higher block completes `block`, triggering a deterministic flush.
	fc := &fakeChain{head: block, subLogs: append(append([]types.Log{}, logs...), higherLog(block+1)), subErrc: make(chan error, 1)}
	w, pub, _ := newWatcher(t, fc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- liveLoop(ctx, w, fc) }()

	deadline := time.Now().Add(5 * time.Second)
	for pub.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if pub.count() != 1 {
		t.Fatalf("live published %d, want 1", pub.count())
	}
	if got := pub.first().Size; got != "11.52" { // both logs aggregated (block flushed atomically)
		t.Errorf("aggregated size = %q, want 11.52", got)
	}
	cancel()
	<-done
}

func TestSubscribe_SkipsReorgRemovedLog(t *testing.T) {
	logs := goldenLogs(t)
	block := logs[0].BlockNumber
	// Same decodable logs, but flagged as reorg-removed by go-ethereum.
	removed := make([]types.Log, len(logs))
	for i, l := range logs {
		l.Removed = true
		removed[i] = l
	}
	fc := &fakeChain{head: block, subLogs: append(append([]types.Log{}, removed...), higherLog(block+1)), subErrc: make(chan error, 1)}
	w, pub, _ := newWatcher(t, fc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = liveLoop(ctx, w, fc) }()
	time.Sleep(300 * time.Millisecond) // allow boundary + settle flushes
	cancel()

	if pub.count() != 0 {
		t.Fatalf("published %d reorg-removed trades, want 0", pub.count())
	}
}

func TestSubscribe_DedupAcrossBackfillAndLive(t *testing.T) {
	logs := goldenLogs(t)
	block := logs[0].BlockNumber
	fc := &fakeChain{
		head:      block,
		rangeLogs: func(from, to uint64) []types.Log { return logs },
		subLogs:   append(append([]types.Log{}, logs...), higherLog(block+1)),
		subErrc:   make(chan error, 1),
	}
	w, pub, _ := newWatcher(t, fc)
	w.chunk = defaultChunk

	if err := w.backfill(context.Background(), block, block, true); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if pub.count() != 1 {
		t.Fatalf("after backfill: %d, want 1", pub.count())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = liveLoop(ctx, w, fc) }()
	time.Sleep(300 * time.Millisecond) // allow boundary + settle flushes
	cancel()
	if pub.count() != 1 {
		t.Fatalf("after live replay: %d, want 1 (deduped)", pub.count())
	}
}

// TestProcessBatch_PublishFailure_NoCommit is the H1 regression: a publish
// failure must not mark logs seen or advance the cursor, and a retry must
// re-emit the trade (at-least-once).
func TestProcessBatch_PublishFailure_NoCommit(t *testing.T) {
	logs := goldenLogs(t)
	block := logs[0].BlockNumber
	w, pub, cur := newWatcher(t, &fakeChain{})

	pub.err = errors.New("nats down")
	if err := w.processBatch(context.Background(), logs, block, true, false); err == nil {
		t.Fatal("expected error when publish fails")
	}
	if pub.count() != 0 {
		t.Fatalf("delivered %d on failure, want 0", pub.count())
	}
	if cur.set {
		t.Fatal("cursor advanced despite publish failure")
	}
	if w.seen.Len() != 0 {
		t.Fatalf("seen marked %d despite publish failure, want 0", w.seen.Len())
	}

	// Recover: the same logs must now publish (not silently dropped).
	pub.err = nil
	if err := w.processBatch(context.Background(), logs, block, true, false); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if pub.count() != 1 {
		t.Fatalf("after recovery delivered %d, want 1", pub.count())
	}
	if cur.last() != block {
		t.Fatalf("cursor = %d after recovery, want %d", cur.last(), block)
	}
}

// TestProcessBatch_NoAdvanceWhenNotDoAdvance is the H2 mechanism: the
// shutdown/ticker-mid-block path publishes but never advances the cursor.
func TestProcessBatch_NoAdvanceWhenNotDoAdvance(t *testing.T) {
	logs := goldenLogs(t)
	w, pub, cur := newWatcher(t, &fakeChain{})
	if err := w.processBatch(context.Background(), logs, logs[0].BlockNumber, false, false); err != nil {
		t.Fatalf("processBatch: %v", err)
	}
	if pub.count() != 1 {
		t.Fatalf("delivered %d, want 1 (still publishes)", pub.count())
	}
	if cur.set {
		t.Fatal("cursor advanced with doAdvance=false (would skip an incomplete block on restart)")
	}
}

// TestProcessBatch_MarksBackfilled verifies the backfilled flag reaches the bus,
// so consensus can ignore historical replays.
func TestProcessBatch_MarksBackfilled(t *testing.T) {
	logs := goldenLogs(t)
	w, pub, _ := newWatcher(t, &fakeChain{})
	if err := w.processBatch(context.Background(), logs, logs[0].BlockNumber, true, true); err != nil {
		t.Fatalf("processBatch: %v", err)
	}
	if pub.count() != 1 || !pub.first().Backfilled {
		t.Fatalf("published trade Backfilled = %v, want true", pub.first().Backfilled)
	}
}

// TestBackfill_CatchupNotBackfilled: the near-head catch-up must emit real-time
// trades (Backfilled=false) so a convergence-completing trade on reconnect still
// feeds consensus.
func TestBackfill_CatchupNotBackfilled(t *testing.T) {
	logs := goldenLogs(t)
	block := logs[0].BlockNumber
	fc := &fakeChain{head: block, rangeLogs: func(from, to uint64) []types.Log { return logs }}
	w, pub, _ := newWatcher(t, fc)
	w.chunk = defaultChunk
	if err := w.backfill(context.Background(), block, block, false); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if pub.count() != 1 || pub.first().Backfilled {
		t.Fatalf("catch-up trade Backfilled = %v, want false (real-time)", pub.first().Backfilled)
	}
}

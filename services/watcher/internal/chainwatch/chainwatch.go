// Package chainwatch drives the on-chain side of the watcher: it backfills
// missed OrderFilled logs on startup/reconnect, subscribes to live logs, and
// publishes deduplicated, aggregated trades. The pure decode/aggregate/dedup
// logic lives in the engine; this package owns the I/O loop and recovery.
package chainwatch

import (
	"context"
	"log/slog"
	"math/big"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/chain"
	"github.com/JacobTDang/LobsterRoll/pkg/metrics"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/backoff"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/decode"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/dedup"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/engine"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/watchset"
)

var (
	mWSConnected = metrics.NewGauge("lobsterroll_watcher_ws_connected", "1 while subscribed to chain logs, 0 otherwise")
	mConnects    = metrics.NewCounter("lobsterroll_watcher_ws_connects_total", "chain log subscription (re)connects")
	mTrades      = metrics.NewCounter("lobsterroll_watcher_trades_total", "trades published to the bus")
	mLastTrade   = metrics.NewGauge("lobsterroll_watcher_last_trade_unix", "unix time of the last published trade")
)

// ChainClient is the subset of go-ethereum's ethclient the watcher needs.
type ChainClient interface {
	BlockNumber(ctx context.Context) (uint64, error)
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error)
}

// Publisher publishes detected trades.
type Publisher interface {
	PublishTrade(t bus.TradeDetected) error
}

// Cursor persists the last processed block.
type Cursor interface {
	LastProcessedBlock(ctx context.Context) (uint64, bool, error)
	SetLastProcessedBlock(ctx context.Context, block uint64) error
}

// Tuning.
const (
	defaultChunk         = 2000            // backfill range per FilterLogs call
	defaultFlushInterval = 2 * time.Second // settle window for a quiet block
	reorgDepth           = 128             // keep dedup entries this far behind head
	backoffBase          = 1 * time.Second
	backoffMax           = 30 * time.Second
	healthyDuration      = time.Minute // a cycle lasting this long resets backoff
)

// Watcher subscribes to exchange logs and publishes watched-wallet trades.
type Watcher struct {
	client        ChainClient
	set           *watchset.Set
	seen          *dedup.Seen
	cursor        Cursor
	pub           Publisher
	log           *slog.Logger
	addrs         []common.Address
	topics        [][]common.Hash
	chunk         uint64
	flushInterval time.Duration
	now           func() time.Time
}

// New constructs a Watcher over the verified exchange contracts and OrderFilled
// topics (current + legacy).
func New(client ChainClient, set *watchset.Set, seen *dedup.Seen, cursor Cursor, pub Publisher, log *slog.Logger) *Watcher {
	addrs := make([]common.Address, 0, len(chain.WatchedExchanges()))
	for _, a := range chain.WatchedExchanges() {
		addrs = append(addrs, common.HexToAddress(a))
	}
	topics := [][]common.Hash{{
		common.HexToHash(chain.OrderFilledTopic),
		common.HexToHash(chain.OrderFilledTopicLegacy),
	}}
	return &Watcher{
		client: client, set: set, seen: seen, cursor: cursor, pub: pub, log: log,
		addrs: addrs, topics: topics, chunk: defaultChunk,
		flushInterval: defaultFlushInterval, now: time.Now,
	}
}

func (w *Watcher) baseQuery() ethereum.FilterQuery {
	return ethereum.FilterQuery{Addresses: w.addrs, Topics: w.topics}
}

// Run backfills then subscribes, recovering from errors with capped backoff and
// re-backfilling each reconnect (dedup makes the overlap safe). Returns nil on
// ctx cancellation.
func (w *Watcher) Run(ctx context.Context) error {
	attempt := 0
	for {
		start := w.now()
		if err := w.cycle(ctx); err != nil && ctx.Err() == nil {
			w.log.Warn("watch cycle ended", "err", err)
		}
		if ctx.Err() != nil {
			return nil
		}
		if w.now().Sub(start) >= healthyDuration {
			attempt = 0 // the cycle ran healthily; reset backoff
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff.Delay(attempt, backoffBase, backoffMax)):
		}
		attempt++
	}
}

func (w *Watcher) cycle(ctx context.Context) error {
	head, err := w.client.BlockNumber(ctx)
	if err != nil {
		return err
	}
	start, err := w.startBlock(ctx, head)
	if err != nil {
		return err
	}
	if err := w.backfill(ctx, start, head); err != nil {
		return err
	}

	// Subscribe AFTER the (potentially large) backfill, then catch up any blocks
	// mined while it ran before entering the live loop. The live stream only
	// delivers blocks produced after it activates, so without this the window
	// [head+1, subscription-activation] is never scanned and its trades are
	// silently dropped. The catch-up overlaps the live stream; dedup (w.seen)
	// absorbs the overlap. Subscribing after the big backfill (not before) avoids
	// buffering the full exchange log stream during a long reconnect replay.
	ch := make(chan types.Log, 1024)
	sub, err := w.client.SubscribeFilterLogs(ctx, w.baseQuery(), ch)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()
	mConnects.Inc()
	mWSConnected.Set(1)
	defer mWSConnected.Set(0)

	newHead, err := w.client.BlockNumber(ctx)
	if err != nil {
		return err
	}
	if newHead > head {
		if err := w.backfill(ctx, head+1, newHead); err != nil {
			return err
		}
	}
	return w.runLive(ctx, ch, sub)
}

// startBlock resumes from the persisted cursor, or starts at head on first run
// (we don't replay all history).
func (w *Watcher) startBlock(ctx context.Context, head uint64) (uint64, error) {
	last, ok, err := w.cursor.LastProcessedBlock(ctx)
	if err != nil {
		return 0, err
	}
	if !ok {
		return head, nil
	}
	return last + 1, nil
}

func (w *Watcher) backfill(ctx context.Context, from, to uint64) error {
	for start := from; start <= to; start += w.chunk {
		end := start + w.chunk - 1
		if end > to {
			end = to
		}
		q := w.baseQuery()
		q.FromBlock = new(big.Int).SetUint64(start)
		q.ToBlock = new(big.Int).SetUint64(end)
		logs, err := w.client.FilterLogs(ctx, q)
		if err != nil {
			return err
		}
		// A backfill chunk covers only past (complete) blocks, so it's safe to
		// advance the cursor to its end — but only after every trade is published.
		// backfilled=true: these are replays of historical fills, so consensus must
		// not treat them as real-time convergence.
		if err := w.processBatch(ctx, logs, end, true, true); err != nil {
			return err
		}
	}
	return nil
}

// runLive consumes the live subscription, buffering each block until complete
// before flushing + advancing the cursor. The subscription is created by cycle so
// it can be established before the catch-up backfill.
func (w *Watcher) runLive(ctx context.Context, ch <-chan types.Log, sub ethereum.Subscription) error {
	// buf holds the unpublished logs of the current block. A block is flushed
	// (and the cursor advanced) only once we know it is complete: either a higher
	// block has arrived (boundary), or no new log has arrived for a full tick
	// (settled). This keeps a tx's logs together for VWAP aggregation and never
	// advances the cursor into a partially-streamed block.
	var buf []types.Log
	var pendingBlock uint64
	grew := false

	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Emit what we have for latency, but don't advance: we may be mid-block.
			_ = w.processBatch(ctx, buf, pendingBlock, false, false)
			return nil
		case err := <-sub.Err():
			_ = w.processBatch(ctx, buf, pendingBlock, false, false)
			if err != nil {
				return err
			}
			return nil
		case l := <-ch:
			if l.Removed {
				// A reorg dropped this log; never publish it as a fresh trade.
				w.log.Warn("skipping reorg-removed log", "block", l.BlockNumber, "tx", l.TxHash.Hex(), "idx", l.Index)
				continue
			}
			if pendingBlock > 0 && l.BlockNumber < pendingBlock {
				// Out-of-order: a log for a block we've already flushed + advanced past.
				// go-ethereum delivers canonical-chain logs in monotonic block order, so
				// this shouldn't occur; skip it rather than buffer it under a higher
				// advance block, which would violate the invariant that the cursor never
				// advances into a partially-streamed lower block.
				w.log.Warn("skipping out-of-order log below pending block", "block", l.BlockNumber, "pending", pendingBlock, "tx", l.TxHash.Hex())
				continue
			}
			if len(buf) > 0 && l.BlockNumber > pendingBlock {
				// The previous block is complete: flush it atomically + advance.
				if err := w.processBatch(ctx, buf, pendingBlock, true, false); err != nil {
					return err
				}
				buf = nil
			}
			buf = append(buf, l)
			if l.BlockNumber > pendingBlock {
				pendingBlock = l.BlockNumber
			}
			grew = true
		case <-ticker.C:
			if grew {
				grew = false // block still receiving; give it another tick
				continue
			}
			if len(buf) > 0 {
				// Quiet for a full tick: treat the block as settled (complete).
				if err := w.processBatch(ctx, buf, pendingBlock, true, false); err != nil {
					return err
				}
				buf = nil
			}
		}
	}
}

// processBatch decodes, filters, and aggregates the logs, publishes each trade,
// and only then marks the consumed logs seen and (optionally) advances the
// cursor. A publish failure leaves nothing marked or advanced, so the batch is
// safely re-emitted on retry (downstream dedups by source-trade id).
func (w *Watcher) processBatch(ctx context.Context, logs []types.Log, advanceBlock uint64, doAdvance, backfilled bool) error {
	if len(logs) == 0 {
		if doAdvance {
			return w.advance(ctx, advanceBlock)
		}
		return nil
	}
	trades, consumed, _ := engine.ProcessBatch(logs, w.set, w.seen, w.log)
	for _, tr := range trades {
		if err := w.emit(ctx, tr, backfilled); err != nil {
			return err // nothing marked/advanced; retry re-emits (dedup-safe downstream)
		}
	}
	for _, c := range consumed {
		w.seen.Mark(c.Tx, c.Index, c.Block)
	}
	if doAdvance {
		return w.advance(ctx, advanceBlock)
	}
	return nil
}

func (w *Watcher) emit(_ context.Context, tr decode.Trade, backfilled bool) error {
	td := bus.TradeDetected{
		Wallet:      tr.Wallet,
		TokenID:     tr.TokenID,
		Side:        tr.Side,
		Price:       tr.Price,
		Size:        tr.Size,
		TxHash:      tr.TxHash,
		LogIndex:    tr.LogIndex,
		BlockNumber: tr.BlockNumber,
		ObservedAt:  w.now().UTC(),
		Backfilled:  backfilled,
	}
	if err := w.pub.PublishTrade(td); err != nil {
		return err
	}
	mTrades.Inc()
	mLastTrade.Set(float64(td.ObservedAt.Unix()))
	w.log.Info("trade detected",
		"wallet", td.Wallet, "side", td.Side, "size", td.Size, "price", td.Price,
		"token", td.TokenID, "tx", td.TxHash)
	return nil
}

func (w *Watcher) advance(ctx context.Context, block uint64) error {
	if err := w.cursor.SetLastProcessedBlock(ctx, block); err != nil {
		return err
	}
	if block > reorgDepth {
		w.seen.PruneBelow(block - reorgDepth)
	}
	return nil
}

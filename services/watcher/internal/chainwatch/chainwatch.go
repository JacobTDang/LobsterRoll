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
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/backoff"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/decode"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/dedup"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/engine"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/watchset"
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
	defaultChunk    = 2000             // backfill range per FilterLogs call
	flushInterval   = 2 * time.Second  // bound live-buffer latency
	reorgDepth      = 128              // keep dedup entries this far behind head
	backoffBase     = 1 * time.Second
	backoffMax      = 30 * time.Second
	healthyDuration = time.Minute // a cycle lasting this long resets backoff
)

// Watcher subscribes to exchange logs and publishes watched-wallet trades.
type Watcher struct {
	client ChainClient
	set    *watchset.Set
	seen   *dedup.Seen
	cursor Cursor
	pub    Publisher
	log    *slog.Logger
	addrs  []common.Address
	topics [][]common.Hash
	chunk  uint64
	now    func() time.Time
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
		addrs: addrs, topics: topics, chunk: defaultChunk, now: time.Now,
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
	return w.subscribe(ctx)
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
		if err := w.processAndEmit(ctx, logs); err != nil {
			return err
		}
		if err := w.advance(ctx, end); err != nil {
			return err
		}
	}
	return nil
}

func (w *Watcher) subscribe(ctx context.Context) error {
	ch := make(chan types.Log, 256)
	sub, err := w.client.SubscribeFilterLogs(ctx, w.baseQuery(), ch)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()

	var buf []types.Log
	var bufBlock uint64
	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		if err := w.processAndEmit(ctx, buf); err != nil {
			return err
		}
		err := w.advance(ctx, bufBlock)
		buf = nil
		return err
	}

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return flush()
		case err := <-sub.Err():
			_ = flush()
			if err != nil {
				return err
			}
			return nil
		case l := <-ch:
			// Flush the previous block before starting a new one, so a tx's logs
			// are aggregated together.
			if len(buf) > 0 && l.BlockNumber != bufBlock {
				if err := flush(); err != nil {
					return err
				}
			}
			buf = append(buf, l)
			bufBlock = l.BlockNumber
		case <-ticker.C:
			if err := flush(); err != nil {
				return err
			}
		}
	}
}

func (w *Watcher) processAndEmit(ctx context.Context, logs []types.Log) error {
	trades, _ := engine.ProcessBatch(logs, w.set, w.seen, w.log)
	for _, tr := range trades {
		if err := w.emit(ctx, tr); err != nil {
			return err
		}
	}
	return nil
}

func (w *Watcher) emit(_ context.Context, tr decode.Trade) error {
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
	}
	if err := w.pub.PublishTrade(td); err != nil {
		return err
	}
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

// Package engine turns a batch of raw exchange logs into deduplicated, watched,
// per-transaction-aggregated trades. It is the watcher's pure core: no network,
// no clock — fully testable from golden logs.
//
// It does NOT mark logs as seen. It reports which logs it consumed so the caller
// can record them only after the resulting trades are durably published
// (at-least-once: never drop a trade because a publish failed).
package engine

import (
	"log/slog"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/decode"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/dedup"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/watchset"
)

// Consumed identifies a log that contributed to the returned trades and should
// be marked seen once those trades are published.
type Consumed struct {
	Tx    common.Hash
	Index uint
	Block uint64
}

// ProcessBatch decodes each log, keeps only those involving a watched wallet,
// skips logs already marked seen, and aggregates a watched wallet's fills within
// a transaction into one VWAP trade. It returns the trades, the logs consumed
// (to be marked seen after publish), and the highest block number in the batch.
//
// Malformed logs are logged and skipped (fail-soft). The dedup set is consulted
// read-only here; only relevant logs are reported as consumed.
func ProcessBatch(logs []types.Log, set *watchset.Set, seen *dedup.Seen, log *slog.Logger) ([]decode.Trade, []Consumed, uint64) {
	var fills []decode.Fill
	var consumed []Consumed
	var maxBlock uint64

	for _, l := range logs {
		if l.BlockNumber > maxBlock {
			maxBlock = l.BlockNumber
		}

		of, err := decode.Decode(l)
		if err != nil {
			if log != nil {
				log.Warn("skipping undecodable log", "tx", l.TxHash.Hex(), "index", l.Index, "err", err)
			}
			continue
		}

		makerWatched := set.Has(of.Maker)
		takerWatched := set.Has(of.Taker)
		if !makerWatched && !takerWatched {
			continue
		}

		// Already emitted in a prior committed batch? Skip (read-only check).
		if seen.Has(l.TxHash, l.Index) {
			continue
		}
		consumed = append(consumed, Consumed{Tx: l.TxHash, Index: l.Index, Block: l.BlockNumber})

		if makerWatched {
			if f, ok := of.FillFor(of.Maker); ok {
				fills = append(fills, f)
			}
		}
		if takerWatched {
			if f, ok := of.FillFor(of.Taker); ok {
				fills = append(fills, f)
			}
		}
	}

	return decode.AggregateByTx(fills), consumed, maxBlock
}

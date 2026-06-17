// Package engine turns a batch of raw exchange logs into deduplicated, watched,
// per-transaction-aggregated trades. It is the watcher's pure core: no network,
// no clock — fully testable from golden logs.
package engine

import (
	"log/slog"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/decode"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/dedup"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/watchset"
)

// ProcessBatch decodes each log, keeps only those involving a watched wallet,
// dedups by (txHash, logIndex) so backfill/live overlap can't double-emit, and
// aggregates a watched wallet's fills within a transaction into one VWAP trade.
// It returns the trades and the highest block number seen in the batch.
//
// Malformed logs are logged and skipped (fail-soft) so one bad log can't stall
// the pipeline. Only logs that involve a watched wallet are recorded in seen,
// keeping the dedup set bounded to relevant activity.
func ProcessBatch(logs []types.Log, set *watchset.Set, seen *dedup.Seen, log *slog.Logger) ([]decode.Trade, uint64) {
	var fills []decode.Fill
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

		// Dedup only relevant logs (those that would emit a trade).
		if seen.Mark(l.TxHash, l.Index, l.BlockNumber) {
			continue
		}

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

	return decode.AggregateByTx(fills), maxBlock
}

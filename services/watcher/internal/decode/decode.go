// Package decode turns raw Polymarket CTF Exchange OrderFilled logs into
// Trades from a watched wallet's perspective.
//
// Current ABI (verified against Sourcify + live logs):
//
//	OrderFilled(bytes32 indexed orderHash, address indexed maker,
//	  address indexed taker, uint8 side, uint256 tokenId,
//	  uint256 makerAmountFilled, uint256 takerAmountFilled, uint256 fee,
//	  bytes32 builder, bytes32 metadata)
//
// `side` is the MAKER's side (0=BUY, 1=SELL). Amounts are 6-decimal fixed point
// (USDC and shares both 1e6).
package decode

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/JacobTDang/LobsterRoll/pkg/chain"
)

// usdcDecimals is the fixed-point scale for both USDC and share amounts.
const usdcDecimals = 6

// Side values for the maker's order.
const (
	sideBuy  = 0
	sideSell = 1
)

// OrderFilled is the decoded event (current ABI).
type OrderFilled struct {
	OrderHash   common.Hash
	Maker       common.Address
	Taker       common.Address
	Side        uint8 // maker's side: 0=BUY, 1=SELL
	TokenID     *big.Int
	MakerAmount *big.Int
	TakerAmount *big.Int
	Fee         *big.Int
	TxHash      common.Hash
	LogIndex    uint64
	BlockNumber uint64
}

// Trade is a single fill from a watched wallet's perspective, with price/size as
// human-readable decimal strings.
type Trade struct {
	Wallet      string
	TokenID     string
	Side        string // "buy" | "sell"
	Price       string // USDC per share, decimal string
	Size        string // shares, decimal string
	TxHash      string
	LogIndex    uint64
	BlockNumber uint64
}

// DecodeOrderFilled decodes a current-ABI OrderFilled log. It fails loudly on
// anything that doesn't match the expected shape rather than risk a mis-decode.
func DecodeOrderFilled(log types.Log) (OrderFilled, error) {
	if len(log.Topics) != 4 {
		return OrderFilled{}, fmt.Errorf("OrderFilled: want 4 topics, got %d", len(log.Topics))
	}
	if log.Topics[0].Hex() != chain.OrderFilledTopic {
		return OrderFilled{}, fmt.Errorf("OrderFilled: unexpected topic0 %s", log.Topics[0].Hex())
	}
	const wantWords = 7
	if len(log.Data) != wantWords*32 {
		return OrderFilled{}, fmt.Errorf("OrderFilled: want %d data bytes, got %d", wantWords*32, len(log.Data))
	}

	word := func(i int) []byte { return log.Data[i*32 : (i+1)*32] }
	side := new(big.Int).SetBytes(word(0))
	if !side.IsUint64() || (side.Uint64() != sideBuy && side.Uint64() != sideSell) {
		return OrderFilled{}, fmt.Errorf("OrderFilled: invalid side %s", side)
	}

	return OrderFilled{
		OrderHash:   log.Topics[1],
		Maker:       common.BytesToAddress(log.Topics[2].Bytes()),
		Taker:       common.BytesToAddress(log.Topics[3].Bytes()),
		Side:        uint8(side.Uint64()),
		TokenID:     new(big.Int).SetBytes(word(1)),
		MakerAmount: new(big.Int).SetBytes(word(2)),
		TakerAmount: new(big.Int).SetBytes(word(3)),
		Fee:         new(big.Int).SetBytes(word(4)),
		TxHash:      log.TxHash,
		LogIndex:    uint64(log.Index),
		BlockNumber: log.BlockNumber,
	}, nil
}

// DecodeOrderFilledLegacy decodes a legacy-ABI OrderFilled log (CTFExchangeLegacy):
//
//	OrderFilled(bytes32 orderHash, address maker, address taker,
//	  uint256 makerAssetId, uint256 takerAssetId,
//	  uint256 makerAmountFilled, uint256 takerAmountFilled, uint256 fee)
//
// Side is inferred from which asset is collateral (assetId 0 == USDC): if the
// maker provides USDC it is a BUY, if the maker provides the token it is a SELL.
// The result is normalized to the same OrderFilled shape so TradeFor is shared.
func DecodeOrderFilledLegacy(log types.Log) (OrderFilled, error) {
	if len(log.Topics) != 4 {
		return OrderFilled{}, fmt.Errorf("legacy OrderFilled: want 4 topics, got %d", len(log.Topics))
	}
	if log.Topics[0].Hex() != chain.OrderFilledTopicLegacy {
		return OrderFilled{}, fmt.Errorf("legacy OrderFilled: unexpected topic0 %s", log.Topics[0].Hex())
	}
	const wantWords = 5
	if len(log.Data) != wantWords*32 {
		return OrderFilled{}, fmt.Errorf("legacy OrderFilled: want %d data bytes, got %d", wantWords*32, len(log.Data))
	}

	word := func(i int) *big.Int { return new(big.Int).SetBytes(log.Data[i*32 : (i+1)*32]) }
	makerAssetID, takerAssetID := word(0), word(1)

	var side uint8
	var tokenID *big.Int
	switch {
	case makerAssetID.Sign() == 0 && takerAssetID.Sign() != 0:
		side, tokenID = sideBuy, takerAssetID // maker provides USDC -> BUY token
	case takerAssetID.Sign() == 0 && makerAssetID.Sign() != 0:
		side, tokenID = sideSell, makerAssetID // maker provides token -> SELL
	default:
		return OrderFilled{}, fmt.Errorf("legacy OrderFilled: ambiguous assets (maker=%s taker=%s)", makerAssetID, takerAssetID)
	}

	return OrderFilled{
		OrderHash:   log.Topics[1],
		Maker:       common.BytesToAddress(log.Topics[2].Bytes()),
		Taker:       common.BytesToAddress(log.Topics[3].Bytes()),
		Side:        side,
		TokenID:     tokenID,
		MakerAmount: word(2),
		TakerAmount: word(3),
		Fee:         word(4),
		TxHash:      log.TxHash,
		LogIndex:    uint64(log.Index),
		BlockNumber: log.BlockNumber,
	}, nil
}

// Decode dispatches to the current or legacy decoder based on the log's topic0,
// so callers don't need to know which exchange a log came from.
func Decode(log types.Log) (OrderFilled, error) {
	if len(log.Topics) == 0 {
		return OrderFilled{}, fmt.Errorf("OrderFilled: log has no topics")
	}
	switch log.Topics[0].Hex() {
	case chain.OrderFilledTopic:
		return DecodeOrderFilled(log)
	case chain.OrderFilledTopicLegacy:
		return DecodeOrderFilledLegacy(log)
	default:
		return OrderFilled{}, fmt.Errorf("OrderFilled: unknown topic0 %s", log.Topics[0].Hex())
	}
}

// Fill is one decoded fill from a watched wallet's perspective, with raw
// 6-decimal integer amounts so fills can be aggregated before formatting.
type Fill struct {
	Wallet      string
	TokenID     string // decimal
	Buy         bool
	USDC        *big.Int // 1e6
	Shares      *big.Int // 1e6
	TxHash      string
	LogIndex    uint64
	BlockNumber uint64
}

// FillFor returns the fill as seen by wallet, or ok=false if wallet is neither
// the maker nor the taker of this event.
//
// The (usdc, shares) pair is fixed by the maker's side; only the side label
// flips when the watched wallet is the taker (the counterparty).
func (of OrderFilled) FillFor(wallet common.Address) (Fill, bool) {
	isMaker := wallet == of.Maker
	isTaker := wallet == of.Taker
	if !isMaker && !isTaker {
		return Fill{}, false
	}

	var usdc, shares *big.Int
	makerSideBuy := of.Side == sideBuy
	if makerSideBuy {
		usdc, shares = of.MakerAmount, of.TakerAmount // maker pays USDC, gets shares
	} else {
		shares, usdc = of.MakerAmount, of.TakerAmount // maker gives shares, gets USDC
	}

	walletBuys := makerSideBuy
	if isTaker {
		walletBuys = !makerSideBuy // taker takes the opposite side of the maker
	}

	return Fill{
		Wallet:      strings.ToLower(wallet.Hex()),
		TokenID:     of.TokenID.String(),
		Buy:         walletBuys,
		USDC:        usdc,
		Shares:      shares,
		TxHash:      of.TxHash.Hex(),
		LogIndex:    of.LogIndex,
		BlockNumber: of.BlockNumber,
	}, true
}

// TradeFor returns the trade as seen by wallet, or ok=false if wallet is neither
// the maker nor the taker of this fill.
func (of OrderFilled) TradeFor(wallet common.Address) (Trade, bool) {
	f, ok := of.FillFor(wallet)
	if !ok {
		return Trade{}, false
	}
	return tradeFromFill(f), true
}

func tradeFromFill(f Fill) Trade {
	side := "sell"
	if f.Buy {
		side = "buy"
	}
	return Trade{
		Wallet:      f.Wallet,
		TokenID:     f.TokenID,
		Side:        side,
		Price:       priceString(f.USDC, f.Shares),
		Size:        fixedString(f.Shares, usdcDecimals),
		TxHash:      f.TxHash,
		LogIndex:    f.LogIndex,
		BlockNumber: f.BlockNumber,
	}
}

// AggregateByTx combines fills that belong to the same wallet, token, side, and
// transaction into one Trade with the summed size and size-weighted average
// price. A single aggressor (taker) trade that the exchange splits across many
// maker fills thus surfaces as one Trade. The aggregated LogIndex is the minimum
// in the group (stable identity for dedup/ordering). Output is sorted by
// (TxHash, LogIndex) for determinism.
func AggregateByTx(fills []Fill) []Trade {
	type key struct {
		tx, wallet, token string
		buy               bool
	}
	type acc struct {
		usdc, shares *big.Int
		minIdx, blk  uint64
	}
	groups := make(map[key]*acc)
	var order []key
	for _, f := range fills {
		k := key{f.TxHash, f.Wallet, f.TokenID, f.Buy}
		a, ok := groups[k]
		if !ok {
			a = &acc{usdc: new(big.Int), shares: new(big.Int), minIdx: f.LogIndex, blk: f.BlockNumber}
			groups[k] = a
			order = append(order, k)
		}
		a.usdc.Add(a.usdc, f.USDC)
		a.shares.Add(a.shares, f.Shares)
		if f.LogIndex < a.minIdx {
			a.minIdx = f.LogIndex
		}
	}

	trades := make([]Trade, 0, len(order))
	for _, k := range order {
		a := groups[k]
		trades = append(trades, tradeFromFill(Fill{
			Wallet: k.wallet, TokenID: k.token, Buy: k.buy,
			USDC: a.usdc, Shares: a.shares,
			TxHash: k.tx, LogIndex: a.minIdx, BlockNumber: a.blk,
		}))
	}
	sort.Slice(trades, func(i, j int) bool {
		if trades[i].TxHash != trades[j].TxHash {
			return trades[i].TxHash < trades[j].TxHash
		}
		return trades[i].LogIndex < trades[j].LogIndex
	})
	return trades
}

// priceString returns usdc/shares (USDC per share) as a trimmed decimal string.
func priceString(usdc, shares *big.Int) string {
	if shares.Sign() == 0 {
		return "0"
	}
	r := new(big.Rat).SetFrac(usdc, shares)
	return trimDecimal(r.FloatString(usdcDecimals))
}

// fixedString formats a 1e{decimals} fixed-point integer as a trimmed decimal.
func fixedString(n *big.Int, decimals int) string {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	q, rem := new(big.Int).QuoRem(n, scale, new(big.Int))
	if rem.Sign() == 0 {
		return q.String()
	}
	frac := fmt.Sprintf("%0*d", decimals, rem)
	return trimDecimal(q.String() + "." + frac)
}

// trimDecimal removes trailing zeros (and a trailing dot) from a decimal string.
func trimDecimal(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}

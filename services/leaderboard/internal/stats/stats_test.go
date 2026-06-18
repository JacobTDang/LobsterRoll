package stats

import (
	"math"
	"testing"

	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/dataapi"
)

func act(typ, side string, size float64, cond string) dataapi.Activity {
	return dataapi.Activity{Type: typ, Side: side, USDCSize: size, ConditionID: cond}
}

// actS includes a share quantity (for closed-by-sell resolution tests).
func actS(typ, side string, usdc, shares float64, cond string) dataapi.Activity {
	return dataapi.Activity{Type: typ, Side: side, USDCSize: usdc, Size: shares, ConditionID: cond}
}

const eps = 1e-9

func approx(a, b float64) bool { return math.Abs(a-b) < eps }

func TestCompute_ROI(t *testing.T) {
	// m1: buy $100, redeem $160 -> +$60 on $100. m2: buy $100, redeem $80 -> -$20.
	// Aggregate: +$40 on $200 deployed -> ROI = 0.20.
	acts := []dataapi.Activity{
		act(typeTrade, sideBuy, 100, "m1"),
		act(typeRedeem, "", 160, "m1"),
		act(typeTrade, sideBuy, 100, "m2"),
		act(typeRedeem, "", 80, "m2"),
	}
	s := Compute(acts)
	if s.ResolvedMarkets != 2 {
		t.Fatalf("ResolvedMarkets = %d, want 2", s.ResolvedMarkets)
	}
	if !approx(s.CapitalDeployed, 200) || !approx(s.RealizedPnL, 40) || !approx(s.ROI, 0.20) {
		t.Errorf("got cap=%v pnl=%v roi=%v, want 200/40/0.20", s.CapitalDeployed, s.RealizedPnL, s.ROI)
	}
}

func TestCompute_ROI_SplitCountsAsCapital(t *testing.T) {
	// A SPLIT deploys capital; redeeming resolves it. -$100 (split) +$130 (redeem)
	// = +$30 on $100 deployed -> ROI 0.30.
	acts := []dataapi.Activity{
		act(typeSplit, "", 100, "m1"),
		act(typeRedeem, "", 130, "m1"),
	}
	s := Compute(acts)
	if !approx(s.CapitalDeployed, 100) || !approx(s.RealizedPnL, 30) || !approx(s.ROI, 0.30) {
		t.Errorf("got cap=%v pnl=%v roi=%v, want 100/30/0.30", s.CapitalDeployed, s.RealizedPnL, s.ROI)
	}
}

func TestCompute_NoResolved(t *testing.T) {
	// Two markets traded but never redeemed -> nothing resolved.
	acts := []dataapi.Activity{
		act(typeTrade, sideBuy, 100, "m1"),
		act(typeTrade, sideSell, 50, "m1"),
		act(typeTrade, sideBuy, 200, "m2"),
	}
	s := Compute(acts)
	if s.ResolvedMarkets != 0 {
		t.Errorf("ResolvedMarkets = %d, want 0", s.ResolvedMarkets)
	}
	if s.WinRate != 0 {
		t.Errorf("WinRate = %v, want 0", s.WinRate)
	}
	if s.RealizedPnL != 0 {
		t.Errorf("RealizedPnL = %v, want 0", s.RealizedPnL)
	}
	if s.TradedMarkets != 2 {
		t.Errorf("TradedMarkets = %d, want 2", s.TradedMarkets)
	}
}

func TestCompute_AllWins(t *testing.T) {
	// m1: buy 100, redeem 250 -> net +150 (win). m2: buy 50, redeem 80 -> +30 (win).
	acts := []dataapi.Activity{
		act(typeTrade, sideBuy, 100, "m1"),
		act(typeRedeem, "", 250, "m1"),
		act(typeTrade, sideBuy, 50, "m2"),
		act(typeRedeem, "", 80, "m2"),
	}
	s := Compute(acts)
	if s.ResolvedMarkets != 2 {
		t.Fatalf("ResolvedMarkets = %d, want 2", s.ResolvedMarkets)
	}
	if !approx(s.WinRate, 1.0) {
		t.Errorf("WinRate = %v, want 1.0", s.WinRate)
	}
	if !approx(s.RealizedPnL, 180) {
		t.Errorf("RealizedPnL = %v, want 180", s.RealizedPnL)
	}
}

func TestCompute_Mixed(t *testing.T) {
	// m1 win: buy 100, redeem 300 -> +200.
	// m2 loss: buy 100, redeem 0 (resolved via redeem of 0) -> -100.
	// m3 traded only (no redeem) -> not resolved, excluded from win rate/pnl.
	acts := []dataapi.Activity{
		act(typeTrade, sideBuy, 100, "m1"),
		act(typeRedeem, "", 300, "m1"),
		act(typeTrade, sideBuy, 100, "m2"),
		act(typeRedeem, "", 0, "m2"),
		act(typeTrade, sideBuy, 500, "m3"),
	}
	s := Compute(acts)
	if s.ResolvedMarkets != 2 {
		t.Fatalf("ResolvedMarkets = %d, want 2", s.ResolvedMarkets)
	}
	if !approx(s.WinRate, 0.5) {
		t.Errorf("WinRate = %v, want 0.5", s.WinRate)
	}
	if !approx(s.RealizedPnL, 100) { // +200 + (-100)
		t.Errorf("RealizedPnL = %v, want 100", s.RealizedPnL)
	}
	if s.TradedMarkets != 3 {
		t.Errorf("TradedMarkets = %d, want 3", s.TradedMarkets)
	}
}

// Mutation-strength: this would FAIL if SELL were treated as cash-out (-).
func TestCompute_SellIsCashIn(t *testing.T) {
	// buy 100, sell 60, redeem 70 -> +30 net (win). If SELL sign were flipped
	// to -60 the net would be -90 (loss) and the win count would be 0.
	acts := []dataapi.Activity{
		act(typeTrade, sideBuy, 100, "m1"),
		act(typeTrade, sideSell, 60, "m1"),
		act(typeRedeem, "", 70, "m1"),
	}
	s := Compute(acts)
	if !approx(s.RealizedPnL, 30) {
		t.Fatalf("RealizedPnL = %v, want 30 (SELL must be cash-in)", s.RealizedPnL)
	}
	if !approx(s.WinRate, 1.0) {
		t.Errorf("WinRate = %v, want 1.0", s.WinRate)
	}
}

// Mutation-strength: would FAIL if BUY were treated as cash-in (+).
func TestCompute_BuyIsCashOut(t *testing.T) {
	// buy 100, redeem 40 -> -60 net (loss). If BUY sign were flipped this would
	// be +140 (win).
	acts := []dataapi.Activity{
		act(typeTrade, sideBuy, 100, "m1"),
		act(typeRedeem, "", 40, "m1"),
	}
	s := Compute(acts)
	if !approx(s.RealizedPnL, -60) {
		t.Fatalf("RealizedPnL = %v, want -60 (BUY must be cash-out)", s.RealizedPnL)
	}
	if !approx(s.WinRate, 0) {
		t.Errorf("WinRate = %v, want 0 (loss)", s.WinRate)
	}
}

// Mutation-strength: MERGE is cash-in (+), SPLIT is cash-out (-).
func TestCompute_MergeSplitSigns(t *testing.T) {
	// m1: split 100 (cash out), merge 100 (cash in), redeem 50 -> net +50 (win).
	acts := []dataapi.Activity{
		act(typeSplit, "", 100, "m1"),
		act(typeMerge, "", 100, "m1"),
		act(typeRedeem, "", 50, "m1"),
	}
	s := Compute(acts)
	if !approx(s.RealizedPnL, 50) {
		t.Fatalf("RealizedPnL = %v, want 50 (merge +, split -)", s.RealizedPnL)
	}
	if !approx(s.WinRate, 1.0) {
		t.Errorf("WinRate = %v, want 1.0", s.WinRate)
	}
}

// REWARD events must be ignored entirely (no effect on net or resolution).
func TestCompute_RewardIgnored(t *testing.T) {
	// Without REWARD: buy 100, redeem 90 -> -10 net (loss). A REWARD of 1000
	// must NOT flip this to a win.
	acts := []dataapi.Activity{
		act(typeTrade, sideBuy, 100, "m1"),
		act("REWARD", "", 1000, "m1"),
		act(typeRedeem, "", 90, "m1"),
	}
	s := Compute(acts)
	if !approx(s.RealizedPnL, -10) {
		t.Fatalf("RealizedPnL = %v, want -10 (REWARD ignored)", s.RealizedPnL)
	}
	if !approx(s.WinRate, 0) {
		t.Errorf("WinRate = %v, want 0", s.WinRate)
	}
}

// A REWARD-only market is never resolved (REWARD does not mark resolution).
func TestCompute_RewardDoesNotResolve(t *testing.T) {
	acts := []dataapi.Activity{
		act(typeTrade, sideBuy, 100, "m1"),
		act("REWARD", "", 5, "m1"),
	}
	s := Compute(acts)
	if s.ResolvedMarkets != 0 {
		t.Errorf("ResolvedMarkets = %d, want 0 (REWARD must not resolve)", s.ResolvedMarkets)
	}
	if s.TradedMarkets != 1 {
		t.Errorf("TradedMarkets = %d, want 1", s.TradedMarkets)
	}
}

// The key fix: a position closed by selling out (no redeem) at a LOSS must be
// counted as a resolved loss — a redeem-only rule would omit it and inflate win rate.
func TestCompute_ClosedBySellCountsLoss(t *testing.T) {
	// Bought 100 shares for $50, sold all 100 for $30 -> net -$20, no redeem.
	acts := []dataapi.Activity{
		actS(typeTrade, sideBuy, 50, 100, "m1"),
		actS(typeTrade, sideSell, 30, 100, "m1"),
	}
	s := Compute(acts)
	if s.ResolvedMarkets != 1 {
		t.Fatalf("ResolvedMarkets = %d, want 1 (closed by selling out)", s.ResolvedMarkets)
	}
	if !approx(s.RealizedPnL, -20) {
		t.Errorf("RealizedPnL = %v, want -20", s.RealizedPnL)
	}
	if !approx(s.WinRate, 0) {
		t.Errorf("WinRate = %v, want 0 (closed at a loss)", s.WinRate)
	}
}

func TestCompute_ClosedBySellWin(t *testing.T) {
	// Bought 100 for $50, sold all 100 for $70 -> +$20, no redeem.
	s := Compute([]dataapi.Activity{
		actS(typeTrade, sideBuy, 50, 100, "m1"),
		actS(typeTrade, sideSell, 70, 100, "m1"),
	})
	if s.ResolvedMarkets != 1 || !approx(s.WinRate, 1.0) || !approx(s.RealizedPnL, 20) {
		t.Fatalf("got %+v, want 1 resolved / winRate 1 / pnl 20", s)
	}
}

func TestCompute_PartialSellNotResolved(t *testing.T) {
	// Bought 100, sold only 50 -> still holding 50 shares -> NOT resolved.
	s := Compute([]dataapi.Activity{
		actS(typeTrade, sideBuy, 50, 100, "m1"),
		actS(typeTrade, sideSell, 30, 50, "m1"),
	})
	if s.ResolvedMarkets != 0 {
		t.Fatalf("ResolvedMarkets = %d, want 0 (position only half-exited)", s.ResolvedMarkets)
	}
}

func TestCompute_Empty(t *testing.T) {
	s := Compute(nil)
	if s != (Stats{}) {
		t.Errorf("Compute(nil) = %+v, want zero", s)
	}
}

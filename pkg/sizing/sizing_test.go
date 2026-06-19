package sizing

import (
	"math"
	"testing"
)

// base config + a clearly-sizable input; tests tweak one field at a time.
func cfg() Config {
	return Config{
		KellyFraction: 0.25, EdgeBuffer: 0.02, MaxSpread: 0.04,
		PerBetFrac: 0.03, MaxExposureFrac: 0.10, DDDerisk: 0.10, DDStop: 0.20,
		CLVFull: 50, MinStakeUSD: 1,
	}
}

func sizable() Inputs {
	return Inputs{
		Price: 0.50, HalfSpread: 0.005, FeeRate: 0, Slippage: 0,
		AvgCLV: 0.10, CLVN: 100, ShrunkROI: 0.30, Fresh: true,
		Bankroll: 10_000, Exposure: 0, DrawdownFrac: 0, DepthCapUSD: 0,
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestSize_BasicKelly(t *testing.T) {
	// edge = CLV 0.10 (full confidence), cost = halfSpread 0.005, edgeEff = 0.095.
	// f = 0.095/0.5 = 0.19; stake = 0.25*0.19*10000 = 475. Per-bet cap = 300 -> 300.
	d := Size(sizable(), cfg())
	if d.Reason != "" {
		t.Fatalf("unexpected skip: %q", d.Reason)
	}
	if !approx(d.Stake, 300) { // capped by PerBetFrac 0.03 * 10000
		t.Errorf("stake = %v, want 300 (per-bet cap)", d.Stake)
	}
}

func TestSize_KellyBelowCap(t *testing.T) {
	in := sizable()
	in.AvgCLV = 0.03 // small edge: edgeEff = 0.025; f = 0.05; stake = 0.25*0.05*10000 = 125
	d := Size(in, cfg())
	if d.Reason != "" || !approx(d.Stake, 125) {
		t.Fatalf("stake = %v reason=%q, want 125", d.Stake, d.Reason)
	}
}

func TestSize_SkipReasons(t *testing.T) {
	cases := []struct {
		name   string
		mut    func(*Inputs)
		reason string
	}{
		{"not fresh", func(i *Inputs) { i.Fresh = false }, "leader cooling off"},
		{"wide spread", func(i *Inputs) { i.HalfSpread = 0.03 }, "spread too wide"},
		{"dd stop", func(i *Inputs) { i.DrawdownFrac = 0.25 }, "drawdown stop"},
		{"no edge", func(i *Inputs) { i.AvgCLV = 0; i.ShrunkROI = 0 }, "no edge"},
		{"edge below buffer", func(i *Inputs) { i.AvgCLV = 0.02 }, "edge below buffer after costs"},
		{"no bankroll", func(i *Inputs) { i.Bankroll = 0 }, "no bankroll"},
		{"bad price", func(i *Inputs) { i.Price = 1 }, "price out of range"},
		{"exposure full", func(i *Inputs) { i.Exposure = 1000 }, "exposure cap reached"}, // cap=0.10*10000=1000
	}
	for _, c := range cases {
		in := sizable()
		c.mut(&in)
		if d := Size(in, cfg()); d.Reason != c.reason {
			t.Errorf("%s: reason = %q, want %q (stake %v)", c.name, d.Reason, c.reason, d.Stake)
		}
	}
}

func TestSize_DepthCapBinds(t *testing.T) {
	in := sizable()
	in.DepthCapUSD = 50 // below the per-bet cap of 300
	if d := Size(in, cfg()); !approx(d.Stake, 50) {
		t.Errorf("stake = %v, want 50 (depth cap)", d.Stake)
	}
}

func TestSize_DrawdownHalves(t *testing.T) {
	in := sizable()
	in.DrawdownFrac = 0.12 // >= DDDerisk 0.10, < DDStop -> halve
	// per-bet cap 300 then halved -> 150.
	if d := Size(in, cfg()); !approx(d.Stake, 150) {
		t.Errorf("stake = %v, want 150 (drawdown de-risk halves)", d.Stake)
	}
}

func TestSize_ExposureCapClamps(t *testing.T) {
	in := sizable()
	in.Exposure = 950 // cap 1000 -> only 50 available, below per-bet 300
	if d := Size(in, cfg()); !approx(d.Stake, 50) {
		t.Errorf("stake = %v, want 50 (exposure headroom)", d.Stake)
	}
}

func TestSize_ROIFallbackWhenNoCLV(t *testing.T) {
	in := sizable()
	in.CLVN = 0       // no CLV -> lean on ROI
	in.AvgCLV = 0.10  // ignored (weight 0)
	in.ShrunkROI = 0.30
	// edgeROI = price*ROI = 0.5*0.3 = 0.15; cost 0.005; edgeEff 0.145; f=0.29;
	// stake 0.25*0.29*10000=725 -> per-bet cap 300.
	d := Size(in, cfg())
	if d.Reason != "" || !approx(d.Stake, 300) {
		t.Fatalf("ROI fallback: stake=%v reason=%q, want 300", d.Stake, d.Reason)
	}
	// Negative ROI and no CLV -> no edge.
	in.ShrunkROI = -0.2
	if d := Size(in, cfg()); d.Reason != "no edge" {
		t.Errorf("negative ROI: reason=%q, want 'no edge'", d.Reason)
	}
}

// Command verifysizing runs sample signals through the pure sizing engine with
// stubbed inputs and prints the stake + skip reason for each — a local, US-safe
// way to sanity-check and tune KELLY_FRACTION / caps without touching trading.
package main

import (
	"flag"
	"fmt"

	"github.com/JacobTDang/LobsterRoll/pkg/sizing"
)

func main() {
	bankroll := flag.Float64("bankroll", 10_000, "bankroll USD")
	kelly := flag.Float64("kelly", 0.25, "Kelly fraction")
	flag.Parse()

	cfg := sizing.Config{
		KellyFraction: *kelly, EdgeBuffer: 0.02, MaxSpread: 0.02, PerBetFrac: 0.03,
		MaxExposureFrac: 0.10, DDDerisk: 0.08, DDStop: 0.15, CLVFull: 50, MinStakeUSD: 1,
	}
	base := sizing.Inputs{
		Price: 0.50, HalfSpread: 0.005, AvgCLV: 0.08, CLVN: 100, ShrunkROI: 0.30,
		Fresh: true, Bankroll: *bankroll, DepthCapUSD: 100_000,
	}

	scenarios := []struct {
		name string
		mut  func(*sizing.Inputs)
	}{
		{"strong CLV edge", nil},
		{"ROI-only (no CLV obs)", func(i *sizing.Inputs) { i.CLVN = 0 }},
		{"thin edge", func(i *sizing.Inputs) { i.AvgCLV = 0.03; i.CLVN = 100 }},
		{"wide spread", func(i *sizing.Inputs) { i.HalfSpread = 0.03 }},
		{"cooling leader", func(i *sizing.Inputs) { i.Fresh = false }},
		{"shallow book", func(i *sizing.Inputs) { i.DepthCapUSD = 40 }},
		{"in drawdown", func(i *sizing.Inputs) { i.DrawdownFrac = 0.12 }},
		{"exposure near cap", func(i *sizing.Inputs) { i.Exposure = 950 }},
		{"no edge", func(i *sizing.Inputs) { i.AvgCLV = 0; i.ShrunkROI = 0 }},
	}

	fmt.Printf("bankroll=$%.0f  kelly=%.2f  per-bet cap=$%.0f  exposure cap=$%.0f\n\n",
		*bankroll, *kelly, cfg.PerBetFrac*(*bankroll), cfg.MaxExposureFrac*(*bankroll))
	fmt.Printf("%-22s %12s   %s\n", "scenario", "stake", "note")
	fmt.Printf("%-22s %12s   %s\n", "--------", "-----", "----")
	for _, s := range scenarios {
		in := base
		if s.mut != nil {
			s.mut(&in)
		}
		d := sizing.Size(in, cfg)
		if d.Reason != "" {
			fmt.Printf("%-22s %12s   skip: %s\n", s.name, "-", d.Reason)
		} else {
			fmt.Printf("%-22s %11.2f$   sized\n", s.name, d.Stake)
		}
	}
}

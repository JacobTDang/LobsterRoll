package skill

import "math"

const (
	// freshMinSamples is the minimum resolved bets before cooling-off detection is
	// trusted; below it a wallet is treated as fresh (insufficient evidence).
	freshMinSamples = 20
	// cusumK is the slack (half the ~1σ shift to detect); cusumH is the decision
	// threshold (ARL0 ≈ 465 false-alarm interval at these values).
	cusumK = 0.5
	cusumH = 5.0
	// cusumClamp bounds any single standardized observation's influence. Returns
	// are standardized by the series' own in-sample sd, so a long, tightly-clustered
	// winning streak collapses sd and turns one ordinary recent loss into an
	// enormous z that would breach cusumH in a SINGLE step — flagging the most
	// consistent winners as "cooling off" (exactly backwards). Clamping to 4σ caps a
	// single step's contribution at clamp-k = 3.5 (< cusumH), so only a SUSTAINED
	// downward run can accumulate past the threshold. 4σ events are negligibly rare
	// under ~N(0,1), so the ARL0 on well-behaved series is essentially unchanged.
	cusumClamp = 4.0
)

// Fresh reports whether a wallet is NOT cooling off, via a one-sided lower CUSUM
// on its own return series (oldest first). The slack drains ordinary noise to 0
// while a sustained run below the wallet's own mean accumulates past the
// threshold — flagging a recent downward regime. Too few samples or zero
// variance returns true (not enough evidence to flag).
func Fresh(returns []float64) bool {
	if len(returns) < freshMinSamples {
		return true
	}
	mean, sd := meanSD(returns)
	if sd < 1e-9 { // (near-)constant series: nothing to detect
		return true
	}
	var sLo float64
	for _, r := range returns {
		z := (r - mean) / sd
		if z > cusumClamp {
			z = cusumClamp
		} else if z < -cusumClamp {
			z = -cusumClamp
		}
		if sLo = sLo - z - cusumK; sLo < 0 {
			sLo = 0
		}
	}
	return sLo <= cusumH
}

func meanSD(xs []float64) (mean, sd float64) {
	n := float64(len(xs))
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean = sum / n
	var ss float64
	for _, x := range xs {
		d := x - mean
		ss += d * d
	}
	sd = math.Sqrt(ss / n)
	return mean, sd
}

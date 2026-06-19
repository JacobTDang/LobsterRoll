package skill

import "testing"

func rep(v float64, n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = v
	}
	return s
}

func TestFresh_SteadyWalletIsFresh(t *testing.T) {
	// 40 markets all at +20% return: steady, no cooling.
	if !Fresh(rep(0.20, 40)) {
		t.Error("steady positive wallet should be fresh")
	}
}

func TestFresh_RecentSlumpFlagged(t *testing.T) {
	// Long good history then a sustained recent losing streak -> cooling.
	r := rep(0.30, 40)
	for i := 0; i < 15; i++ {
		r = append(r, -0.8) // 15 consecutive heavy losses
	}
	if Fresh(r) {
		t.Error("a sustained recent losing streak should be flagged as cooling (not fresh)")
	}
}

func TestFresh_OneRecentLossOnSteadyWinnerStaysFresh(t *testing.T) {
	// Regression (HIGH bug): a long, consistent winner with ONE ordinary recent
	// loss must NOT be flagged. In-sample sd collapse used to make a single loss
	// breach the threshold in one step — gating out exactly the best wallets.
	if !Fresh(append(rep(0.30, 40), -0.5)) {
		t.Error("one recent loss on a long steady winner should stay fresh")
	}
	// But a SUSTAINED recent slump (2+) must still flag.
	if Fresh(append(rep(0.30, 40), -0.5, -0.5, -0.5)) {
		t.Error("a sustained recent slump should be flagged as cooling")
	}
}

func TestFresh_TooFewSamples(t *testing.T) {
	// Below the sample floor we lack evidence -> treat as fresh.
	if !Fresh([]float64{-0.9, -0.9, -0.9}) {
		t.Error("too-few-samples should default to fresh")
	}
}

func TestFresh_ZeroVariance(t *testing.T) {
	// Constant returns -> zero variance -> fresh (nothing to detect).
	if !Fresh(rep(0.0, 30)) {
		t.Error("zero-variance series should be fresh")
	}
}

func TestFresh_OldSlumpRecovered(t *testing.T) {
	// An early slump that fully recovered should NOT flag now (CUSUM drained).
	r := rep(-0.5, 10)
	r = append(r, rep(0.4, 60)...)
	if !Fresh(r) {
		t.Error("a recovered wallet should be fresh again")
	}
}

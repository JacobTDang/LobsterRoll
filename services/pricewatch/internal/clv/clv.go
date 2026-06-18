// Package clv computes Closing Line Value: how much a trade's entry price beat
// the market's price near close. CLV is the lowest-variance leading indicator of
// skill in betting markets — beating the closing line predicts long-run profit
// with far fewer samples than realized ROI.
package clv

// CLV returns the closing-line value of a trade, in probability terms, from the
// trader's perspective. A BUY (holding YES) profits when the price rises, so its
// CLV is close - entry; a SELL profits when the price falls, so its CLV is
// entry - close. Positive = beat the close (good); negative = the market moved
// against the entry. Both prices are 0..1 share prices.
func CLV(entry, close float64, buy bool) float64 {
	if buy {
		return close - entry
	}
	return entry - close
}

// Average returns the mean CLV over a set of trades; 0 for an empty set.
func Average(clvs []float64) float64 {
	if len(clvs) == 0 {
		return 0
	}
	var sum float64
	for _, v := range clvs {
		sum += v
	}
	return sum / float64(len(clvs))
}

// BeatRate returns the fraction of trades with strictly positive CLV (beat the
// close); 0 for an empty set.
func BeatRate(clvs []float64) float64 {
	if len(clvs) == 0 {
		return 0
	}
	var beat int
	for _, v := range clvs {
		if v > 0 {
			beat++
		}
	}
	return float64(beat) / float64(len(clvs))
}

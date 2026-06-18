package settle

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/store"
)

type fakeStore struct {
	unsettled []store.Trade
	snaps     map[string]store.Snapshot // token -> snapshot to return; absent => ErrNoSnapshot
	set       map[string]float64        // tx -> clv recorded
}

func (f *fakeStore) UnsettledTrades(context.Context) ([]store.Trade, error) { return f.unsettled, nil }
func (f *fakeStore) Nearest(_ context.Context, token string, _ int64) (store.Snapshot, error) {
	if s, ok := f.snaps[token]; ok {
		return s, nil
	}
	return store.Snapshot{}, store.ErrNoSnapshot
}
func (f *fakeStore) SetTradeCLV(_ context.Context, tx string, _ uint64, _ string, clv float64) error {
	if f.set == nil {
		f.set = map[string]float64{}
	}
	f.set[tx] = clv
	return nil
}

type fakeEnds map[string]int64

func (f fakeEnds) EndDate(_ context.Context, token string) (int64, error) { return f[token], nil }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newSettler(st TradeStore, ends EndDater) *Settler {
	s := New(st, ends, 4*time.Hour, quiet())
	s.now = func() time.Time { return time.Unix(10_000, 0) }
	return s
}

func TestRun_SettlesResolvedTradeWithSnapshot(t *testing.T) {
	st := &fakeStore{
		unsettled: []store.Trade{{Wallet: "w", TokenID: "tok", Tx: "0xa", LogIndex: 1, Entry: 0.40, Buy: true}},
		snaps:     map[string]store.Snapshot{"tok": {TS: 5000, Mid: 0.55}},
	}
	ends := fakeEnds{"tok": 5000} // resolved before now=10000
	newSettler(st, ends).Run(context.Background())

	if got, ok := st.set["0xa"]; !ok {
		t.Fatal("trade should have been settled")
	} else if got < 0.149 || got > 0.151 { // CLV buy = 0.55 - 0.40
		t.Errorf("clv = %v, want ~0.15", got)
	}
}

func TestRun_SkipsUnresolved(t *testing.T) {
	st := &fakeStore{
		unsettled: []store.Trade{{TokenID: "tok", Tx: "0xa", Entry: 0.4, Buy: true}},
		snaps:     map[string]store.Snapshot{"tok": {Mid: 0.55}},
	}
	ends := fakeEnds{"tok": 999_999} // ends in the future
	newSettler(st, ends).Run(context.Background())
	if len(st.set) != 0 {
		t.Errorf("unresolved trade must not settle: %v", st.set)
	}
}

func TestRun_SkipsUnknownEndAndMissingSnapshot(t *testing.T) {
	// Unknown end date (0) -> skip; resolved but no snapshot -> skip.
	st := &fakeStore{
		unsettled: []store.Trade{
			{TokenID: "noend", Tx: "0xa", Entry: 0.4, Buy: true},
			{TokenID: "nosnap", Tx: "0xb", Entry: 0.4, Buy: true},
		},
		snaps: map[string]store.Snapshot{}, // none
	}
	ends := fakeEnds{"noend": 0, "nosnap": 5000}
	newSettler(st, ends).Run(context.Background())
	if len(st.set) != 0 {
		t.Errorf("nothing should settle: %v", st.set)
	}
}

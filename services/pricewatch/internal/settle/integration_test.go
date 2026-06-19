package settle_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/settle"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/store"
)

type stubEnds struct{ end int64 }

func (s stubEnds) EndDate(context.Context, string) (int64, error) { return s.end, nil }

// TestVerifyCLV is the `make verify-clv` harness: it drives the real snapshot
// store through record -> settle -> aggregate and asserts the closing-line value,
// proving the CLV pipeline end-to-end (no network).
func TestVerifyCLV(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "pw.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	const tok, wallet = "tok1", "0xwhale"
	end := int64(1_700_000_000) // past -> settles vs time.Now

	// Bought at 0.40 (buy); captured close 0.55 near end-4h. Expected CLV +0.15.
	if err := st.RecordTrade(ctx, store.Trade{
		Wallet: wallet, TokenID: tok, Tx: "0xabc", LogIndex: 1, Entry: 0.40, Buy: true, ObservedUnix: end - 86_400,
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := st.Put(ctx, tok, end-4*3600, 0.55); err != nil {
		t.Fatalf("put snapshot: %v", err)
	}

	settle.New(st, stubEnds{end: end}, 4*time.Hour, quietLog()).Run(ctx)

	agg, err := st.WalletCLV(ctx, []string{wallet})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	a := agg[wallet]
	t.Logf("entry 0.40 (buy), close 0.55 -> avg CLV %+.4f (n=%d)", a.AvgCLV, a.N)
	if a.N != 1 || a.AvgCLV < 0.149 || a.AvgCLV > 0.151 {
		t.Fatalf("CLV = %+.4f (n=%d), want ~+0.15 (n=1)", a.AvgCLV, a.N)
	}
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

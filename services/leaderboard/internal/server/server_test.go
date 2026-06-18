package server

import (
	"context"
	"net"
	"reflect"
	"testing"
	"time"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type fakeLister struct {
	wallets  []string
	err      error
	lastSync int64
	syncErr  error
	stats    map[string]store.StatsRecord
	statsErr error
}

func (f fakeLister) List(context.Context) ([]string, error) { return f.wallets, f.err }

func (f fakeLister) LastSync(context.Context) (int64, error) { return f.lastSync, f.syncErr }

func (f fakeLister) GetStats(_ context.Context, wallet string) (store.StatsRecord, bool, error) {
	if f.statsErr != nil {
		return store.StatsRecord{}, false, f.statsErr
	}
	rec, ok := f.stats[wallet]
	return rec, ok, nil
}

func newTestClient(t *testing.T, l Lister) (*Server, lobsterrollv1.LeaderboardClient) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := New(l)
	gs := grpc.NewServer()
	lobsterrollv1.RegisterLeaderboardServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return srv, lobsterrollv1.NewLeaderboardClient(conn)
}

func TestGetWatchset(t *testing.T) {
	_, client := newTestClient(t, fakeLister{wallets: []string{"0xa", "0xb"}, lastSync: 12345})

	resp, err := client.GetWatchset(context.Background(), &lobsterrollv1.GetWatchsetRequest{})
	if err != nil {
		t.Fatalf("GetWatchset: %v", err)
	}
	if !reflect.DeepEqual(resp.GetWallets(), []string{"0xa", "0xb"}) {
		t.Fatalf("wallets = %v", resp.GetWallets())
	}
	if resp.GetLastSyncedUnix() != 12345 {
		t.Fatalf("lastSyncedUnix = %d, want 12345", resp.GetLastSyncedUnix())
	}
}

func TestStreamWatchset_EmitsOnChange(t *testing.T) {
	// Empty lister: snapshot is skipped, so the first message is the diff.
	srv, client := newTestClient(t, fakeLister{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.StreamWatchset(ctx, &lobsterrollv1.StreamWatchsetRequest{})
	if err != nil {
		t.Fatalf("StreamWatchset: %v", err)
	}

	// Wait for the server-side subscription to register before broadcasting,
	// so the update isn't sent into the void.
	waitFor(t, func() bool { return srv.subscriberCount() == 1 })

	srv.Broadcast([]string{"0xc"}, []string{"0xa"})

	upd, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if !reflect.DeepEqual(upd.GetAdded(), []string{"0xc"}) {
		t.Errorf("added = %v, want [0xc]", upd.GetAdded())
	}
	if !reflect.DeepEqual(upd.GetRemoved(), []string{"0xa"}) {
		t.Errorf("removed = %v, want [0xa]", upd.GetRemoved())
	}
}

func TestStreamWatchset_SnapshotFirst(t *testing.T) {
	srv, client := newTestClient(t, fakeLister{wallets: []string{"0xa", "0xb"}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.StreamWatchset(ctx, &lobsterrollv1.StreamWatchsetRequest{})
	if err != nil {
		t.Fatalf("StreamWatchset: %v", err)
	}

	// First message must be the snapshot: Added == current set, Removed empty.
	upd, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv snapshot: %v", err)
	}
	if !reflect.DeepEqual(upd.GetAdded(), []string{"0xa", "0xb"}) {
		t.Fatalf("snapshot added = %v, want [0xa 0xb]", upd.GetAdded())
	}
	if len(upd.GetRemoved()) != 0 {
		t.Fatalf("snapshot removed = %v, want empty", upd.GetRemoved())
	}

	waitFor(t, func() bool { return srv.subscriberCount() == 1 })
	srv.Broadcast([]string{"0xc"}, nil)

	upd2, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv diff: %v", err)
	}
	if !reflect.DeepEqual(upd2.GetAdded(), []string{"0xc"}) {
		t.Fatalf("diff added = %v, want [0xc]", upd2.GetAdded())
	}
}

func TestStreamWatchset_UnsubscribesOnDisconnect(t *testing.T) {
	srv, client := newTestClient(t, fakeLister{})

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.StreamWatchset(ctx, &lobsterrollv1.StreamWatchsetRequest{})
	if err != nil {
		t.Fatalf("StreamWatchset: %v", err)
	}
	_ = stream
	waitFor(t, func() bool { return srv.subscriberCount() == 1 })

	cancel() // client disconnects
	waitFor(t, func() bool { return srv.subscriberCount() == 0 })
}

func TestStreamWatchset_OverflowReturnsResourceExhausted(t *testing.T) {
	srv, client := newTestClient(t, fakeLister{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.StreamWatchset(ctx, &lobsterrollv1.StreamWatchsetRequest{})
	if err != nil {
		t.Fatalf("StreamWatchset: %v", err)
	}
	waitFor(t, func() bool { return srv.subscriberCount() == 1 })

	// Flood far past the buffer without the client draining. gRPC may pull a
	// couple messages into its send window, so overshoot generously.
	for i := 0; i < subBuffer*4+8; i++ {
		srv.Broadcast([]string{"0x1"}, nil)
	}

	// The client should eventually observe a ResourceExhausted error telling it
	// to reconnect and re-sync.
	for {
		_, err := stream.Recv()
		if err == nil {
			continue
		}
		if status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("Recv err = %v, want ResourceExhausted", err)
		}
		break
	}
}

// TestBroadcast_OverflowRemovesSubscriberAndClosesLost is a package-internal
// test: it fills a subscriber's buffer beyond capacity via Broadcast without
// draining, then asserts the subscriber was removed and its lost channel closed.
func TestBroadcast_OverflowRemovesSubscriberAndClosesLost(t *testing.T) {
	srv := New(fakeLister{})
	sub, _ := srv.subscribe()
	if srv.subscriberCount() != 1 {
		t.Fatalf("subscriberCount = %d, want 1", srv.subscriberCount())
	}

	// Never drain sub.ch; broadcast past capacity to trigger overflow eviction.
	for i := 0; i < subBuffer+2; i++ {
		srv.Broadcast([]string{"0x1"}, nil)
	}

	if srv.subscriberCount() != 0 {
		t.Fatalf("subscriberCount = %d after overflow, want 0", srv.subscriberCount())
	}

	select {
	case <-sub.lost:
		// closed as expected
	default:
		t.Fatal("lost channel not closed after overflow")
	}
}

func TestBroadcast_DoesNotAliasCallerSlices(t *testing.T) {
	srv := New(fakeLister{})
	sub, _ := srv.subscribe()

	added := []string{"0xa"}
	removed := []string{"0xb"}
	srv.Broadcast(added, removed)

	// Mutate the caller's slices after Broadcast returns.
	added[0] = "MUTATED"
	removed[0] = "MUTATED"

	select {
	case upd := <-sub.ch:
		if !reflect.DeepEqual(upd.GetAdded(), []string{"0xa"}) {
			t.Fatalf("added aliased caller slice: %v", upd.GetAdded())
		}
		if !reflect.DeepEqual(upd.GetRemoved(), []string{"0xb"}) {
			t.Fatalf("removed aliased caller slice: %v", upd.GetRemoved())
		}
	default:
		t.Fatal("no update delivered")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}

func TestGetWalletStats_Found(t *testing.T) {
	const w = "0xf0318c32136c2db7fec88b84869aee6a1106c80c"
	rec := store.StatsRecord{
		Wallet: w, WinRate: 0.65, ResolvedMarkets: 29, RealizedPnL: 31_000_000,
		Profit30D: 1234.5, PortfolioValue: 999.9, TradedMarkets: 40, ComputedUnix: 1700000000,
	}
	_, client := newTestClient(t, fakeLister{stats: map[string]store.StatsRecord{w: rec}})

	// Request with mixed-case wallet to prove normalization before lookup.
	resp, err := client.GetWalletStats(context.Background(),
		&lobsterrollv1.GetWalletStatsRequest{Wallet: "0xF0318C32136C2DB7FEC88B84869AEE6A1106C80C"})
	if err != nil {
		t.Fatalf("GetWalletStats: %v", err)
	}
	if !resp.GetFound() {
		t.Fatal("Found = false, want true")
	}
	if resp.GetWallet() != w || resp.GetWinRate() != 0.65 || resp.GetResolvedMarkets() != 29 {
		t.Errorf("resp = %+v", resp)
	}
	if resp.GetRealizedPnl() != 31_000_000 || resp.GetProfit_30D() != 1234.5 ||
		resp.GetPortfolioValue() != 999.9 || resp.GetTradedMarkets() != 40 ||
		resp.GetComputedUnix() != 1700000000 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestGetWalletStats_NotFound(t *testing.T) {
	const w = "0xf0318c32136c2db7fec88b84869aee6a1106c80c"
	_, client := newTestClient(t, fakeLister{stats: map[string]store.StatsRecord{}})
	resp, err := client.GetWalletStats(context.Background(),
		&lobsterrollv1.GetWalletStatsRequest{Wallet: w})
	if err != nil {
		t.Fatalf("GetWalletStats: %v", err)
	}
	if resp.GetFound() {
		t.Fatal("Found = true, want false for unknown wallet")
	}
	if resp.GetWallet() != w {
		t.Errorf("Wallet = %q, want echoed %q", resp.GetWallet(), w)
	}
}

func TestGetWalletStats_InvalidWallet(t *testing.T) {
	_, client := newTestClient(t, fakeLister{})
	_, err := client.GetWalletStats(context.Background(),
		&lobsterrollv1.GetWalletStatsRequest{Wallet: "not-an-address"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
}

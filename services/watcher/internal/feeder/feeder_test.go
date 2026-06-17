package feeder

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/watchset"
	"github.com/ethereum/go-ethereum/common"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// testServer is a minimal, controllable Leaderboard server for the feeder tests.
type testServer struct {
	lobsterrollv1.UnimplementedLeaderboardServer
	wallets []string
	updates chan *lobsterrollv1.WatchsetUpdate // streamed until closed
}

func (s *testServer) GetWatchset(context.Context, *lobsterrollv1.GetWatchsetRequest) (*lobsterrollv1.GetWatchsetResponse, error) {
	return &lobsterrollv1.GetWatchsetResponse{Wallets: s.wallets, LastSyncedUnix: 1700000000}, nil
}

func (s *testServer) StreamWatchset(_ *lobsterrollv1.StreamWatchsetRequest, stream lobsterrollv1.Leaderboard_StreamWatchsetServer) error {
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case upd, ok := <-s.updates:
			if !ok {
				return nil
			}
			if err := stream.Send(upd); err != nil {
				return err
			}
		}
	}
}

func dial(t *testing.T, srv *testServer) lobsterrollv1.LeaderboardClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	lobsterrollv1.RegisterLeaderboardServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return lobsterrollv1.NewLeaderboardClient(conn)
}

const (
	addrA = "0x037c0f46600702e77ccb738721a78d6418d3a458"
	addrB = "0xa6d24a207011c9a5d54fa3a04f3e87365d2e12f4"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSnapshot(t *testing.T) {
	client := dial(t, &testServer{wallets: []string{addrA, addrB}})
	set := watchset.New()
	f := New(client, set, quiet())

	if err := f.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !set.Has(common.HexToAddress(addrA)) || !set.Has(common.HexToAddress(addrB)) {
		t.Fatalf("snapshot did not populate set (len=%d)", set.Len())
	}
}

func TestStream_AppliesDiffs(t *testing.T) {
	updates := make(chan *lobsterrollv1.WatchsetUpdate, 2)
	client := dial(t, &testServer{updates: updates})
	set := watchset.New()
	set.Apply([]string{addrA}, nil) // start with A
	f := New(client, set, quiet())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.Stream(ctx) }()

	// Push a diff: add B, remove A.
	updates <- &lobsterrollv1.WatchsetUpdate{Added: []string{addrB}, Removed: []string{addrA}}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if set.Has(common.HexToAddress(addrB)) && !set.Has(common.HexToAddress(addrA)) {
			cancel()
			<-done
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("diff not applied: hasA=%v hasB=%v", set.Has(common.HexToAddress(addrA)), set.Has(common.HexToAddress(addrB)))
}

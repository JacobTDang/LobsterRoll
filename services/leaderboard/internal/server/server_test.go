package server

import (
	"context"
	"net"
	"reflect"
	"testing"
	"time"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type fakeLister struct {
	wallets []string
	err     error
}

func (f fakeLister) List(context.Context) ([]string, error) { return f.wallets, f.err }

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
	_, client := newTestClient(t, fakeLister{wallets: []string{"0xa", "0xb"}})

	resp, err := client.GetWatchset(context.Background(), &lobsterrollv1.GetWatchsetRequest{})
	if err != nil {
		t.Fatalf("GetWatchset: %v", err)
	}
	if !reflect.DeepEqual(resp.GetWallets(), []string{"0xa", "0xb"}) {
		t.Fatalf("wallets = %v", resp.GetWallets())
	}
}

func TestStreamWatchset_EmitsOnChange(t *testing.T) {
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

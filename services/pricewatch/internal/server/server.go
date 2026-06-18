// Package server exposes per-wallet CLV aggregates over gRPC for the leaderboard.
package server

import (
	"context"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/store"
)

// CLVStore is the store subset the server reads.
type CLVStore interface {
	WalletCLV(ctx context.Context, wallets []string) (map[string]store.CLVAgg, error)
}

// Server implements the Pricewatch gRPC service.
type Server struct {
	lobsterrollv1.UnimplementedPricewatchServer
	store CLVStore
}

// New constructs a Server.
func New(s CLVStore) *Server { return &Server{store: s} }

// GetWalletCLV returns the settled-CLV aggregate for each requested wallet that
// has at least one settled trade (absent wallets are simply omitted).
func (s *Server) GetWalletCLV(ctx context.Context, req *lobsterrollv1.GetWalletCLVRequest) (*lobsterrollv1.GetWalletCLVResponse, error) {
	agg, err := s.store.WalletCLV(ctx, req.GetWallets())
	if err != nil {
		return nil, err
	}
	resp := &lobsterrollv1.GetWalletCLVResponse{Clv: make([]*lobsterrollv1.WalletCLV, 0, len(agg))}
	for w, a := range agg {
		resp.Clv = append(resp.Clv, &lobsterrollv1.WalletCLV{Wallet: w, AvgClv: a.AvgCLV, N: int64(a.N)})
	}
	return resp, nil
}

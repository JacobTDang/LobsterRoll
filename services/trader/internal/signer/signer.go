// Package signer builds and EIP-712-signs an order from a proposal and renders
// the CLOB wire payload.
package signer

import (
	"crypto/ecdsa"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/clob"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/order"
)

// Signer signs orders for one exchange with one key.
type Signer struct {
	key      *ecdsa.PrivateKey
	maker    common.Address
	signer   common.Address
	exchange common.Address
	sigType  uint8
	now      func() time.Time
}

// New builds a Signer from a hex private key (no 0x), the maker (funds source;
// empty = the signer's own address), the exchange (verifying contract), and the
// signature type.
func New(privHex, makerAddr, exchangeAddr string, sigType uint8) (*Signer, error) {
	key, err := crypto.HexToECDSA(strings.TrimPrefix(privHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	signerAddr := crypto.PubkeyToAddress(key.PublicKey)
	maker := signerAddr
	if makerAddr != "" {
		maker = common.HexToAddress(makerAddr)
	}
	return &Signer{
		key: key, maker: maker, signer: signerAddr,
		exchange: common.HexToAddress(exchangeAddr), sigType: sigType, now: time.Now,
	}, nil
}

// Sign builds the order from the proposal, signs it, and returns the CLOB payload.
func (s *Signer) Sign(p bus.OrderProposal) (clob.SignedOrder, error) {
	now := s.now()
	o, err := order.FromProposal(p, s.maker, s.signer, s.sigType, now.UnixNano(), now.UnixMilli())
	if err != nil {
		return clob.SignedOrder{}, err
	}
	sig, err := o.Sign(s.exchange, s.key)
	if err != nil {
		return clob.SignedOrder{}, err
	}
	side := "BUY"
	if o.Side == order.SideSell {
		side = "SELL"
	}
	return clob.SignedOrder{
		Salt:          o.Salt.String(),
		Maker:         o.Maker.Hex(),
		Signer:        o.Signer.Hex(),
		TokenID:       o.TokenID.String(),
		MakerAmount:   o.MakerAmount.String(),
		TakerAmount:   o.TakerAmount.String(),
		Side:          side,
		SignatureType: int(o.SignatureType),
		Timestamp:     o.Timestamp.String(),
		Metadata:      hexutil.Encode(o.Metadata[:]),
		Builder:       hexutil.Encode(o.Builder[:]),
		Signature:     hexutil.Encode(sig),
	}, nil
}

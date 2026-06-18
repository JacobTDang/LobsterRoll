// Package order builds and EIP-712-signs Polymarket CTF Exchange orders.
//
// The domain (name "Polymarket CTF Exchange", version "2") and the 11-field
// Order type are taken from the exchange's verified source; ORDER_TYPEHASH and
// the domain separators are asserted against those values in tests, so a signed
// order is correct by construction (not by a circular golden).
package order

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

// EIP-712 domain.
const (
	DomainName    = "Polymarket CTF Exchange"
	DomainVersion = "2"
	ChainID       = 137
)

// orderTypeHash = keccak256 of the Order type string (verified == on-chain constant).
const orderTypeString = "Order(uint256 salt,address maker,address signer,uint256 tokenId,uint256 makerAmount,uint256 takerAmount,uint8 side,uint8 signatureType,uint256 timestamp,bytes32 metadata,bytes32 builder)"

// Side / SignatureType enums (match the exchange).
const (
	SideBuy  uint8 = 0
	SideSell uint8 = 1

	SigEOA        uint8 = 0
	SigPolyProxy  uint8 = 1
	SigPolyGnosis uint8 = 2
)

// Order is the EIP-712 order payload.
type Order struct {
	Salt          *big.Int
	Maker         common.Address
	Signer        common.Address
	TokenID       *big.Int
	MakerAmount   *big.Int
	TakerAmount   *big.Int
	Side          uint8
	SignatureType uint8
	Timestamp     *big.Int
	Metadata      [32]byte
	Builder       [32]byte
}

func keccak(b ...[]byte) []byte { return crypto.Keccak256(b...) }

func pad32(b []byte) []byte {
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

func uintWord(n *big.Int) []byte { return pad32(n.Bytes()) }
func u8Word(n uint8) []byte      { return pad32([]byte{n}) }
func addrWord(a common.Address) []byte { return pad32(a.Bytes()) }

// OrderTypeHash returns keccak256(orderTypeString).
func OrderTypeHash() []byte { return keccak([]byte(orderTypeString)) }

// DomainSeparator returns the EIP-712 domain separator for verifyingContract.
func DomainSeparator(verifyingContract common.Address) []byte {
	typeHash := keccak([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	return keccak(
		typeHash,
		keccak([]byte(DomainName)),
		keccak([]byte(DomainVersion)),
		uintWord(big.NewInt(ChainID)),
		addrWord(verifyingContract),
	)
}

// structHash returns keccak256(ORDER_TYPEHASH || encoded fields).
func (o Order) structHash() []byte {
	return keccak(
		OrderTypeHash(),
		uintWord(o.Salt),
		addrWord(o.Maker),
		addrWord(o.Signer),
		uintWord(o.TokenID),
		uintWord(o.MakerAmount),
		uintWord(o.TakerAmount),
		u8Word(o.Side),
		u8Word(o.SignatureType),
		uintWord(o.Timestamp),
		o.Metadata[:],
		o.Builder[:],
	)
}

// Digest returns the EIP-712 signing digest for the order on verifyingContract.
func (o Order) Digest(verifyingContract common.Address) []byte {
	return keccak([]byte{0x19, 0x01}, DomainSeparator(verifyingContract), o.structHash())
}

// Sign returns the 65-byte EIP-712 signature (r||s||v) with v in {27,28}.
func (o Order) Sign(verifyingContract common.Address, key *ecdsa.PrivateKey) ([]byte, error) {
	sig, err := crypto.Sign(o.Digest(verifyingContract), key)
	if err != nil {
		return nil, fmt.Errorf("sign order: %w", err)
	}
	sig[64] += 27 // go-ethereum returns v in {0,1}; EIP-712 expects {27,28}
	return sig, nil
}

// scale6 converts a USD/share float to a 6-decimal integer amount via big.Float
// so large sizes can't overflow int64.
func scale6(v float64) *big.Int {
	f := new(big.Float).Mul(big.NewFloat(v), big.NewFloat(1e6))
	i, _ := f.Int(nil)
	return i
}

// FromProposal builds an unsigned Order mirroring a proposal. maker is the funds
// source (proxy wallet), signer is the EOA. salt/ts are caller-provided so the
// result is deterministic and testable.
func FromProposal(p bus.OrderProposal, maker, signer common.Address, sigType uint8, salt, timestampMs int64) (Order, error) {
	price, err := strconv.ParseFloat(p.LimitPrice, 64)
	if err != nil || price <= 0 {
		return Order{}, fmt.Errorf("invalid limit price %q", p.LimitPrice)
	}
	tokenID, ok := new(big.Int).SetString(p.TokenID, 10)
	if !ok {
		return Order{}, fmt.Errorf("invalid token id %q", p.TokenID)
	}
	if p.SizeUSD <= 0 {
		return Order{}, fmt.Errorf("invalid size %v", p.SizeUSD)
	}

	usdc := scale6(p.SizeUSD)
	shares := scale6(p.SizeUSD / price)

	o := Order{
		Salt:          big.NewInt(salt),
		Maker:         maker,
		Signer:        signer,
		TokenID:       tokenID,
		Side:          SideBuy,
		SignatureType: sigType,
		Timestamp:     big.NewInt(timestampMs),
	}
	switch p.Side {
	case "buy":
		o.Side = SideBuy
		o.MakerAmount, o.TakerAmount = usdc, shares // pay USDC, receive shares
	case "sell":
		o.Side = SideSell
		o.MakerAmount, o.TakerAmount = shares, usdc // give shares, receive USDC
	default:
		return Order{}, fmt.Errorf("unknown side %q", p.Side)
	}
	return o, nil
}

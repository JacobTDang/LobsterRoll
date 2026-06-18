package order

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

const (
	ctfExchange     = "0xe111180000d2663c0091e4f400237545b87b996b"
	negRiskExchange = "0xc5d563a36ae78145c45a50134d48a1215220f80a"
	// Well-known hardhat test key (NOT a real wallet) -> 0xf39Fd6...92266.
	testKeyHex  = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	testKeyAddr = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
)

// Anchors: these must match the values verified against the exchange's source.
func TestConstants_MatchVerifiedSource(t *testing.T) {
	if got := "0x" + hex.EncodeToString(OrderTypeHash()); got != "0xbb86318a2138f5fa8ae32fbe8e659f8fcf13cc6ae4014a707893055433818589" {
		t.Errorf("ORDER_TYPEHASH = %s", got)
	}
	if got := "0x" + hex.EncodeToString(DomainSeparator(common.HexToAddress(ctfExchange))); got != "0x3264e159346253e26a64e00b69032db0e7d32f94628de3e6eecb50304d7af3d2" {
		t.Errorf("CTF domainSeparator = %s", got)
	}
	if got := "0x" + hex.EncodeToString(DomainSeparator(common.HexToAddress(negRiskExchange))); got != "0x4d8bb53b87f65b1b2d04cb3da570cd24d6f9f578ef2fdd0ce53db5bc0880f9df" {
		t.Errorf("NegRisk domainSeparator = %s", got)
	}
}

func TestFromProposal_Amounts(t *testing.T) {
	// BUY $25 @ 0.50 -> pay 25 USDC, receive 50 shares.
	buy, err := FromProposal(bus.OrderProposal{TokenID: "123", Side: "buy", LimitPrice: "0.50", SizeUSD: 25},
		common.HexToAddress(testKeyAddr), common.HexToAddress(testKeyAddr), SigEOA, 1, 1000)
	if err != nil {
		t.Fatalf("buy: %v", err)
	}
	if buy.Side != SideBuy || buy.MakerAmount.Cmp(big.NewInt(25_000_000)) != 0 || buy.TakerAmount.Cmp(big.NewInt(50_000_000)) != 0 {
		t.Fatalf("buy amounts = maker %s taker %s side %d", buy.MakerAmount, buy.TakerAmount, buy.Side)
	}
	// SELL $25 @ 0.50 -> give 50 shares, receive 25 USDC.
	sell, _ := FromProposal(bus.OrderProposal{TokenID: "123", Side: "sell", LimitPrice: "0.50", SizeUSD: 25},
		common.HexToAddress(testKeyAddr), common.HexToAddress(testKeyAddr), SigEOA, 1, 1000)
	if sell.Side != SideSell || sell.MakerAmount.Cmp(big.NewInt(50_000_000)) != 0 || sell.TakerAmount.Cmp(big.NewInt(25_000_000)) != 0 {
		t.Fatalf("sell amounts = maker %s taker %s side %d", sell.MakerAmount, sell.TakerAmount, sell.Side)
	}
}

func TestFromProposal_Invalid(t *testing.T) {
	a := common.HexToAddress(testKeyAddr)
	for _, p := range []bus.OrderProposal{
		{TokenID: "1", Side: "buy", LimitPrice: "0", SizeUSD: 10},
		{TokenID: "x", Side: "buy", LimitPrice: "0.5", SizeUSD: 10},
		{TokenID: "1", Side: "buy", LimitPrice: "0.5", SizeUSD: 0},
		{TokenID: "1", Side: "hold", LimitPrice: "0.5", SizeUSD: 10},
	} {
		if _, err := FromProposal(p, a, a, SigEOA, 1, 1); err == nil {
			t.Errorf("expected error for %+v", p)
		}
	}
}

func TestSign_DeterministicAndRecoverable(t *testing.T) {
	key, err := crypto.HexToECDSA(testKeyHex)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	exch := common.HexToAddress(ctfExchange)
	o, _ := FromProposal(bus.OrderProposal{TokenID: "123", Side: "buy", LimitPrice: "0.50", SizeUSD: 25},
		common.HexToAddress(testKeyAddr), common.HexToAddress(testKeyAddr), SigEOA, 42, 1700000000000)

	sig1, err := o.Sign(exch, key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sig2, _ := o.Sign(exch, key)
	if !bytes.Equal(sig1, sig2) {
		t.Fatal("signature not deterministic")
	}
	if len(sig1) != 65 || (sig1[64] != 27 && sig1[64] != 28) {
		t.Fatalf("bad sig shape: len=%d v=%d", len(sig1), sig1[64])
	}

	// Recover the signer from the digest+signature; must be the test key's address.
	digest := o.Digest(exch)
	recovered := sig1[:64] // pubkey recovery needs v in {0,1}
	rv := make([]byte, 65)
	copy(rv, recovered)
	rv[64] = sig1[64] - 27
	pub, err := crypto.SigToPub(digest, rv)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if got := crypto.PubkeyToAddress(*pub); got != common.HexToAddress(testKeyAddr) {
		t.Fatalf("recovered %s, want %s", got.Hex(), testKeyAddr)
	}
}

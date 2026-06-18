// Package chain holds Polygon / Polymarket on-chain constants and log-decoding
// helpers for the watcher.
package chain

// CTF Exchange contract addresses on Polygon that emit OrderFilled (verified
// live against Sourcify + on-chain logs). Addresses are lowercase so they
// compare cleanly against normalized maker/taker addresses.
const (
	// CTFExchange is the current Polymarket CTF Exchange (new OrderFilled ABI).
	CTFExchange = "0xe111180000d2663c0091e4f400237545b87b996b"
	// NegRiskCTFExchange is the neg-risk variant; same new OrderFilled ABI.
	NegRiskCTFExchange = "0xc5d563a36ae78145c45a50134d48a1215220f80a"
	// CTFExchangeLegacy is the original exchange (legacy 8-field OrderFilled ABI),
	// now largely idle but still watched for residual flow.
	CTFExchangeLegacy = "0x4bfb41d5b3570defd03c39a9a4d8de6bd8b8982e"
)

// OrderFilled event topic0 hashes (keccak256 of the event signature). Verified
// in contracts_test.go against the real signatures.
const (
	// OrderFilledTopic is the current event emitted by CTFExchange/NegRiskCTFExchange:
	// OrderFilled(bytes32,address,address,uint8,uint256,uint256,uint256,uint256,bytes32,bytes32)
	OrderFilledTopic = "0xd543adfd945773f1a62f74f0ee55a5e3b9b1a28262980ba90b1a89f2ea84d8ee"
	// OrderFilledTopicLegacy is the legacy event emitted by CTFExchangeLegacy:
	// OrderFilled(bytes32,address,address,uint256,uint256,uint256,uint256,uint256)
	OrderFilledTopicLegacy = "0xd0a08e8c493f9c94f29311604c9de1b4e8c8d4c06bd0c789af57f2d65bfec0f6"
)

// NewABIExchanges returns the exchanges that emit the current OrderFilled ABI.
func NewABIExchanges() []string {
	return []string{CTFExchange, NegRiskCTFExchange}
}

// WatchedExchanges returns every exchange contract to subscribe to.
func WatchedExchanges() []string {
	return []string{CTFExchange, NegRiskCTFExchange, CTFExchangeLegacy}
}

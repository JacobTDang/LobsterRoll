// Package chain holds Polygon / Polymarket on-chain constants and (later)
// log-decoding helpers for the watcher.
package chain

// PolygonChainID is the Polygon mainnet chain id.
const PolygonChainID = 137

// CTF Exchange contract addresses on Polygon that emit OrderFilled.
// Both must be watched: V2 is current, V1 still carries historical/residual flow.
const (
	CTFExchangeV1 = "0x4bFb41d5B3570DeFd03C39a9A4D8dE6Bd8B8982E"
	CTFExchangeV2 = "0xE111180000d2663C0091e4f400237545B87B996B"
)

// WatchedExchanges returns the exchange contract addresses to subscribe to.
func WatchedExchanges() []string {
	return []string{CTFExchangeV1, CTFExchangeV2}
}

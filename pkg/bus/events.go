package bus

import "time"

// TradeDetected is published on SubjectTradeDetected when a watched wallet's
// OrderFilled event is observed on-chain.
type TradeDetected struct {
	Wallet      string    `json:"wallet"`
	TokenID     string    `json:"token_id"`
	Side        string    `json:"side"` // "buy" | "sell"
	Price       string    `json:"price"`
	Size        string    `json:"size"`
	TxHash      string    `json:"tx_hash"`
	LogIndex    uint64    `json:"log_index"`
	BlockNumber uint64    `json:"block_number"`
	ObservedAt  time.Time `json:"observed_at"`
}

// OrderProposal is published on SubjectOrderProposed by strategy-svc.
type OrderProposal struct {
	ID          string        `json:"id"`
	SourceTrade TradeDetected `json:"source_trade"`
	TokenID     string        `json:"token_id"`
	Side        string        `json:"side"`
	LimitPrice  string        `json:"limit_price"`
	SizeUSD     float64       `json:"size_usd"`
	Reason      string        `json:"reason"`
}

// OrderDecision is published on SubjectOrderApproved/SubjectOrderRejected. It
// carries the full proposal so the trader can execute an approved order without
// a separate lookup.
type OrderDecision struct {
	Proposal OrderProposal `json:"proposal"`
	Approved bool          `json:"approved"`
	By       string        `json:"by"` // "telegram:<user>" or "auto"
}

// ControlMsg is published on SubjectControlHalt to halt or resume execution
// (the kill switch). trader-svc consumes it.
type ControlMsg struct {
	Halted bool   `json:"halted"`
	By     string `json:"by"`
}

// OrderResult is published on SubjectOrderFilled/SubjectOrderFailed by trader-svc.
type OrderResult struct {
	ProposalID string `json:"proposal_id"`
	OrderID    string `json:"order_id"`
	Filled     bool   `json:"filled"`
	Err        string `json:"err,omitempty"`
}

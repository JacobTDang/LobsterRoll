// Package bus defines the NATS subjects and event payloads that connect the
// services. Subjects are the contract for the async event pipeline.
package bus

// Subjects for the event pipeline:
//
//	trades.detected -> orders.proposed -> orders.approved -> orders.filled
//	                                   \-> orders.rejected  \-> orders.failed
//	control.halt (kill switch) is consumed by trader-svc.
const (
	SubjectTradeDetected = "trades.detected"
	SubjectOrderProposed = "orders.proposed"
	SubjectOrderApproved = "orders.approved"
	SubjectOrderRejected = "orders.rejected"
	SubjectOrderFilled   = "orders.filled"
	SubjectOrderFailed   = "orders.failed"
	SubjectControlHalt   = "control.halt"
)

// AllSubjects returns every subject used on the bus.
func AllSubjects() []string {
	return []string{
		SubjectTradeDetected,
		SubjectOrderProposed,
		SubjectOrderApproved,
		SubjectOrderRejected,
		SubjectOrderFilled,
		SubjectOrderFailed,
		SubjectControlHalt,
	}
}

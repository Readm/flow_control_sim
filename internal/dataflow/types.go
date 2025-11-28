package dataflow

// TransactionID uniquely identifies a transaction.
// It consists of NodeID and TxnID, allowing each node to independently count transactions.
type TransactionID struct {
	NodeID int // Node that created this transaction
	TxnID  int // Transaction ID within the node (monotonically increasing)
}

// MessageID uniquely identifies a message.
// It consists of NodeID and MessageID, allowing each node to independently count messages.
type MessageID struct {
	NodeID    int // Node that created this message
	MessageID int // Message ID within the node (monotonically increasing)
}


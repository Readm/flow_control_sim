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

// Channel represents the physical or virtual channel type for message routing.
// Different protocols may define their own channel types (e.g., CHI: REQ, RSP, DAT, SNP).
type Channel string

const (
	// Generic channel types (can be extended by protocols)
	ChannelREQ Channel = "REQ" // Request channel
	ChannelRSP Channel = "RSP" // Response channel
	ChannelDAT Channel = "DAT" // Data channel
	ChannelSNP Channel = "SNP" // Snoop channel
)

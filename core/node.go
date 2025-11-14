package core

// NodeType represents the CHI protocol node type.
type NodeType string

const (
	NodeTypeRN NodeType = "RN" // Request Node - initiates transactions
	NodeTypeHN NodeType = "HN" // Home Node - manages cache coherence
	NodeTypeSN NodeType = "SN" // Slave Node - provides data
	NodeTypeRT NodeType = "RT" // Routing Node - forwards traffic on ring
)

// Position represents the position of a node in visualization.
type Position struct {
	X, Y float64
}

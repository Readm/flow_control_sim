package main

// NodeType represents the CHI protocol node type
type NodeType string

const (
	NodeTypeRN NodeType = "RN" // Request Node - initiates transactions
	NodeTypeHN NodeType = "HN" // Home Node - manages cache coherence
	NodeTypeSN NodeType = "SN" // Slave Node - provides data
)

// QueueInfo represents queue information for visualization
type QueueInfo struct {
	Name     string `json:"name"`
	Length   int    `json:"length"`
	Capacity int    `json:"capacity"` // -1 means unlimited capacity
}

// Position represents the position of a node in visualization
type Position struct {
	X, Y float64
}

// Node is the base class for Master, Slave, and Relay
// It provides common functionality including visualization support
type Node struct {
	ID       int
	Type     NodeType
	Queues   []QueueInfo
	Position Position
}

// AddQueue adds or updates a queue information
func (n *Node) AddQueue(name string, length, capacity int) {
	for i := range n.Queues {
		if n.Queues[i].Name == name {
			n.Queues[i].Length = length
			n.Queues[i].Capacity = capacity
			return
		}
	}
	n.Queues = append(n.Queues, QueueInfo{
		Name:     name,
		Length:   length,
		Capacity: capacity,
	})
}

// UpdateQueue updates the length of an existing queue
func (n *Node) UpdateQueue(name string, length int) {
	for i := range n.Queues {
		if n.Queues[i].Name == name {
			n.Queues[i].Length = length
			return
		}
	}
}

// GetQueueInfo returns all queue information
func (n *Node) GetQueueInfo() []QueueInfo {
	return n.Queues
}

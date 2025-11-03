package main

// NodeType represents the type of a node
type NodeType string

const (
	NodeTypeMaster NodeType = "master"
	NodeTypeSlave  NodeType = "slave"
	NodeTypeRelay  NodeType = "relay"
)

// QueueInfo represents queue information for visualization
type QueueInfo struct {
	Name     string
	Length   int
	Capacity int // -1 means unlimited capacity
}

// Position represents the position of a node in visualization
type Position struct {
	X, Y float64
}

// Node is the base class for Master, Slave, and Relay
// It provides common functionality including visualization support
type Node struct {
	ID         int
	Type       NodeType
	Queues     []QueueInfo
	Position   Position
	visualizer Visualizer
}

// Visualizer is the interface for visualization implementations
type Visualizer interface {
	UpdateNode(node *Node)
	Render()
	SetHeadless(headless bool)
	IsHeadless() bool
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

// SetVisualizer sets the visualizer for this node
func (n *Node) SetVisualizer(v Visualizer) {
	n.visualizer = v
}

// UpdateVisualization updates the visualization if visualizer is set and not headless
func (n *Node) UpdateVisualization() {
	if n.visualizer != nil && !n.visualizer.IsHeadless() {
		n.visualizer.UpdateNode(n)
	}
}

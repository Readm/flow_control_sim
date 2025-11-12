package main

import "github.com/Readm/flow_sim/core"

// Node is the base class for Master, Slave, and Relay.
// It provides common functionality including visualization support.
type Node struct {
	ID       int
	Type     core.NodeType
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
	n.UpdateQueueState(name, length, -1)
}

// UpdateQueueState updates both length and capacity of an existing queue.
// Pass capacity < 0 to keep the current capacity unchanged.
func (n *Node) UpdateQueueState(name string, length, capacity int) {
	for i := range n.Queues {
		if n.Queues[i].Name == name {
			n.Queues[i].Length = length
			if capacity >= 0 {
				n.Queues[i].Capacity = capacity
			}
			return
		}
	}
}

// GetQueueInfo returns all queue information
func (n *Node) GetQueueInfo() []QueueInfo {
	return n.Queues
}

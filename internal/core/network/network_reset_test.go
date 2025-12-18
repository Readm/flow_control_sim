package network

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

func TestNetworkReset(t *testing.T) {
	t.Parallel()

	net := New()

	// Create initial network with one node
	initialSchema := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID: 0,
				InPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  4,
						OutBandwidth: 4,
					},
				},
				OutPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  2,
						OutBandwidth: 2,
					},
				},
			},
		},
		Edges: []EdgeSchema{},
	}

	// Reset network with initial schema
	if err := net.Reset(initialSchema); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	// Verify node was created
	if len(net.nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(net.nodes))
	}

	node0, ok := net.nodes[0]
	if !ok {
		t.Fatalf("Node 0 not found")
	}

	// Verify input queue configuration
	if len(node0.Inputs) != 1 {
		t.Fatalf("Expected 1 input queue, got %d", len(node0.Inputs))
	}
	if node0.Inputs[0].Capacity() != 16 {
		t.Fatalf("Expected input queue capacity 16, got %d", node0.Inputs[0].Capacity())
	}

	// Verify output queue configuration
	if len(node0.Outputs) != 1 {
		t.Fatalf("Expected 1 output queue, got %d", len(node0.Outputs))
	}
	if node0.Outputs[0].Capacity() != 16 {
		t.Fatalf("Expected output queue capacity 16, got %d", node0.Outputs[0].Capacity())
	}

	// Reset with a different network topology
	newSchema := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID: 1,
				InPorts: []PortSchema{
					{
						BufferSize:   32,
						InBandwidth:  8,
						OutBandwidth: 8,
					},
				},
				OutPorts: []PortSchema{
					{
						BufferSize:   32,
						InBandwidth:  4,
						OutBandwidth: 4,
					},
				},
			},
			{
				NodeID: 2,
				InPorts: []PortSchema{
					{
						BufferSize:   32,
						InBandwidth:  8,
						OutBandwidth: 8,
					},
				},
				OutPorts: []PortSchema{},
			},
		},
		Edges: []EdgeSchema{
			{
				EdgeID:    0,
				SrcNodeID: 1,
				SrcPortID: 0,
				DstNodeID: 2,
				DstPortID: 0,
			},
		},
	}

	// Reset network with new schema
	if err := net.Reset(newSchema); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	// Verify old node is gone
	if _, ok := net.nodes[0]; ok {
		t.Fatalf("Node 0 should have been removed")
	}

	// Verify new nodes were created
	if len(net.nodes) != 2 {
		t.Fatalf("Expected 2 nodes, got %d", len(net.nodes))
	}

	node1, ok := net.nodes[1]
	if !ok {
		t.Fatalf("Node 1 not found")
	}
	node2, ok := net.nodes[2]
	if !ok {
		t.Fatalf("Node 2 not found")
	}

	// Verify node 1 configuration
	if node1.Inputs[0].Capacity() != 32 {
		t.Fatalf("Expected node 1 input queue capacity 32, got %d", node1.Inputs[0].Capacity())
	}
	if node1.Outputs[0].Capacity() != 32 {
		t.Fatalf("Expected node 1 output queue capacity 32, got %d", node1.Outputs[0].Capacity())
	}

	// Verify node 2 configuration
	if node2.Inputs[0].Capacity() != 32 {
		t.Fatalf("Expected node 2 input queue capacity 32, got %d", node2.Inputs[0].Capacity())
	}
	if len(node2.Outputs) != 0 {
		t.Fatalf("Expected node 2 to have 0 output queues, got %d", len(node2.Outputs))
	}

	// Verify link was created
	if len(net.links) != 1 {
		t.Fatalf("Expected 1 link, got %d", len(net.links))
	}

	link := net.links[0]
	if link.SourceID() != 1 || link.TargetID() != 2 {
		t.Fatalf("Expected link 1->2, got %d->%d", link.SourceID(), link.TargetID())
	}
}

func TestNetworkResetWithZeroBandwidth(t *testing.T) {
	t.Parallel()

	net := New()

	// Test with zero inBandwidth - should return error
	schema1 := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID: 0,
				InPorts: []PortSchema{
					{
						BufferSize:   8,
						InBandwidth:  0, // Should cause error
						OutBandwidth: 4,
					},
				},
				OutPorts: []PortSchema{},
			},
		},
		Edges: []EdgeSchema{},
	}

	if err := net.Reset(schema1); err == nil {
		t.Fatalf("Expected error for zero inBandwidth, got nil")
	}

	// Test with zero outBandwidth - should return error
	schema2 := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID: 0,
				InPorts: []PortSchema{
					{
						BufferSize:   8,
						InBandwidth:  4,
						OutBandwidth: 0, // Should cause error
					},
				},
				OutPorts: []PortSchema{},
			},
		},
		Edges: []EdgeSchema{},
	}

	if err := net.Reset(schema2); err == nil {
		t.Fatalf("Expected error for zero outBandwidth, got nil")
	}

	// Test with zero bufferSize - should default to 8, but bandwidth must be positive
	schema3 := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID: 0,
				InPorts: []PortSchema{
					{
						BufferSize:   0, // Should default to 8
						InBandwidth:  4, // Must be positive
						OutBandwidth: 4, // Must be positive
					},
				},
				OutPorts: []PortSchema{
					{
						BufferSize:   0, // Should default to 8
						InBandwidth:  2, // Must be positive
						OutBandwidth: 2, // Must be positive
					},
				},
			},
		},
		Edges: []EdgeSchema{},
	}

	if err := net.Reset(schema3); err != nil {
		t.Fatalf("Reset failed with valid bandwidth but zero bufferSize: %v", err)
	}

	node0 := net.nodes[0]
	// Verify bufferSize default was applied
	if node0.Inputs[0].Capacity() != 8 {
		t.Fatalf("Expected default input queue capacity 8, got %d", node0.Inputs[0].Capacity())
	}
	if node0.Outputs[0].Capacity() != 8 {
		t.Fatalf("Expected default output queue capacity 8, got %d", node0.Outputs[0].Capacity())
	}
}

func TestNetworkResetWithMultiplePorts(t *testing.T) {
	t.Parallel()

	net := New()

	schema := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID: 0,
				InPorts: []PortSchema{
					{
						BufferSize:   10,
						InBandwidth:  5,
						OutBandwidth: 5,
					},
					{
						BufferSize:   20,
						InBandwidth:  10,
						OutBandwidth: 10,
					},
				},
				OutPorts: []PortSchema{
					{
						BufferSize:   15,
						InBandwidth:  3,
						OutBandwidth: 3,
					},
					{
						BufferSize:   25,
						InBandwidth:  5,
						OutBandwidth: 5,
					},
				},
			},
		},
		Edges: []EdgeSchema{},
	}

	if err := net.Reset(schema); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	node0 := net.nodes[0]
	if len(node0.Inputs) != 2 {
		t.Fatalf("Expected 2 input queues, got %d", len(node0.Inputs))
	}
	if len(node0.Outputs) != 2 {
		t.Fatalf("Expected 2 output queues, got %d", len(node0.Outputs))
	}

	// Verify first input queue
	if node0.Inputs[0].Capacity() != 10 {
		t.Fatalf("Expected first input queue capacity 10, got %d", node0.Inputs[0].Capacity())
	}

	// Verify second input queue
	if node0.Inputs[1].Capacity() != 20 {
		t.Fatalf("Expected second input queue capacity 20, got %d", node0.Inputs[1].Capacity())
	}

	// Verify first output queue
	if node0.Outputs[0].Capacity() != 15 {
		t.Fatalf("Expected first output queue capacity 15, got %d", node0.Outputs[0].Capacity())
	}

	// Verify second output queue
	if node0.Outputs[1].Capacity() != 25 {
		t.Fatalf("Expected second output queue capacity 25, got %d", node0.Outputs[1].Capacity())
	}
}

func TestNetworkResetWithEdges(t *testing.T) {
	t.Parallel()

	net := New()

	schema := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID:  0,
				InPorts: []PortSchema{},
				OutPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  2,
						OutBandwidth: 2,
					},
				},
			},
			{
				NodeID: 1,
				InPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  2,
						OutBandwidth: 2,
					},
				},
				OutPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  2,
						OutBandwidth: 2,
					},
				},
			},
			{
				NodeID: 2,
				InPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  2,
						OutBandwidth: 2,
					},
				},
				OutPorts: []PortSchema{},
			},
		},
		Edges: []EdgeSchema{
			{
				EdgeID:    0,
				SrcNodeID: 0,
				SrcPortID: 0,
				DstNodeID: 1,
				DstPortID: 0,
			},
			{
				EdgeID:    1,
				SrcNodeID: 1,
				SrcPortID: 0,
				DstNodeID: 2,
				DstPortID: 0,
			},
		},
	}

	if err := net.Reset(schema); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	// Verify all nodes were created
	if len(net.nodes) != 3 {
		t.Fatalf("Expected 3 nodes, got %d", len(net.nodes))
	}

	// Verify all links were created
	if len(net.links) != 2 {
		t.Fatalf("Expected 2 links, got %d", len(net.links))
	}

	// Verify link 0->1
	link0 := net.links[0]
	if link0.SourceID() != 0 || link0.TargetID() != 1 {
		t.Fatalf("Expected first link 0->1, got %d->%d", link0.SourceID(), link0.TargetID())
	}

	// Verify link 1->2
	link1 := net.links[1]
	if link1.SourceID() != 1 || link1.TargetID() != 2 {
		t.Fatalf("Expected second link 1->2, got %d->%d", link1.SourceID(), link1.TargetID())
	}
}

func TestNetworkResetErrorCases(t *testing.T) {
	t.Parallel()

	net := New()

	// Test nil schema
	if err := net.Reset(nil); err == nil {
		t.Fatalf("Expected error for nil schema, got nil")
	}

	// Test invalid source node in edge
	schema1 := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID:   0,
				InPorts:  []PortSchema{},
				OutPorts: []PortSchema{},
			},
		},
		Edges: []EdgeSchema{
			{
				EdgeID:    0,
				SrcNodeID: 99, // Non-existent node
				SrcPortID: 0,
				DstNodeID: 0,
				DstPortID: 0,
			},
		},
	}
	if err := net.Reset(schema1); err == nil {
		t.Fatalf("Expected error for invalid source node, got nil")
	}

	// Test invalid target node in edge
	schema2 := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID: 0,
				InPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  2,
						OutBandwidth: 2,
					},
				},
				OutPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  2,
						OutBandwidth: 2,
					},
				},
			},
		},
		Edges: []EdgeSchema{
			{
				EdgeID:    0,
				SrcNodeID: 0,
				SrcPortID: 0,
				DstNodeID: 99, // Non-existent node
				DstPortID: 0,
			},
		},
	}
	if err := net.Reset(schema2); err == nil {
		t.Fatalf("Expected error for invalid target node, got nil")
	}

	// Test invalid source port index
	schema3 := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID:  0,
				InPorts: []PortSchema{},
				OutPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  2,
						OutBandwidth: 2,
					},
				},
			},
			{
				NodeID: 1,
				InPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  2,
						OutBandwidth: 2,
					},
				},
				OutPorts: []PortSchema{},
			},
		},
		Edges: []EdgeSchema{
			{
				EdgeID:    0,
				SrcNodeID: 0,
				SrcPortID: 99, // Invalid port index
				DstNodeID: 1,
				DstPortID: 0,
			},
		},
	}
	if err := net.Reset(schema3); err == nil {
		t.Fatalf("Expected error for invalid source port index, got nil")
	}

	// Test invalid target port index
	schema4 := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID:  0,
				InPorts: []PortSchema{},
				OutPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  2,
						OutBandwidth: 2,
					},
				},
			},
			{
				NodeID: 1,
				InPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  2,
						OutBandwidth: 2,
					},
				},
				OutPorts: []PortSchema{},
			},
		},
		Edges: []EdgeSchema{
			{
				EdgeID:    0,
				SrcNodeID: 0,
				SrcPortID: 0,
				DstNodeID: 1,
				DstPortID: 99, // Invalid port index
			},
		},
	}
	if err := net.Reset(schema4); err == nil {
		t.Fatalf("Expected error for invalid target port index, got nil")
	}

	// Test zero inBandwidth in output port
	schema5 := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID:  0,
				InPorts: []PortSchema{},
				OutPorts: []PortSchema{
					{
						BufferSize:   8,
						InBandwidth:  0, // Should cause error
						OutBandwidth: 2,
					},
				},
			},
		},
		Edges: []EdgeSchema{},
	}
	if err := net.Reset(schema5); err == nil {
		t.Fatalf("Expected error for zero inBandwidth in output port, got nil")
	}

	// Test zero outBandwidth in output port
	schema6 := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID:  0,
				InPorts: []PortSchema{},
				OutPorts: []PortSchema{
					{
						BufferSize:   8,
						InBandwidth:  2,
						OutBandwidth: 0, // Should cause error
					},
				},
			},
		},
		Edges: []EdgeSchema{},
	}
	if err := net.Reset(schema6); err == nil {
		t.Fatalf("Expected error for zero outBandwidth in output port, got nil")
	}
}

func TestNewInputQueuePanicOnZeroBandwidth(t *testing.T) {
	t.Parallel()

	// Test panic on zero inBandwidth
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("Expected panic for zero inBandwidth, got none")
		}
	}()
	queue.NewInputQueue(8, 0)
}

func TestNewOutputQueuePanicOnZeroBandwidth(t *testing.T) {
	t.Parallel()

	// Test panic on zero inBandwidth
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("Expected panic for zero inBandwidth, got none")
		}
	}()
	queue.NewOutputQueue(8, 0, 4)
}

func TestNewOutputQueuePanicOnZeroOutBandwidth(t *testing.T) {
	t.Parallel()

	// Test panic on zero outBandwidth
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("Expected panic for zero outBandwidth, got none")
		}
	}()
	queue.NewOutputQueue(8, 4, 0)
}

func TestNetworkResetWithCacheAndDirectory(t *testing.T) {
	t.Parallel()

	net := New()

	schema := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID: 0,
				InPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  4,
						OutBandwidth: 4,
					},
				},
				OutPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  2,
						OutBandwidth: 2,
					},
				},
				Cache: &CacheConfigSchema{
					Capacity:          64,
					NumSets:           1,
					ReplacementPolicy: "random",
					States:            "MESI",
				},
				Directory: &DirectoryConfigSchema{
					Capacity:          128,
					NumSets:           1,
					ReplacementPolicy: "random",
					States:            "MESI",
				},
			},
			{
				NodeID: 1,
				InPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  4,
						OutBandwidth: 4,
					},
				},
				OutPorts: []PortSchema{},
				// No cache or directory for node 1
			},
		},
		Edges: []EdgeSchema{},
	}

	if err := net.Reset(schema); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	// Verify node 0 has cache and directory
	node0 := net.nodes[0]
	caches := node0.Node.Caches()
	if len(caches) != 1 {
		t.Fatalf("Expected 1 cache for node 0, got %d", len(caches))
	}
	// Verify cache is not nil
	if caches[0] == nil {
		t.Fatalf("Cache should not be nil")
	}

	directories := node0.Node.Directories()
	if len(directories) != 1 {
		t.Fatalf("Expected 1 directory for node 0, got %d", len(directories))
	}
	// Verify directory is not nil
	if directories[0] == nil {
		t.Fatalf("Directory should not be nil")
	}

	// Verify node 1 has no cache or directory
	node1 := net.nodes[1]
	caches1 := node1.Node.Caches()
	if len(caches1) != 0 {
		t.Fatalf("Expected 0 caches for node 1, got %d", len(caches1))
	}
	directories1 := node1.Node.Directories()
	if len(directories1) != 0 {
		t.Fatalf("Expected 0 directories for node 1, got %d", len(directories1))
	}
}

func TestNetworkResetFunctional(t *testing.T) {
	t.Parallel()

	net := New()

	schema := &NetworkSchema{
		Nodes: []NodeSchema{
			{
				NodeID: 0,
				InPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  4,
						OutBandwidth: 4,
					},
				},
				OutPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  2,
						OutBandwidth: 2,
					},
				},
			},
			{
				NodeID: 1,
				InPorts: []PortSchema{
					{
						BufferSize:   16,
						InBandwidth:  4,
						OutBandwidth: 4,
					},
				},
				OutPorts: []PortSchema{},
			},
		},
		Edges: []EdgeSchema{
			{
				EdgeID:    0,
				SrcNodeID: 0,
				SrcPortID: 0,
				DstNodeID: 1,
				DstPortID: 0,
			},
		},
	}

	if err := net.Reset(schema); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	// Verify network structure is correct
	if len(net.nodes) != 2 {
		t.Fatalf("Expected 2 nodes, got %d", len(net.nodes))
	}
	if len(net.links) != 1 {
		t.Fatalf("Expected 1 link, got %d", len(net.links))
	}

	// Verify link configuration
	link := net.links[0]
	if link.SourceID() != 0 || link.TargetID() != 1 {
		t.Fatalf("Expected link 0->1, got %d->%d", link.SourceID(), link.TargetID())
	}
	if link.Latency() != 1 {
		t.Fatalf("Expected link latency 1, got %d", link.Latency())
	}
	if link.Bandwidth() != 1 {
		t.Fatalf("Expected link bandwidth 1, got %d", link.Bandwidth())
	}

	// Verify packet injection works
	pkt := packet.Packet{Payload: "test"}
	if err := net.nodes[0].Outputs[0].InjectPackets(0, []packet.Packet{pkt}); err != nil {
		t.Fatalf("Failed to inject packet: %v", err)
	}

	// Verify packet is in output queue
	if net.nodes[0].Outputs[0].Length() == 0 {
		t.Fatalf("Expected packet in output queue, but queue is empty")
	}
}

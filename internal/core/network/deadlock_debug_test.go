package network

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestDeadlockDiagnosis creates a minimal network to diagnose the deadlock issue
func TestDeadlockDiagnosis(t *testing.T) {
	fmt.Println("=== Starting Deadlock Diagnosis Test ===")

	// Create a simple 2-node network: Node0 -> Link -> Node1
	net := New()

	// Create Node0 (source)
	node0 := node.New(0)
	input0 := queue.NewInputQueue(8, 1)
	output0 := queue.NewOutputQueue(8, 1, 1)
	if err := node0.AddInputQueue(input0); err != nil {
		t.Fatalf("Node0 AddInputQueue: %v", err)
	}
	if err := node0.AddOutputQueue(output0); err != nil {
		t.Fatalf("Node0 AddOutputQueue: %v", err)
	}

	nodeHandle0 := &NodeHandle{
		Node:    node0,
		Inputs:  []*queue.InputQueue{input0},
		Outputs: []*queue.OutputQueue{output0},
	}

	// Create Node1 (destination)
	node1 := node.New(1)
	input1 := queue.NewInputQueue(8, 1)
	output1 := queue.NewOutputQueue(8, 1, 1)
	if err := node1.AddInputQueue(input1); err != nil {
		t.Fatalf("Node1 AddInputQueue: %v", err)
	}
	if err := node1.AddOutputQueue(output1); err != nil {
		t.Fatalf("Node1 AddOutputQueue: %v", err)
	}

	nodeHandle1 := &NodeHandle{
		Node:    node1,
		Inputs:  []*queue.InputQueue{input1},
		Outputs: []*queue.OutputQueue{output1},
	}

	// Add nodes to network
	if err := net.AddNode(nodeHandle0); err != nil {
		t.Fatalf("AddNode 0: %v", err)
	}
	if err := net.AddNode(nodeHandle1); err != nil {
		t.Fatalf("AddNode 1: %v", err)
	}

	fmt.Println("Nodes created and added to network")

	// Connect nodes
	linkLatency := 2
	linkHandle, err := net.Connect(0, 0, 1, 0, linkLatency, 1)
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	fmt.Printf("Connected Node0 -> Node1 with latency=%d\n", linkLatency)

	// Add hook to monitor Link activity
	linkHandle.SetTickHook(func(cycle int) {
		occupancy := linkHandle.SnapshotOccupancy()
		fmt.Printf("[Link Tick] cycle=%d, occupancy=%v\n", cycle, occupancy)
	})

	fmt.Printf("Link readyUntil after creation: (checking via advance)\n")

	// Add hooks to monitor activities
	fmt.Println("=== Setting up monitoring hooks ===")

	node0.SetTickHook(func(cycle uint64) {
		fmt.Printf("[Node0 Tick] cycle=%d, OutputQueue0=%d slots\n", cycle, output0.Length())
	})

	node1.SetTickHook(func(cycle uint64) {
		fmt.Printf("[Node1 Tick] cycle=%d, InputQueue1=%d/%d\n", cycle, input1.Length(), input1.Capacity())
	})

	// Setup Node0 to inject a packet
	var injected bool
	node0.SetProcessHook(func(_ context.Context, cycle uint64, buffer []packet.Packet) ([]packet.Packet, error) {
		fmt.Printf("[Node0 ProcessHook] cycle=%d, buffer=%v, injected=%v\n", cycle, buffer, injected)
		if cycle == 0 && !injected {
			pkt := packet.Packet{
				SourceID: 0,
				TargetID: 1,
				Payload:  "test",
			}
			fmt.Printf("[Node0 Hook] Attempting to inject packet at cycle=%d\n", cycle)
			if err := output0.InjectPackets(int(cycle), []packet.Packet{pkt}); err != nil {
				fmt.Printf("[Node0 Hook] Injection failed: %v\n", err)
				return nil, err
			}
			injected = true
			fmt.Printf("[Node0 Hook] Packet injected successfully\n")
		}
		return buffer, nil
	})

	// Setup Node1 to receive packets
	var receivedPackets []packet.Packet
	var mu sync.Mutex
	node1.SetProcessHook(func(_ context.Context, cycle uint64, buffer []packet.Packet) ([]packet.Packet, error) {
		fmt.Printf("[Node1 Hook] cycle=%d, received %d packets\n", cycle, len(buffer))
		if len(buffer) > 0 {
			mu.Lock()
			receivedPackets = append(receivedPackets, buffer...)
			mu.Unlock()
		}
		return nil, nil
	})

	// Add timeout to catch deadlock
	done := make(chan error, 1)
	go func() {
		fmt.Println("Starting network.Advance(10)")
		err := net.Advance(10)
		fmt.Printf("network.Advance completed with err=%v\n", err)
		done <- err
	}()

	// Wait with timeout
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Advance failed: %v", err)
		}
		fmt.Printf("Test completed successfully. Received %d packets\n", len(receivedPackets))

		// Print link state
		fmt.Printf("Link final state: sourceID=%d, targetID=%d, latency=%d\n",
			linkHandle.SourceID(), linkHandle.TargetID(), linkHandle.Latency())

	case <-time.After(2 * time.Second):
		t.Fatal("DEADLOCK DETECTED: Test timed out after 2 seconds")
	}
}

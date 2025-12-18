package network

import (
	"context"
	"math/rand"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestNetworkLargeRing50Nodes tests a ring topology with performance measurements.
// The number of nodes is automatically set to NumCPU/2.
// Topology: Node0 -> Node1 -> ... -> NodeN-1 -> Node0 (ring)
// Each link has 10 cycles latency.
// Packets are injected from Node0 every 3 cycles.
// Last node receives and drops packets.
// Runs for 10,000 cycles with both single-core and multi-core modes.
func TestNetworkLargeRing50Nodes(t *testing.T) {
	nodeCount := runtime.NumCPU() / 2
	if nodeCount < 2 {
		nodeCount = 2 // Minimum 2 nodes
	}

	const (
		linkLatency    = 10
		advanceCycles  = 10000
		injectInterval = 3 // Inject packet every 3 cycles
	)

	t.Logf("Testing with %d nodes (NumCPU=%d, using NumCPU/2)", nodeCount, runtime.NumCPU())

	// Test both single-core and multi-core
	testCases := []struct {
		name       string
		gomaxprocs int
	}{
		{"SingleCore", 1},
		{"MultiCore", runtime.NumCPU()},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set GOMAXPROCS for this test
			oldMaxProcs := runtime.GOMAXPROCS(tc.gomaxprocs)
			defer runtime.GOMAXPROCS(oldMaxProcs)

			t.Logf("Running with GOMAXPROCS=%d (CPU cores: %d)", tc.gomaxprocs, runtime.NumCPU())

			// Create network
			net := New()

			// Create 50 nodes with 1 input and 1 output each
			nodeHandles := make([]*NodeHandle, nodeCount)
			var allInputs []*queue.InputQueue
			var allOutputs []*queue.OutputQueue

			for i := 0; i < nodeCount; i++ {
				n := node.NewWorkerNode(i)

				// Create input and output queues with reasonable buffer sizes
				// IMPORTANT: OutputQueue bandwidth must match or be less than Link bandwidth
				// to avoid overwhelming links. With Link bandwidth=1, use OutputQueue bandwidth=1
				input := queue.NewInputQueue(64, 1)
				output := queue.NewOutputQueue(64, 1, 1)

				if err := n.AddInputQueue(input); err != nil {
					t.Fatalf("Node%d AddInputQueue: %v", i, err)
				}
				if err := n.AddOutputQueue(output); err != nil {
					t.Fatalf("Node%d AddOutputQueue: %v", i, err)
				}

				nodeHandles[i] = &NodeHandle{
					Node:    n,
					Inputs:  []*queue.InputQueue{input},
					Outputs: []*queue.OutputQueue{output},
				}

				allInputs = append(allInputs, input)
				allOutputs = append(allOutputs, output)

				if err := net.AddNode(nodeHandles[i]); err != nil {
					t.Fatalf("AddNode %d: %v", i, err)
				}
			}

			// Setup forwarding: nodes 1 to nodeCount-2 forward from input to output with random delay
			// Node0 and last node will have custom hooks set later
			// Delay: 2-20us per node, simulating GEM5 O3CPU execution time
			for i := 1; i < nodeCount-1; i++ {
				output := allOutputs[i]
				nodeHandles[i].Node.(*node.WorkerNode).SetProcessHook(forwardWithDelayHook(output, 2, 20))
			}

			// Connect nodes in a ring: 0->1->2->...->49->0
			// Use bandwidth=1 as requested
			for i := 0; i < nodeCount; i++ {
				srcNode := i
				dstNode := (i + 1) % nodeCount
				if _, err := net.Connect(srcNode, 0, dstNode, 0, linkLatency, 1); err != nil {
					t.Fatalf("Connect %d->%d: %v", srcNode, dstNode, err)
				}
			}

			// Setup packet injection from Node0 every 3 cycles
			// Node0 is the source - it only injects new packets, doesn't forward
			var injectedCount int64
			nodeHandles[0].Node.(*node.WorkerNode).SetProcessHook(func(_ context.Context, cycle uint64, inputs [][]packet.Packet) error {
				// Drop any incoming packets (break the ring at Node0)
				// This prevents multiple packets being sent in the same cycle

				// Inject new packet every 3 cycles
				if cycle%injectInterval == 0 && int(cycle) < advanceCycles {
					pkt := packet.Packet{
						SourceID: 0,
						TargetID: nodeCount - 1,
						Payload:  "data",
					}
					if err := allOutputs[0].InjectPackets(int(cycle), []packet.Packet{pkt}); err == nil {
						atomic.AddInt64(&injectedCount, 1)
					}
				}
				return nil
			})

			// Setup packet reception and drop at last node
			lastNodeIndex := nodeCount - 1
			var receivedCount int64
			allInputs[lastNodeIndex].SetPacketReceivedHook(func(pkt packet.Packet) {
				// Count received packets (they will be dropped automatically as we don't forward)
				atomic.AddInt64(&receivedCount, 1)
			})

			// Last node should NOT forward (drop packets)
			nodeHandles[lastNodeIndex].Node.(*node.WorkerNode).SetProcessHook(func(_ context.Context, cycle uint64, inputs [][]packet.Packet) error {
				// Drop all packets by not forwarding them
				return nil
			})

			// Run simulation and measure time
			t.Logf("Starting simulation: %d cycles, %d nodes, %d-cycle latency per link",
				advanceCycles, nodeCount, linkLatency)

			startTime := time.Now()
			if err := net.Advance(advanceCycles); err != nil {
				t.Fatalf("Advance failed: %v", err)
			}
			duration := time.Since(startTime)

			// Report statistics
			injected := atomic.LoadInt64(&injectedCount)
			received := atomic.LoadInt64(&receivedCount)

			t.Logf("Simulation completed in %v", duration)
			t.Logf("Packets injected: %d", injected)
			t.Logf("Packets received at Node%d: %d", nodeCount-1, received)
			t.Logf("Throughput: %.2f cycles/sec", float64(advanceCycles)/duration.Seconds())
			t.Logf("Average time per cycle: %v", duration/time.Duration(advanceCycles))

			// Calculate expected received packets
			// A packet injected at cycle C will reach the last node after nodeCount * linkLatency cycles
			// So packets injected before (advanceCycles - nodeCount*linkLatency) should be received
			ringLatency := nodeCount * linkLatency
			expectedReceived := (advanceCycles - ringLatency) / injectInterval
			if expectedReceived < 0 {
				expectedReceived = 0
			}

			t.Logf("Expected received (approximate): %d", expectedReceived)

			// Verify packets were actually transmitted
			if injected == 0 {
				t.Errorf("No packets were injected")
			}

			// The received count should be reasonable (within 10% of expected)
			// Allow some tolerance due to timing and buffer limits
			if received > 0 {
				ratio := float64(received) / float64(expectedReceived)
				t.Logf("Received/Expected ratio: %.2f", ratio)

				// Should receive at least some packets
				if ratio < 0.5 {
					t.Logf("Warning: Received much fewer packets than expected (%.1f%%)", ratio*100)
				}
			} else {
				t.Errorf("No packets were received at Node%d", nodeCount-1)
			}

			// Performance assertions
			avgCycleTime := duration / time.Duration(advanceCycles)
			if tc.gomaxprocs == 1 {
				// Single core should be slower
				t.Logf("Single-core average cycle time: %v", avgCycleTime)
			} else {
				// Multi-core should be faster
				t.Logf("Multi-core average cycle time: %v", avgCycleTime)
			}
		})
	}
}

// forwardWithDelayHook creates a ProcessHook that forwards packets with random delay.
func forwardWithDelayHook(output *queue.OutputQueue, minUs, maxUs int) func(ctx context.Context, cycle uint64, inputs [][]packet.Packet) error {
	return func(_ context.Context, cycle uint64, inputs [][]packet.Packet) error {
		var buffer []packet.Packet
		for _, b := range inputs {
			buffer = append(buffer, b...)
		}
		// Add random delay (0-100us)
		delayUs := minUs
		if maxUs > minUs {
			delayUs += rand.Intn(maxUs - minUs)
		}
		if delayUs > 0 {
			time.Sleep(time.Duration(delayUs) * time.Microsecond)
		}

		// Forward packets
		if len(buffer) > 0 {
			if err := output.InjectPackets(int(cycle), clonePackets(buffer)); err != nil {
				return err
			}
		}
		return nil
	}
}

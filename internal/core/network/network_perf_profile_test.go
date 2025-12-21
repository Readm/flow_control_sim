package network

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// BenchmarkNetworkScaling tests network performance with different node counts.
// This benchmark is designed for profiling with:
//
//	go test -bench=BenchmarkNetworkScaling -cpuprofile=cpu.prof -memprofile=mem.prof
//	go tool pprof cpu.prof
func BenchmarkNetworkScaling(b *testing.B) {
	testCases := []struct {
		name      string
		nodeCount int
	}{
		{"Nodes_4", 4},
		{"Nodes_8", 8},
		{"Nodes_16", 16},
		{"Nodes_32", 32},
		{"Nodes_64", 64},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			const (
				linkLatency    = 10
				advanceCycles  = 1000 // Reduced for benchmarking
				injectInterval = 3
			)

			nodeCount := tc.nodeCount

			// Create network
			net := New()

			// Create nodes
			nodeHandles := make([]*NodeHandle, nodeCount)
			var allOutputs []*queue.OutputQueue

			for i := 0; i < nodeCount; i++ {
				n := node.NewWorkerNode(i)
				input := queue.NewInputQueue(64, 1)
				output := queue.NewOutputQueue(64, 1)

				if err := n.AddInputQueue(input); err != nil {
					b.Fatalf("Node%d AddInputQueue: %v", i, err)
				}
				if err := n.AddOutputQueue(output); err != nil {
					b.Fatalf("Node%d AddOutputQueue: %v", i, err)
				}

				nodeHandles[i] = &NodeHandle{
					Node:    n,
					Inputs:  []*queue.InputQueue{input},
					Outputs: []*queue.OutputQueue{output},
				}

				allOutputs = append(allOutputs, output)

				if err := net.AddNode(nodeHandles[i]); err != nil {
					b.Fatalf("AddNode %d: %v", i, err)
				}
			}

			// Setup forwarding
			for i := 1; i < nodeCount-1; i++ {
				output := allOutputs[i]
				nodeHandles[i].Node.(*node.WorkerNode).SetProcessHook(func(_ context.Context, cycle uint64, incoming [][]packet.Packet) error {
					if len(incoming) > 0 {
						var flat []packet.Packet
						for _, pkts := range incoming {
							flat = append(flat, pkts...)
						}
						if len(flat) > 0 {
							if err := output.InjectPackets(int(cycle), clonePackets(flat)); err != nil {
								return err
							}
						}
					}
					return nil
				})
			}

			// Connect nodes in a ring
			for i := 0; i < nodeCount; i++ {
				srcNode := i
				dstNode := (i + 1) % nodeCount
				if _, err := net.Connect(srcNode, 0, dstNode, 0, linkLatency, 1); err != nil {
					b.Fatalf("Connect %d->%d: %v", srcNode, dstNode, err)
				}
			}

			// Setup packet injection from Node0
			var injectedCount int64
			nodeHandles[0].Node.(*node.WorkerNode).SetProcessHook(func(_ context.Context, cycle uint64, incoming [][]packet.Packet) error {
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

			// Last node drops packets
			lastNodeIndex := nodeCount - 1
			nodeHandles[lastNodeIndex].Node.(*node.WorkerNode).SetProcessHook(func(_ context.Context, cycle uint64, incoming [][]packet.Packet) error {
				return nil
			})

			// Reset timer before actual benchmark
			b.ResetTimer()

			// Run benchmark
			for i := 0; i < b.N; i++ {
				if err := net.AdvanceTo(net.CurrentCycle() + advanceCycles - 1); err != nil {
					b.Fatalf("Advance failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkNetworkScalingMultiCore tests with full CPU cores
func BenchmarkNetworkScalingMultiCore(b *testing.B) {
	// Force multi-core
	runtime.GOMAXPROCS(runtime.NumCPU())

	testCases := []struct {
		name      string
		nodeCount int
	}{
		{"Nodes_4", 4},
		{"Nodes_8", 8},
		{"Nodes_16", 16},
		{"Nodes_32", 32},
		{"Nodes_64", 64},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			const (
				linkLatency    = 10
				advanceCycles  = 1000
				injectInterval = 3
			)

			nodeCount := tc.nodeCount

			net := New()
			nodeHandles := make([]*NodeHandle, nodeCount)
			var allOutputs []*queue.OutputQueue

			for i := 0; i < nodeCount; i++ {
				n := node.NewWorkerNode(i)
				input := queue.NewInputQueue(64, 1)
				output := queue.NewOutputQueue(64, 1)

				if err := n.AddInputQueue(input); err != nil {
					b.Fatalf("Node%d AddInputQueue: %v", i, err)
				}
				if err := n.AddOutputQueue(output); err != nil {
					b.Fatalf("Node%d AddOutputQueue: %v", i, err)
				}

				nodeHandles[i] = &NodeHandle{
					Node:    n,
					Inputs:  []*queue.InputQueue{input},
					Outputs: []*queue.OutputQueue{output},
				}

				allOutputs = append(allOutputs, output)

				if err := net.AddNode(nodeHandles[i]); err != nil {
					b.Fatalf("AddNode %d: %v", i, err)
				}
			}

			for i := 1; i < nodeCount-1; i++ {
				output := allOutputs[i]
				nodeHandles[i].Node.(*node.WorkerNode).SetProcessHook(func(_ context.Context, cycle uint64, incoming [][]packet.Packet) error {
					if len(incoming) > 0 {
						var flat []packet.Packet
						for _, pkts := range incoming {
							flat = append(flat, pkts...)
						}
						if len(flat) > 0 {
							if err := output.InjectPackets(int(cycle), clonePackets(flat)); err != nil {
								return err
							}
						}
					}
					return nil
				})
			}

			for i := 0; i < nodeCount; i++ {
				srcNode := i
				dstNode := (i + 1) % nodeCount
				if _, err := net.Connect(srcNode, 0, dstNode, 0, linkLatency, 1); err != nil {
					b.Fatalf("Connect %d->%d: %v", srcNode, dstNode, err)
				}
			}

			var injectedCount int64
			nodeHandles[0].Node.(*node.WorkerNode).SetProcessHook(func(_ context.Context, cycle uint64, incoming [][]packet.Packet) error {
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

			lastNodeIndex := nodeCount - 1
			nodeHandles[lastNodeIndex].Node.(*node.WorkerNode).SetProcessHook(func(_ context.Context, cycle uint64, incoming [][]packet.Packet) error {
				return nil
			})

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				if err := net.AdvanceTo(net.CurrentCycle() + advanceCycles - 1); err != nil {
					b.Fatalf("Advance failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkRing50CoreScaling benchmarks a 50-node ring network with varying CPU core counts.
// This scans from 1 to NumCPU cores, sampling at most 8 points.
// Usage:
//
//	go test -bench=BenchmarkRing50CoreScaling -benchmem ./internal/core/network
func BenchmarkRing50CoreScaling(b *testing.B) {
	const (
		nodeCount      = 50
		linkLatency    = 10
		advanceCycles  = 1000
		injectInterval = 3
		maxSamples     = 8
	)

	numCPU := runtime.NumCPU()

	// Generate sample points from 1 to NumCPU (at most 8 points)
	var coreCountSamples []int
	if numCPU <= maxSamples {
		// If we have 8 or fewer CPUs, test all of them
		for i := 1; i <= numCPU; i++ {
			coreCountSamples = append(coreCountSamples, i)
		}
	} else {
		// Sample at most 8 points evenly distributed from 1 to NumCPU
		// Always include 1 and NumCPU
		step := float64(numCPU-1) / float64(maxSamples-1)
		for i := 0; i < maxSamples; i++ {
			coreCount := int(1 + float64(i)*step + 0.5) // Round to nearest
			// Avoid duplicates
			if len(coreCountSamples) == 0 || coreCountSamples[len(coreCountSamples)-1] != coreCount {
				coreCountSamples = append(coreCountSamples, coreCount)
			}
		}
		// Ensure we include NumCPU
		if coreCountSamples[len(coreCountSamples)-1] != numCPU {
			coreCountSamples[len(coreCountSamples)-1] = numCPU
		}
	}

	b.Logf("Testing with core counts: %v (NumCPU=%d)", coreCountSamples, numCPU)

	for _, coreCount := range coreCountSamples {
		b.Run(fmt.Sprintf("Cores_%d", coreCount), func(b *testing.B) {
			// Set GOMAXPROCS for this benchmark
			oldMaxProcs := runtime.GOMAXPROCS(coreCount)
			defer runtime.GOMAXPROCS(oldMaxProcs)

			// Create network
			net := New()

			// Create nodes
			nodeHandles := make([]*NodeHandle, nodeCount)
			var allInputs []*queue.InputQueue
			var allOutputs []*queue.OutputQueue

			for i := 0; i < nodeCount; i++ {
				n := node.NewWorkerNode(i)
				input := queue.NewInputQueue(64, 1)
				output := queue.NewOutputQueue(64, 1)

				if err := n.AddInputQueue(input); err != nil {
					b.Fatalf("Node%d AddInputQueue: %v", i, err)
				}
				if err := n.AddOutputQueue(output); err != nil {
					b.Fatalf("Node%d AddOutputQueue: %v", i, err)
				}

				nodeHandles[i] = &NodeHandle{
					Node:    n,
					Inputs:  []*queue.InputQueue{input},
					Outputs: []*queue.OutputQueue{output},
				}

				allInputs = append(allInputs, input)
				allOutputs = append(allOutputs, output)

				if err := net.AddNode(nodeHandles[i]); err != nil {
					b.Fatalf("AddNode %d: %v", i, err)
				}
			}

			// Setup forwarding: nodes 1 to nodeCount-2 forward from input to output with random delay
			// Delay: 5-20us per node, simulating GEM5 O3CPU execution time
			for i := 1; i < nodeCount-1; i++ {
				output := allOutputs[i]
				nodeHandles[i].Node.(*node.WorkerNode).SetProcessHook(func(_ context.Context, cycle uint64, inputs [][]packet.Packet) error {
					// Each node execution simulates GEM5 O3 CPU core processing
					node.SpinWait(5, 20)

					var buffer []packet.Packet
					for _, b := range inputs {
						buffer = append(buffer, b...)
					}

					// Forward packets
					if len(buffer) > 0 {
						if err := output.InjectPackets(int(cycle), clonePackets(buffer)); err != nil {
							return err
						}
					}
					return nil
				})
			}

			// Connect nodes in a ring: 0->1->2->...->49->0
			for i := 0; i < nodeCount; i++ {
				srcNode := i
				dstNode := (i + 1) % nodeCount
				if _, err := net.Connect(srcNode, 0, dstNode, 0, linkLatency, 1); err != nil {
					b.Fatalf("Connect %d->%d: %v", srcNode, dstNode, err)
				}
			}

			// Setup packet injection from Node0 every 3 cycles
			var injectedCount int64
			nodeHandles[0].Node.(*node.WorkerNode).SetProcessHook(func(_ context.Context, cycle uint64, inputs [][]packet.Packet) error {
				// Each node execution simulates GEM5 O3 CPU core processing
				node.SpinWait(5, 20)

				// Drop any incoming packets (break the ring at Node0)

				// Inject new packet every 3 cycles
				// continuously throughout the benchmark to maintain load
				if cycle%injectInterval == 0 {
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
				atomic.AddInt64(&receivedCount, 1)
			})

			// Last node should NOT forward (drop packets)
			nodeHandles[lastNodeIndex].Node.(*node.WorkerNode).SetProcessHook(func(_ context.Context, cycle uint64, inputs [][]packet.Packet) error {
				// Each node execution simulates GEM5 O3 CPU core processing
				node.SpinWait(5, 20)
				// Drop all packets by not forwarding them
				return nil
			})

			// Reset timer before actual benchmark
			b.ResetTimer()

			// Run benchmark
			for i := 0; i < b.N; i++ {
				if err := net.AdvanceTo(net.CurrentCycle() + advanceCycles - 1); err != nil {
					b.Fatalf("Advance failed: %v", err)
				}
			}

			// Stop timer for validation
			b.StopTimer()

			// Functional correctness validation
			injected := atomic.LoadInt64(&injectedCount)
			received := atomic.LoadInt64(&receivedCount)

			// Calculate expected metrics
			// Since we inject continuously, the network has packets "in flight" that haven't arrived yet.
			// The number of in-flight packets is roughly: ringLatency / injectInterval
			ringLatency := nodeCount * linkLatency
			inFlightPackets := int64(ringLatency / injectInterval)

			// Expected received is total injected minus those still in flight
			// We allow a small buffer/margin as simulation timing can vary slightly
			expectedReceived := injected - inFlightPackets
			if expectedReceived < 0 {
				expectedReceived = 0
			}

			// Verify packets were actually transmitted
			if injected == 0 {
				b.Fatalf("Correctness check failed: No packets were injected")
			}

			// Verify data reception logic
			// In a continuous flow, received count should be close to expected
			ratio := 0.0
			if expectedReceived > 0 {
				ratio = float64(received) / float64(expectedReceived)
			}

			// We use a slightly lower threshold (90%) because in the very first iteration
			// the pipeline filling phase might skew the ratio slightly if b.N is small
			if ratio < 0.90 {
				b.Fatalf("Correctness check failed: Received only %.1f%% of expected packets (%d received, %d expected, injected %d)",
					ratio*100, received, expectedReceived, injected)
			}

			// Warning if ratio is too high (should be impossible in valid logic)
			if ratio > 1.1 {
				b.Logf("Warning: Received count %.1f%% higher than expected (maybe timing jitter?)", ratio*100)
			}

			// Report additional stats after validation
			b.ReportMetric(float64(injected), "packets_injected")
			b.ReportMetric(float64(received), "packets_received")
			b.ReportMetric(ratio*100, "reception_rate_pct")
		})
	}
}

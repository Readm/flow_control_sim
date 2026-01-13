package network

import (
	// Unused directly, but might be needed if I missed something. Wait, Node doesn't use it.
	// But runtime.NumCPU is used.
	// I'll remove "context" only.
	"flag"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

var benchNodeCount = flag.Int("bench_nodes", 50, "Number of nodes for benchmark")

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
				nodeHandles[i].Node.(*node.WorkerNode).SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
					if len(inputs) > 0 {
						var flat []packet.Packet
						for _, q := range inputs {
							for _, ref := range q {
								flat = append(flat, ref.Packet)
								ref.Queue.Free(ref.Slot)
							}
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
			nodeHandles[0].Node.(*node.WorkerNode).SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
				// Consume inputs if any
				for _, q := range inputs {
					for _, ref := range q {
						ref.Queue.Free(ref.Slot)
					}
				}
				if cycle%injectInterval == 0 && int(cycle) < advanceCycles {
					pkt := packet.Packet{
						SourceID: 0,
						TargetID: nodeCount - 1,
						Metadata: map[string]interface{}{"payload": "data"},
					}
					if err := allOutputs[0].InjectPackets(int(cycle), []packet.Packet{pkt}); err == nil {
						atomic.AddInt64(&injectedCount, 1)
					}
				}
				return nil
			})

			// Last node drops packets
			lastNodeIndex := nodeCount - 1
			nodeHandles[lastNodeIndex].Node.(*node.WorkerNode).SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
				for _, q := range inputs {
					for _, ref := range q {
						ref.Queue.Free(ref.Slot)
					}
				}
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
				nodeHandles[i].Node.(*node.WorkerNode).SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
					if len(inputs) > 0 {
						var flat []packet.Packet
						for _, q := range inputs {
							for _, ref := range q {
								flat = append(flat, ref.Packet)
								ref.Queue.Free(ref.Slot)
							}
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
			nodeHandles[0].Node.(*node.WorkerNode).SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
				for _, q := range inputs {
					for _, ref := range q {
						ref.Queue.Free(ref.Slot)
					}
				}
				if cycle%injectInterval == 0 && int(cycle) < advanceCycles {
					pkt := packet.Packet{
						SourceID: 0,
						TargetID: nodeCount - 1,
						Metadata: map[string]interface{}{"payload": "data"},
					}
					if err := allOutputs[0].InjectPackets(int(cycle), []packet.Packet{pkt}); err == nil {
						atomic.AddInt64(&injectedCount, 1)
					}
				}
				return nil
			})

			lastNodeIndex := nodeCount - 1
			nodeHandles[lastNodeIndex].Node.(*node.WorkerNode).SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
				for _, q := range inputs {
					for _, ref := range q {
						ref.Queue.Free(ref.Slot)
					}
				}
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

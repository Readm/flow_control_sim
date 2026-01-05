package benchmarks

import (
	"flag"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/network"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/core/trace"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

var benchNodeCount = flag.Int("bench_nodes", 50, "Number of nodes for benchmark")

// BenchmarkRingCoreScaling benchmarks a ring network with varying CPU core counts.
func BenchmarkRingCoreScaling(b *testing.B) {
	RunScalingBenchmark(b, "RingCoreScaling", func(b *testing.B, coreCount int) *network.Network {
		const (
			linkLatency    = 10
			injectInterval = 3
		)

		nodeCount := *benchNodeCount

		// Calculate ring latency: path from Node 0 to Node N-1 is (N-1) hops
		ringLatency := (nodeCount - 1) * linkLatency

		// Ensure advanceCycles is large enough
		advanceCycles := 1000
		if ringLatency*2 > advanceCycles {
			advanceCycles = ringLatency * 2
		}

		// Calibrate CPU cycles per microsecond
		cyclesPerUS := node.CalibrateCyclesPerUS(100 * time.Millisecond)

		// Inject Trace Flush
		defer func() {
			if err := trace.FlushGlobal(); err != nil {
				b.Logf("Trace Flush failed: %v", err)
			}
		}()

		// Calculate min/max cycles for 5-20us spin
		minSpinCycles := int(5.0 * cyclesPerUS)
		maxSpinCycles := int(20.0 * cyclesPerUS)

		// Create network
		net := network.New()

		// Create nodes
		nodeHandles := make([]*network.NodeHandle, nodeCount)
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

			nodeHandles[i] = &network.NodeHandle{
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

		// Setup forwarding
		for i := 1; i < nodeCount-1; i++ {
			output := allOutputs[i]
			nodeHandles[i].Node.(*node.WorkerNode).SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
				// Simulate GEM5 O3 CPU
				cycles := minSpinCycles
				if maxSpinCycles > minSpinCycles {
					cycles += rand.Intn(maxSpinCycles - minSpinCycles)
				}
				node.SpinWaitCycles(uint64(cycles))

				var buffer []packet.Packet
				for _, q := range inputs {
					for _, ref := range q {
						buffer = append(buffer, ref.Packet)
						ref.Queue.Free(ref.Slot)
					}
				}

				if len(buffer) > 0 {
					if err := output.InjectPackets(int(cycle), clonePackets(buffer)); err != nil {
						return err
					}
				}
				return nil
			})
		}

		// Connect nodes via New API
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
			cycles := minSpinCycles
			if maxSpinCycles > minSpinCycles {
				cycles += rand.Intn(maxSpinCycles - minSpinCycles)
			}
			node.SpinWaitCycles(uint64(cycles))

			// Drop incoming
			for _, q := range inputs {
				for _, ref := range q {
					ref.Queue.Free(ref.Slot)
				}
			}

			// Inject
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

		// Last node reception
		lastNodeIndex := nodeCount - 1
		var receivedCount int64
		allInputs[lastNodeIndex].SetPacketReceivedHook(func(pkt packet.Packet) {
			atomic.AddInt64(&receivedCount, 1)
		})

		nodeHandles[lastNodeIndex].Node.(*node.WorkerNode).SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
			cycles := minSpinCycles
			if maxSpinCycles > minSpinCycles {
				cycles += rand.Intn(maxSpinCycles - minSpinCycles)
			}
			node.SpinWaitCycles(uint64(cycles))
			for _, q := range inputs {
				for _, ref := range q {
					ref.Queue.Free(ref.Slot)
				}
			}
			return nil
		})

		// Warmup
		if err := net.AdvanceTo(net.CurrentCycle() + ringLatency + 100); err != nil {
			b.Fatalf("Warmup Advance failed: %v", err)
		}

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			if err := net.AdvanceTo(net.CurrentCycle() + advanceCycles - 1); err != nil {
				b.Fatalf("Advance failed: %v", err)
			}
		}

		b.StopTimer()

		// Metrics
		// Metrics
		injected := atomic.LoadInt64(&injectedCount)
		received := atomic.LoadInt64(&receivedCount)

		ratio := 0.0
		if injected > 0 {
			ratio = float64(received) / float64(injected)
		}

		// Calculate sim_Hz
		// Total simulated cycles = b.N * advanceCycles
		// Total time = b.Elapsed().Seconds()
		// Note: b.Elapsed() includes overhead, but b.ResetTimer() was called.
		// A more accurate per-op latency is N/Elapsed.
		// sim_Hz = (TotalSimCycles) / TotalTime

		elapsedSec := b.Elapsed().Seconds()
		if elapsedSec == 0 {
			elapsedSec = 1e-9 // Avoid div zero
		}
		totalSimCycles := float64(b.N) * float64(advanceCycles)
		simHz := totalSimCycles / elapsedSec

		b.ReportMetric(simHz, "sim_Hz")
		b.ReportMetric(ratio*100, "reception_rate_pct")

		return net
	})
}

// BenchmarkBidirectionalRingCoreScaling tests a bidirectional ring network.
func BenchmarkBidirectionalRingCoreScaling(b *testing.B) {
	RunScalingBenchmark(b, "BidirectionalRingCoreScaling", func(b *testing.B, coreCount int) *network.Network {
		const (
			linkLatency = 10
		)

		nodeCount := *benchNodeCount
		injectInterval := nodeCount / 2
		if injectInterval < 1 {
			injectInterval = 1
		}

		maxLatency := (nodeCount / 2) * linkLatency
		advanceCycles := 2000
		if maxLatency*4 > advanceCycles {
			advanceCycles = maxLatency * 4
		}

		cyclesPerUS := node.CalibrateCyclesPerUS(100 * time.Millisecond)
		minSpinCycles := int(5.0 * cyclesPerUS)
		maxSpinCycles := int(20.0 * cyclesPerUS)

		net := network.New()
		nodeHandles := make([]*network.NodeHandle, nodeCount)
		var allOutputs [][]*queue.OutputQueue

		for i := 0; i < nodeCount; i++ {
			n := node.NewWorkerNode(i)
			input0 := queue.NewInputQueue(64, 1)
			input1 := queue.NewInputQueue(64, 1)
			output0 := queue.NewOutputQueue(64, 1)
			output1 := queue.NewOutputQueue(64, 1)

			n.AddInputQueue(input0)
			n.AddInputQueue(input1)
			n.AddOutputQueue(output0)
			n.AddOutputQueue(output1)

			nodeHandles[i] = &network.NodeHandle{
				Node:    n,
				Inputs:  []*queue.InputQueue{input0, input1},
				Outputs: []*queue.OutputQueue{output0, output1},
			}
			allOutputs = append(allOutputs, []*queue.OutputQueue{output0, output1})

			if err := net.AddNode(nodeHandles[i]); err != nil {
				b.Fatalf("AddNode %d: %v", i, err)
			}
		}

		getDirection := func(src, dst int) int {
			cwDist := (dst - src + nodeCount) % nodeCount
			ccwDist := (src - dst + nodeCount) % nodeCount
			if cwDist <= ccwDist {
				return 0 // CW
			}
			return 1 // CCW
		}

		var injectedCount int64
		var receivedCount int64

		for i := 0; i < nodeCount; i++ {
			nodeIdx := i
			outputs := allOutputs[i]

			nodeHandles[i].Node.(*node.WorkerNode).SetProcessHook(func(cycle uint64, inputs [][]queue.PacketRef) error {
				cycles := minSpinCycles
				if maxSpinCycles > minSpinCycles {
					cycles += rand.Intn(maxSpinCycles - minSpinCycles)
				}
				node.SpinWaitCycles(uint64(cycles))

				// Process forwarding
				for _, q := range inputs {
					for _, ref := range q {
						pkt := ref.Packet
						if pkt.TargetID == nodeIdx {
							atomic.AddInt64(&receivedCount, 1)
							ref.Queue.Free(ref.Slot)
						} else {
							dir := getDirection(nodeIdx, pkt.TargetID)
							outQ := outputs[dir]
							if !outQ.IsFull() {
								if err := outQ.InjectPackets(int(cycle), []packet.Packet{pkt}); err == nil {
									ref.Queue.Free(ref.Slot)
								}
							}
						}
					}
				}

				// Traffic generation
				if cycle%uint64(injectInterval) == 0 && int(cycle) < advanceCycles {
					target := rand.Intn(nodeCount)
					if target == nodeIdx {
						target = (target + 1) % nodeCount
					}
					dir := getDirection(nodeIdx, target)
					outQ := outputs[dir]
					if !outQ.IsFull() {
						pkt := packet.Packet{
							SourceID: nodeIdx,
							TargetID: target,
							Payload:  "bi-data",
						}
						if err := outQ.InjectPackets(int(cycle), []packet.Packet{pkt}); err == nil {
							atomic.AddInt64(&injectedCount, 1)
						}
					}
				}
				return nil
			})
		}

		for i := 0; i < nodeCount; i++ {
			cwNext := (i + 1) % nodeCount
			net.Connect(i, 0, cwNext, 0, linkLatency, 1)
			ccwNext := (i - 1 + nodeCount) % nodeCount
			net.Connect(i, 1, ccwNext, 1, linkLatency, 1)
		}

		if err := net.AdvanceTo(net.CurrentCycle() + maxLatency + 100); err != nil {
			b.Fatalf("Warmup failed: %v", err)
		}

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			if err := net.AdvanceTo(net.CurrentCycle() + advanceCycles); err != nil {
				b.Fatalf("Advance failed: %v", err)
			}
		}

		b.StopTimer()

		// Metrics
		injected := atomic.LoadInt64(&injectedCount)
		received := atomic.LoadInt64(&receivedCount)

		ratio := 0.0
		if injected > 0 {
			ratio = float64(received) / float64(injected)
		}

		// Calculate sim_Hz
		elapsedSec := b.Elapsed().Seconds()
		if elapsedSec == 0 {
			elapsedSec = 1e-9
		}
		totalSimCycles := float64(b.N) * float64(advanceCycles)
		simHz := totalSimCycles / elapsedSec

		b.ReportMetric(simHz, "sim_Hz")
		b.ReportMetric(ratio*100, "reception_rate_pct")

		return net
	})
}

func clonePackets(src []packet.Packet) []packet.Packet {
	cloned := make([]packet.Packet, len(src))
	copy(cloned, src)
	return cloned
}

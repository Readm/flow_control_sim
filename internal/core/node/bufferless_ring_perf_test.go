package node

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// BenchmarkBufferlessRing_Scaling tests scaling performance with different node counts.
// Tests both single-core and multi-core execution.
// Node count is automatically adjusted based on CPU cores.
func BenchmarkBufferlessRing_Scaling(b *testing.B) {
	// Determine node counts based on available CPUs
	baseCPU := runtime.NumCPU()
	nodeCounts := []int{4, 8}
	if baseCPU >= 16 {
		nodeCounts = append(nodeCounts, 16)
	}
	if baseCPU >= 32 {
		nodeCounts = append(nodeCounts, 32)
	}

	testCases := []struct {
		name       string
		gomaxprocs int
	}{
		{"SingleCore", 1},
		{"MultiCore", baseCPU},
	}

	for _, nodeCount := range nodeCounts {
		for _, tc := range testCases {
			name := fmt.Sprintf("Nodes_%d/%s", nodeCount, tc.name)
			b.Run(name, func(b *testing.B) {
				// Set GOMAXPROCS
				oldMaxProcs := runtime.GOMAXPROCS(tc.gomaxprocs)
				defer runtime.GOMAXPROCS(oldMaxProcs)

				benchmarkBufferlessRing(b, nodeCount, 1000)
			})
		}
	}
}

// BenchmarkBufferlessRing_Throughput tests maximum throughput with different injection rates.
func BenchmarkBufferlessRing_Throughput(b *testing.B) {
	const nodeCount = 4
	injectionIntervals := []int{1, 2, 3, 5} // Inject every N cycles

	for _, interval := range injectionIntervals {
		name := fmt.Sprintf("InjectEvery_%dCycles", interval)
		b.Run(name, func(b *testing.B) {
			benchmarkBufferlessRingThroughput(b, nodeCount, 1000, interval)
		})
	}
}

// BenchmarkBufferlessRing_Backpressure tests performance under backpressure conditions.
func BenchmarkBufferlessRing_Backpressure(b *testing.B) {
	const nodeCount = 4
	const cycles = 1000

	b.Run("Normal", func(b *testing.B) {
		benchmarkBufferlessRing(b, nodeCount, cycles)
	})

	b.Run("WithBackpressure", func(b *testing.B) {
		benchmarkBufferlessRingBackpressure(b, nodeCount, cycles)
	})
}

// BenchmarkBufferlessRing_BufferSize tests impact of different router buffer sizes.
func BenchmarkBufferlessRing_BufferSize(b *testing.B) {
	const nodeCount = 4
	const cycles = 1000
	bufferSizes := []int{1, 2, 4, 8, 16}

	for _, bufSize := range bufferSizes {
		name := fmt.Sprintf("Buffer_%d", bufSize)
		b.Run(name, func(b *testing.B) {
			benchmarkBufferlessRingBufferSize(b, nodeCount, cycles, bufSize)
		})
	}
}

// benchmarkBufferlessRing is the core benchmark function.
func benchmarkBufferlessRing(b *testing.B, nodeCount, cycles int) {
	const (
		ringLatency    = 5
		routerBuffer   = 4
		queueSize      = 8
		queueBandwidth = 2
		injectInterval = 3 // Inject packet every 3 cycles
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Build network
		routers, workers, queues, ringLinks := buildBufferlessRingNetworkBench(
			b, nodeCount, ringLatency, routerBuffer, queueSize, queueBandwidth,
		)

		// Setup packet injection from Worker0
		var injectedCount int64
		workers[0].SetProcessHook(func(_ context.Context, cycle uint64, buffer []packet.Packet) ([]packet.Packet, error) {
			// Inject new packet every N cycles
			if cycle%injectInterval == 0 && int(cycle) < cycles {
				pkt := packet.Packet{
					SourceID: 0,
					TargetID: nodeCount - 1,
					Payload:  "perf-test",
				}
				if err := queues.WorkerOut[0].InjectPackets(int(cycle), []packet.Packet{pkt}); err == nil {
					atomic.AddInt64(&injectedCount, 1)
				}
			}
			return buffer, nil
		})

		// Setup packet reception at last worker
		var receivedCount int64
		queues.WorkerIn[nodeCount-1].SetPacketReceivedHook(func(pkt packet.Packet) {
			atomic.AddInt64(&receivedCount, 1)
		})

		// Run simulation
		ctx := context.Background()
		for cycle := 0; cycle < cycles; cycle++ {
			// Tick routers
			for _, r := range routers {
				if err := r.Tick(ctx, uint64(cycle), 0); err != nil {
					b.Fatalf("Router tick failed: %v", err)
				}
			}

			// Tick workers
			for _, w := range workers {
				if err := w.Tick(ctx, uint64(cycle), 0); err != nil {
					b.Fatalf("Worker tick failed: %v", err)
				}
			}

			// Tick queues
			allQueues := [][]*queue.InputQueue{queues.RingIn, queues.LocalIn, queues.WorkerIn}
			for _, queueList := range allQueues {
				for _, q := range queueList {
					if err := q.Tick(cycle); err != nil {
						b.Fatalf("Input queue tick failed: %v", err)
					}
				}
			}

			allOutputQueues := [][]*queue.OutputQueue{queues.RingOut, queues.LocalOut, queues.WorkerOut}
			for _, queueList := range allOutputQueues {
				for _, q := range queueList {
					if err := q.Tick(cycle); err != nil {
						b.Fatalf("Output queue tick failed: %v", err)
					}
				}
			}

			// Tick links
			for _, l := range ringLinks {
				if err := l.Tick(cycle); err != nil {
					b.Fatalf("Link tick failed: %v", err)
				}
			}
		}

		// Report stats (only for first iteration to avoid noise)
		if i == 0 {
			injected := atomic.LoadInt64(&injectedCount)
			received := atomic.LoadInt64(&receivedCount)
			b.ReportMetric(float64(injected), "injected")
			b.ReportMetric(float64(received), "received")
			b.ReportMetric(float64(cycles)/b.Elapsed().Seconds(), "cycles/sec")
		}
	}
}

// benchmarkBufferlessRingThroughput tests throughput with different injection rates.
func benchmarkBufferlessRingThroughput(b *testing.B, nodeCount, cycles, injectInterval int) {
	const (
		ringLatency    = 5
		routerBuffer   = 4
		queueSize      = 8
		queueBandwidth = 2
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		routers, workers, queues, ringLinks := buildBufferlessRingNetworkBench(
			b, nodeCount, ringLatency, routerBuffer, queueSize, queueBandwidth,
		)

		var injectedCount, receivedCount, droppedCount int64

		// Inject packets from Worker0
		workers[0].SetProcessHook(func(_ context.Context, cycle uint64, buffer []packet.Packet) ([]packet.Packet, error) {
			if cycle%uint64(injectInterval) == 0 && int(cycle) < cycles {
				pkt := packet.Packet{
					SourceID: 0,
					TargetID: nodeCount - 1,
					Payload:  fmt.Sprintf("pkt-%d", cycle),
				}
				if err := queues.WorkerOut[0].InjectPackets(int(cycle), []packet.Packet{pkt}); err == nil {
					atomic.AddInt64(&injectedCount, 1)
				} else {
					atomic.AddInt64(&droppedCount, 1)
				}
			}
			return buffer, nil
		})

		queues.WorkerIn[nodeCount-1].SetPacketReceivedHook(func(pkt packet.Packet) {
			atomic.AddInt64(&receivedCount, 1)
		})

		// Run simulation
		ctx := context.Background()
		runBufferlessRingSimulation(ctx, routers, workers, queues, ringLinks, cycles)

		if i == 0 {
			injected := atomic.LoadInt64(&injectedCount)
			received := atomic.LoadInt64(&receivedCount)
			dropped := atomic.LoadInt64(&droppedCount)
			b.ReportMetric(float64(injected), "injected")
			b.ReportMetric(float64(received), "received")
			b.ReportMetric(float64(dropped), "dropped")
			b.ReportMetric(float64(received)/float64(injected)*100, "delivery_%")
		}
	}
}

// benchmarkBufferlessRingBackpressure tests performance with backpressure.
func benchmarkBufferlessRingBackpressure(b *testing.B, nodeCount, cycles int) {
	const (
		ringLatency    = 5
		routerBuffer   = 4
		queueSize      = 8
		queueBandwidth = 2
		injectInterval = 2 // Aggressive injection
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		routers, workers, queues, ringLinks := buildBufferlessRingNetworkBench(
			b, nodeCount, ringLatency, routerBuffer, queueSize, queueBandwidth,
		)

		// Block Worker1 to create backpressure
		workers[1].SetPickHook(func(ctx context.Context, cycle uint64, defaultPick func() []packet.Packet) ([]packet.Packet, error) {
			return []packet.Packet{}, nil // Don't pick any packets
		})

		var injectedCount, circulatingCount int64

		// Inject packets targeting Worker1 (will circulate)
		workers[0].SetProcessHook(func(_ context.Context, cycle uint64, buffer []packet.Packet) ([]packet.Packet, error) {
			if cycle%injectInterval == 0 && int(cycle) < cycles/2 { // Only inject in first half
				pkt := packet.Packet{
					SourceID: 0,
					TargetID: 1, // Target blocked worker
					Payload:  fmt.Sprintf("circ-%d", cycle),
				}
				if err := queues.WorkerOut[0].InjectPackets(int(cycle), []packet.Packet{pkt}); err == nil {
					atomic.AddInt64(&injectedCount, 1)
				}
			}
			return buffer, nil
		})

		// Count packets stuck in ring
		ctx := context.Background()
		runBufferlessRingSimulation(ctx, routers, workers, queues, ringLinks, cycles)

		// Measure queue occupancy at end
		for _, q := range queues.RingIn {
			circulatingCount += int64(q.Length())
		}
		for _, q := range queues.LocalOut {
			circulatingCount += int64(q.Length())
		}

		if i == 0 {
			b.ReportMetric(float64(injectedCount), "injected")
			b.ReportMetric(float64(circulatingCount), "circulating")
		}
	}
}

// benchmarkBufferlessRingBufferSize tests different router buffer sizes.
func benchmarkBufferlessRingBufferSize(b *testing.B, nodeCount, cycles, bufferSize int) {
	const (
		ringLatency    = 5
		queueSize      = 8
		queueBandwidth = 2
		injectInterval = 3
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		routers, workers, queues, ringLinks := buildBufferlessRingNetworkBench(
			b, nodeCount, ringLatency, bufferSize, queueSize, queueBandwidth,
		)

		var injectedCount, receivedCount int64

		workers[0].SetProcessHook(func(_ context.Context, cycle uint64, buffer []packet.Packet) ([]packet.Packet, error) {
			if cycle%injectInterval == 0 && int(cycle) < cycles {
				pkt := packet.Packet{
					SourceID: 0,
					TargetID: nodeCount - 1,
					Payload:  "buf-test",
				}
				if err := queues.WorkerOut[0].InjectPackets(int(cycle), []packet.Packet{pkt}); err == nil {
					atomic.AddInt64(&injectedCount, 1)
				}
			}
			return buffer, nil
		})

		queues.WorkerIn[nodeCount-1].SetPacketReceivedHook(func(pkt packet.Packet) {
			atomic.AddInt64(&receivedCount, 1)
		})

		ctx := context.Background()
		runBufferlessRingSimulation(ctx, routers, workers, queues, ringLinks, cycles)

		if i == 0 {
			b.ReportMetric(float64(receivedCount), "received")
		}
	}
}

// buildBufferlessRingNetworkBench builds a bufferless ring network for benchmarking.
func buildBufferlessRingNetworkBench(b *testing.B, nodeCount, ringLatency, routerBuffer, queueSize, queueBandwidth int) (
	[]*BufferlessRingRouterNode,
	[]*Node,
	*QueueCollection,
	[]*link.Link,
) {
	// Create routers and workers
	routers := make([]*BufferlessRingRouterNode, nodeCount)
	workers := make([]*Node, nodeCount)

	for i := 0; i < nodeCount; i++ {
		routerID := 100 + i
		workerID := i
		routers[i] = NewBufferlessRingRouter(routerID, workerID, routerBuffer)
		workers[i] = New(workerID)
	}

	// Create queues
	ringInQueues := make([]*queue.InputQueue, nodeCount)
	ringOutQueues := make([]*queue.OutputQueue, nodeCount)
	localInQueues := make([]*queue.InputQueue, nodeCount)
	localOutQueues := make([]*queue.OutputQueue, nodeCount)
	workerInQueues := make([]*queue.InputQueue, nodeCount)
	workerOutQueues := make([]*queue.OutputQueue, nodeCount)

	for i := 0; i < nodeCount; i++ {
		ringInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth, queueBandwidth)
		ringOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		localInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth, queueBandwidth)
		localOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		workerInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth, queueBandwidth)
		workerOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)

		ringInQueues[i].EnableAlwaysReady()
		localInQueues[i].EnableAlwaysReady()
		workerInQueues[i].EnableAlwaysReady()
	}

	// Connect routers
	for i := 0; i < nodeCount; i++ {
		if err := routers[i].AddInputQueue(ringInQueues[i]); err != nil {
			b.Fatalf("Router %d AddInputQueue(ringIn): %v", i, err)
		}
		if err := routers[i].AddInputQueue(localInQueues[i]); err != nil {
			b.Fatalf("Router %d AddInputQueue(localIn): %v", i, err)
		}
		if err := routers[i].AddOutputQueue(ringOutQueues[i]); err != nil {
			b.Fatalf("Router %d AddOutputQueue(ringOut): %v", i, err)
		}
		if err := routers[i].AddOutputQueue(localOutQueues[i]); err != nil {
			b.Fatalf("Router %d AddOutputQueue(localOut): %v", i, err)
		}
	}

	// Connect workers
	for i := 0; i < nodeCount; i++ {
		if err := workers[i].AddInputQueue(workerInQueues[i]); err != nil {
			b.Fatalf("Worker %d AddInputQueue: %v", i, err)
		}
		if err := workers[i].AddOutputQueue(workerOutQueues[i]); err != nil {
			b.Fatalf("Worker %d AddOutputQueue: %v", i, err)
		}
	}

	// Create ring links
	ringLinks := make([]*link.Link, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nextRouter := (i + 1) % nodeCount
		sourceID := 100 + i
		targetID := 100 + nextRouter

		fc := link.NewBufferlessFlowControl()
		ringLink, linkIn, linkOut := link.NewLinkWithFlowControl(sourceID, targetID, ringLatency, 1, fc)
		ringLinks[i] = ringLink

		linkIn.Plug(ringOutQueues[i].QueueOutPort())
		linkOut.Plug(ringInQueues[nextRouter].AsInPort())
	}

	// Create local connections
	for i := 0; i < nodeCount; i++ {
		workerOutQueues[i].QueueOutPort().Plug(localInQueues[i].QueueInPort())
		localOutQueues[i].QueueOutPort().Plug(workerInQueues[i].QueueInPort())
	}

	queues := &QueueCollection{
		RingIn:    ringInQueues,
		RingOut:   ringOutQueues,
		LocalIn:   localInQueues,
		LocalOut:  localOutQueues,
		WorkerIn:  workerInQueues,
		WorkerOut: workerOutQueues,
	}

	return routers, workers, queues, ringLinks
}

// runBufferlessRingSimulation runs the simulation for the given number of cycles.
func runBufferlessRingSimulation(
	ctx context.Context,
	routers []*BufferlessRingRouterNode,
	workers []*Node,
	queues *QueueCollection,
	ringLinks []*link.Link,
	cycles int,
) {
	for cycle := 0; cycle < cycles; cycle++ {
		// Tick routers
		for _, r := range routers {
			r.Tick(ctx, uint64(cycle), 0)
		}

		// Tick workers
		for _, w := range workers {
			w.Tick(ctx, uint64(cycle), 0)
		}

		// Tick queues
		allQueues := [][]*queue.InputQueue{queues.RingIn, queues.LocalIn, queues.WorkerIn}
		for _, queueList := range allQueues {
			for _, q := range queueList {
				q.Tick(cycle)
			}
		}

		allOutputQueues := [][]*queue.OutputQueue{queues.RingOut, queues.LocalOut, queues.WorkerOut}
		for _, queueList := range allOutputQueues {
			for _, q := range queueList {
				q.Tick(cycle)
			}
		}

		// Tick links
		for _, l := range ringLinks {
			l.Tick(cycle)
		}
	}
}

// mockDelay simulates processing delay (similar to network tests).
func mockDelay(minUs, maxUs int) {
	delayUs := minUs
	if maxUs > minUs {
		delayUs += rand.Intn(maxUs - minUs)
	}
	if delayUs > 0 {
		time.Sleep(time.Duration(delayUs) * time.Microsecond)
	}
}

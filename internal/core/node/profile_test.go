package node

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestProfileBufferlessRing is a profiling test that runs for a long time.
// Run with: go test -run=TestProfileBufferlessRing -cpuprofile=cpu.prof -mutexprofile=mutex.prof -blockprofile=block.prof
func TestProfileBufferlessRing(t *testing.T) {
	const (
		nodeCount      = 4
		cycles         = 100000 // Run many cycles for profiling
		ringLatency    = 5
		routerBuffer   = 4
		queueSize      = 8
		queueBandwidth = 2
		injectInterval = 3
	)

	// Enable profiling
	runtime.SetMutexProfileFraction(1)
	runtime.SetBlockProfileRate(1)

	// Build network
	routers, workers, queues, ringLinks := buildBufferlessRingNetworkTest(
		t, nodeCount, ringLatency, routerBuffer, queueSize, queueBandwidth,
	)

	// Setup packet injection from Worker0
	var injectedCount int64
	workers[0].SetProcessHook(func(_ context.Context, cycle uint64, buffer []packet.Packet) ([]packet.Packet, error) {
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

	// Setup packet reception
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
				t.Fatalf("Router tick failed: %v", err)
			}
		}

		// Tick workers
		for _, w := range workers {
			if err := w.Tick(ctx, uint64(cycle), 0); err != nil {
				t.Fatalf("Worker tick failed: %v", err)
			}
		}

		// Tick queues
		allQueues := [][]*queue.InputQueue{
			queues.RingIn,
			queues.LocalIn,
			queues.WorkerIn,
		}
		for _, queueGroup := range allQueues {
			for _, q := range queueGroup {
				if err := q.Tick(cycle); err != nil {
					t.Fatalf("InputQueue tick failed: %v", err)
				}
			}
		}

		allOutQueues := [][]*queue.OutputQueue{
			queues.RingOut,
			queues.LocalOut,
			queues.WorkerOut,
		}
		for _, queueGroup := range allOutQueues {
			for _, q := range queueGroup {
				if err := q.Tick(cycle); err != nil {
					t.Fatalf("OutputQueue tick failed: %v", err)
				}
			}
		}

		// Tick links
		for _, l := range ringLinks {
			if err := l.Tick(cycle); err != nil {
				t.Fatalf("Link tick failed: %v", err)
			}
		}
	}

	t.Logf("Completed %d cycles", cycles)
	t.Logf("Injected: %d, Received: %d", injectedCount, receivedCount)
}

// buildBufferlessRingNetworkTest builds the network for testing.
func buildBufferlessRingNetworkTest(
	t *testing.T,
	nodeCount, ringLatency, routerBuffer, queueSize, queueBandwidth int,
) (
	[]*BufferlessRingRouterNode,
	[]*Node,
	*QueueCollection,
	[]*link.Link,
) {
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
			t.Fatalf("Router %d AddInputQueue(ringIn): %v", i, err)
		}
		if err := routers[i].AddInputQueue(localInQueues[i]); err != nil {
			t.Fatalf("Router %d AddInputQueue(localIn): %v", i, err)
		}
		if err := routers[i].AddOutputQueue(ringOutQueues[i]); err != nil {
			t.Fatalf("Router %d AddOutputQueue(ringOut): %v", i, err)
		}
		if err := routers[i].AddOutputQueue(localOutQueues[i]); err != nil {
			t.Fatalf("Router %d AddOutputQueue(localOut): %v", i, err)
		}
	}

	// Connect workers
	for i := 0; i < nodeCount; i++ {
		if err := workers[i].AddInputQueue(workerInQueues[i]); err != nil {
			t.Fatalf("Worker %d AddInputQueue: %v", i, err)
		}
		if err := workers[i].AddOutputQueue(workerOutQueues[i]); err != nil {
			t.Fatalf("Worker %d AddOutputQueue: %v", i, err)
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

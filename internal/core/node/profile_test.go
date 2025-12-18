package node

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
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
	workers[0].SetProcessHook(func(_ context.Context, cycle uint64, _ [][]packet.Packet) error {
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
		return nil
	})

	// Setup packet reception
	var receivedCount int64
	queues.WorkerIn[nodeCount-1].SetPacketReceivedHook(func(pkt packet.Packet) {
		atomic.AddInt64(&receivedCount, 1)
	})

	// Run simulation
	ctx := context.Background()
	for cycle := 0; cycle < cycles; cycle++ {
		// Tick links first
		for _, l := range ringLinks {
			if err := l.Tick(cycle); err != nil {
				t.Fatalf("Link tick failed: %v", err)
			}
		}

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
	[]*TestNode,
	*QueueCollection,
	[]*link.Link,
) {
	routers := make([]*BufferlessRingRouterNode, nodeCount)
	workers := make([]*TestNode, nodeCount)

	for i := 0; i < nodeCount; i++ {
		routerID := 100 + i
		workerID := i
		routers[i] = NewBufferlessRingRouter(routerID, workerID, routerBuffer)
		workers[i] = NewTestNode(workerID)
	}

	// Create queues
	ringInQueues := make([]*queue.InputQueue, nodeCount)
	ringOutQueues := make([]*queue.OutputQueue, nodeCount)
	localInQueues := make([]*queue.InputQueue, nodeCount)
	localOutQueues := make([]*queue.OutputQueue, nodeCount)
	workerInQueues := make([]*queue.InputQueue, nodeCount)
	workerOutQueues := make([]*queue.OutputQueue, nodeCount)

	for i := 0; i < nodeCount; i++ {
		ringInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		ringOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		localInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		localOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		workerInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		workerOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
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
		ringLink := link.NewLinkWithFlowControl(sourceID, targetID, ringLatency, 1, fc)
		ringLinks[i] = ringLink

		// OutputQueue -> Link
		p1 := ahead_port.NewPort()
		ringOutQueues[i].SetDownstreamPort(p1.AsInPort())
		ringLink.SetUpstreamPort(p1.AsOutPort())

		// Link -> InputQueue
		p2 := ahead_port.NewPort()
		ringLink.SetDownstreamPort(p2.AsInPort())
		ringInQueues[nextRouter].SetUpstreamPort(p2.AsOutPort())
	}

	// Create local connections (Needs latency to avoid combinational loop Deadlock)
	for i := 0; i < nodeCount; i++ {
		// Worker -> Router
		fc1 := link.NewBufferlessFlowControl()
		l1 := link.NewLinkWithFlowControl(i, 100+i, 1, queueBandwidth, fc1)

		p1_out := ahead_port.NewPort()
		workerOutQueues[i].SetDownstreamPort(p1_out.AsInPort())
		l1.SetUpstreamPort(p1_out.AsOutPort())

		p1_in := ahead_port.NewPort()
		l1.SetDownstreamPort(p1_in.AsInPort())
		localInQueues[i].SetUpstreamPort(p1_in.AsOutPort())

		// Router -> Worker
		fc2 := link.NewBufferlessFlowControl()
		l2 := link.NewLinkWithFlowControl(100+i, i, 1, queueBandwidth, fc2)

		p2_out := ahead_port.NewPort()
		localOutQueues[i].SetDownstreamPort(p2_out.AsInPort())
		l2.SetUpstreamPort(p2_out.AsOutPort())

		p2_in := ahead_port.NewPort()
		l2.SetDownstreamPort(p2_in.AsInPort())
		workerInQueues[i].SetUpstreamPort(p2_in.AsOutPort())
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

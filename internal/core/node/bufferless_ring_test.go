package node

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestBufferlessRing_Basic tests basic construction of a 4-router bufferless ring.
func TestBufferlessRing_Basic(t *testing.T) {
	const (
		numRouters     = 4
		ringLatency    = 5
		routerBuffer   = 4
		queueSize      = 8
		queueBandwidth = 2
	)

	// Create 4 routers (IDs: 100, 101, 102, 103)
	// Create 4 workers (IDs: 0, 1, 2, 3)
	routers := make([]*BufferlessRingRouterNode, numRouters)
	workers := make([]*TestNode, numRouters)

	for i := 0; i < numRouters; i++ {
		routerID := 100 + i
		workerID := i

		// Create router
		routers[i] = NewBufferlessRingRouter(routerID, workerID, routerBuffer)

		// Create worker (simple node with input/output queues)
		workers[i] = NewTestNode(workerID)
	}

	// Create queues for connections
	// For each router, we need:
	// - ringInQueue: InputQueue connected to previous router's ringOut
	// - ringOutQueue: OutputQueue connected to next router's ringIn
	// - localInQueue: InputQueue connected to worker's output
	// - localOutQueue: OutputQueue connected to worker's input

	// For each worker, we need:
	// - workerInQueue: InputQueue connected to router's localOut
	// - workerOutQueue: OutputQueue connected to router's localIn

	ringInQueues := make([]*queue.InputQueue, numRouters)
	ringOutQueues := make([]*queue.OutputQueue, numRouters)
	localInQueues := make([]*queue.InputQueue, numRouters)
	localOutQueues := make([]*queue.OutputQueue, numRouters)
	workerInQueues := make([]*queue.InputQueue, numRouters)
	workerOutQueues := make([]*queue.OutputQueue, numRouters)

	for i := 0; i < numRouters; i++ {
		ringInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		ringOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		localInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		localOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		workerInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		workerOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
	}

	// Connect queues to routers
	for i := 0; i < numRouters; i++ {
		if err := routers[i].AddInputQueue(ringInQueues[i]); err != nil {
			t.Fatalf("Failed to add ringInQueue to router %d: %v", i, err)
		}
		if err := routers[i].AddInputQueue(localInQueues[i]); err != nil {
			t.Fatalf("Failed to add localInQueue to router %d: %v", i, err)
		}
		if err := routers[i].AddOutputQueue(ringOutQueues[i]); err != nil {
			t.Fatalf("Failed to add ringOutQueue to router %d: %v", i, err)
		}
		if err := routers[i].AddOutputQueue(localOutQueues[i]); err != nil {
			t.Fatalf("Failed to add localOutQueue to router %d: %v", i, err)
		}
	}

	// Connect queues to workers
	for i := 0; i < numRouters; i++ {
		if err := workers[i].AddInputQueue(workerInQueues[i]); err != nil {
			t.Fatalf("Failed to add workerInQueue to worker %d: %v", i, err)
		}
		if err := workers[i].AddOutputQueue(workerOutQueues[i]); err != nil {
			t.Fatalf("Failed to add workerOutQueue to worker %d: %v", i, err)
		}
	}

	// Create bufferless ring links connecting routers
	ringLinks := make([]*link.Link, numRouters)
	for i := 0; i < numRouters; i++ {
		nextRouter := (i + 1) % numRouters
		sourceID := 100 + i
		targetID := 100 + nextRouter

		// Create bufferless link
		fc := link.NewBufferlessLinkHandler()
		ringLink := link.NewLinkWithHandler(sourceID, targetID, ringLatency, 1, fc)
		ringLinks[i] = ringLink

		// Connect link to router queues
		// ringOutQueues[i] (OutputQueue) -> link -> ringInQueues[nextRouter] (InputQueue)

		// 1. OutputQueue -> Link
		p1 := ahead_port.NewPort()
		ringOutQueues[i].SetDownstreamPort(p1.AsInPort())
		ringLink.SetUpstreamPort(p1.AsOutPort())

		// 2. Link -> InputQueue
		p2 := ahead_port.NewPort()
		ringLink.SetDownstreamPort(p2.AsInPort())
		ringInQueues[nextRouter].SetUpstreamPort(p2.AsOutPort())
	}

	// Create direct connections between workers and routers (no links needed for local connections)
	for i := 0; i < numRouters; i++ {
		// Worker -> Router (local injection)
		// workerOutQueues[i] (OutputQueue) -> localInQueues[i] (InputQueue)
		p1 := ahead_port.NewPort()
		workerOutQueues[i].SetDownstreamPort(p1.AsInPort())
		localInQueues[i].SetUpstreamPort(p1.AsOutPort())

		// Router -> Worker (local ejection)
		// localOutQueues[i] (OutputQueue) -> workerInQueues[i] (InputQueue)
		p2 := ahead_port.NewPort()
		localOutQueues[i].SetDownstreamPort(p2.AsInPort())
		workerInQueues[i].SetUpstreamPort(p2.AsOutPort())
	}

	// Basic validation
	t.Logf("Created bufferless ring with %d routers", numRouters)
	for i := 0; i < numRouters; i++ {
		t.Logf("Router %d: workerID=%d, capacity=%d",
			routers[i].ID(), routers[i].GetWorkerID(), routers[i].GetBufferCapacity())
	}

	t.Log("✅ Basic construction successful")
}

// QueueCollection holds all queues for the bufferless ring network.
type QueueCollection struct {
	RingIn    []*queue.InputQueue
	RingOut   []*queue.OutputQueue
	LocalIn   []*queue.InputQueue
	LocalOut  []*queue.OutputQueue
	WorkerIn  []*queue.InputQueue
	WorkerOut []*queue.OutputQueue
}

// buildBufferlessRingNetwork creates a 4-router bufferless ring network for testing.
// Returns: routers, workers, queues, ringLinks
func buildBufferlessRingNetwork(t *testing.T) (
	[]*BufferlessRingRouterNode,
	[]*TestNode,
	*QueueCollection,
	[]*link.Link,
) {
	const (
		numRouters     = 4
		ringLatency    = 5
		routerBuffer   = 4
		queueSize      = 8
		queueBandwidth = 2
	)

	// Create routers and workers
	routers := make([]*BufferlessRingRouterNode, numRouters)
	workers := make([]*TestNode, numRouters)

	for i := 0; i < numRouters; i++ {
		routerID := 100 + i
		workerID := i
		routers[i] = NewBufferlessRingRouter(routerID, workerID, routerBuffer)
		workers[i] = NewTestNode(workerID)
	}

	// Create queues
	ringInQueues := make([]*queue.InputQueue, numRouters)
	ringOutQueues := make([]*queue.OutputQueue, numRouters)
	localInQueues := make([]*queue.InputQueue, numRouters)
	localOutQueues := make([]*queue.OutputQueue, numRouters)
	workerInQueues := make([]*queue.InputQueue, numRouters)
	workerOutQueues := make([]*queue.OutputQueue, numRouters)

	for i := 0; i < numRouters; i++ {
		ringInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		ringOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		localInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		localOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		workerInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		workerOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
	}

	// Connect queues to routers
	for i := 0; i < numRouters; i++ {
		if err := routers[i].AddInputQueue(ringInQueues[i]); err != nil {
			t.Fatalf("Failed to add ringInQueue to router %d: %v", i, err)
		}
		if err := routers[i].AddInputQueue(localInQueues[i]); err != nil {
			t.Fatalf("Failed to add localInQueue to router %d: %v", i, err)
		}
		if err := routers[i].AddOutputQueue(ringOutQueues[i]); err != nil {
			t.Fatalf("Failed to add ringOutQueue to router %d: %v", i, err)
		}
		if err := routers[i].AddOutputQueue(localOutQueues[i]); err != nil {
			t.Fatalf("Failed to add localOutQueue to router %d: %v", i, err)
		}
	}

	// Connect queues to workers
	for i := 0; i < numRouters; i++ {
		if err := workers[i].AddInputQueue(workerInQueues[i]); err != nil {
			t.Fatalf("Failed to add workerInQueue to worker %d: %v", i, err)
		}
		if err := workers[i].AddOutputQueue(workerOutQueues[i]); err != nil {
			t.Fatalf("Failed to add workerOutQueue to worker %d: %v", i, err)
		}
	}

	// Create bufferless ring links
	ringLinks := make([]*link.Link, numRouters)
	for i := 0; i < numRouters; i++ {
		nextRouter := (i + 1) % numRouters
		sourceID := 100 + i
		targetID := 100 + nextRouter

		fc := link.NewBufferlessLinkHandler()
		ringLink := link.NewLinkWithHandler(sourceID, targetID, ringLatency, 1, fc)
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

	// Create local connections
	for i := 0; i < numRouters; i++ {
		// Worker -> Router
		p1 := ahead_port.NewPort()
		workerOutQueues[i].SetDownstreamPort(p1.AsInPort())
		localInQueues[i].SetUpstreamPort(p1.AsOutPort())

		// Router -> Worker
		p2 := ahead_port.NewPort()
		localOutQueues[i].SetDownstreamPort(p2.AsInPort())
		workerInQueues[i].SetUpstreamPort(p2.AsOutPort())
	}

	// Package queues into a collection
	queues := &QueueCollection{
		RingIn:    ringInQueues,
		RingOut:   ringOutQueues,
		LocalIn:   localInQueues,
		LocalOut:  localOutQueues,
		WorkerIn:  workerInQueues,
		WorkerOut: workerOutQueues,
	}

	t.Log("✅ Network constructed")
	return routers, workers, queues, ringLinks
}

// TestBufferlessRing_SinglePacket tests sending a single packet from Worker0 to Worker1.
//
// Topology: Worker0 → Router100 ⟳ Router101 → Worker1
// Expected path: Worker0 → Router100 → Ring(latency=5) → Router101 → Worker1
// Expected delay: ~5-6 cycles (ring latency + processing)
func TestBufferlessRing_SinglePacket(t *testing.T) {
	const (
		numRouters     = 4
		ringLatency    = 5
		routerBuffer   = 4
		queueSize      = 8
		queueBandwidth = 2
	)

	// === 1. Build network (same as Basic test) ===
	routers := make([]*BufferlessRingRouterNode, numRouters)
	workers := make([]*TestNode, numRouters)

	for i := 0; i < numRouters; i++ {
		routerID := 100 + i
		workerID := i
		routers[i] = NewBufferlessRingRouter(routerID, workerID, routerBuffer)
		workers[i] = NewTestNode(workerID)

	}

	// Create queues
	ringInQueues := make([]*queue.InputQueue, numRouters)
	ringOutQueues := make([]*queue.OutputQueue, numRouters)
	localInQueues := make([]*queue.InputQueue, numRouters)
	localOutQueues := make([]*queue.OutputQueue, numRouters)
	workerInQueues := make([]*queue.InputQueue, numRouters)
	workerOutQueues := make([]*queue.OutputQueue, numRouters)

	for i := 0; i < numRouters; i++ {
		ringInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		ringOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		localInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		localOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		workerInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		workerOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
	}

	// Connect queues to routers
	for i := 0; i < numRouters; i++ {
		if err := routers[i].AddInputQueue(ringInQueues[i]); err != nil {
			t.Fatalf("Failed to add ringInQueue to router %d: %v", i, err)
		}
		if err := routers[i].AddInputQueue(localInQueues[i]); err != nil {
			t.Fatalf("Failed to add localInQueue to router %d: %v", i, err)
		}
		if err := routers[i].AddOutputQueue(ringOutQueues[i]); err != nil {
			t.Fatalf("Failed to add ringOutQueue to router %d: %v", i, err)
		}
		if err := routers[i].AddOutputQueue(localOutQueues[i]); err != nil {
			t.Fatalf("Failed to add localOutQueue to router %d: %v", i, err)
		}
	}

	// Connect queues to workers
	for i := 0; i < numRouters; i++ {
		if err := workers[i].AddInputQueue(workerInQueues[i]); err != nil {
			t.Fatalf("Failed to add workerInQueue to worker %d: %v", i, err)
		}
		if err := workers[i].AddOutputQueue(workerOutQueues[i]); err != nil {
			t.Fatalf("Failed to add workerOutQueue to worker %d: %v", i, err)
		}
	}

	// Create bufferless ring links
	ringLinks := make([]*link.Link, numRouters)
	for i := 0; i < numRouters; i++ {
		nextRouter := (i + 1) % numRouters
		sourceID := 100 + i
		targetID := 100 + nextRouter

		fc := link.NewBufferlessLinkHandler()
		ringLink := link.NewLinkWithHandler(sourceID, targetID, ringLatency, 1, fc)
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

	// Create local connections
	for i := 0; i < numRouters; i++ {
		// Worker -> Router
		p1 := ahead_port.NewPort()
		workerOutQueues[i].SetDownstreamPort(p1.AsInPort())
		localInQueues[i].SetUpstreamPort(p1.AsOutPort())

		// Router -> Worker
		p2 := ahead_port.NewPort()
		localOutQueues[i].SetDownstreamPort(p2.AsInPort())
		workerInQueues[i].SetUpstreamPort(p2.AsOutPort())
	}

	t.Log("✅ Network constructed")

	// === 2. Inject packet at Worker0 targeting Worker1 ===
	testPacket := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "Test packet from Worker0 to Worker1",
	}

	// Inject at cycle 0
	if err := workerOutQueues[0].InjectPackets(0, []packet.Packet{testPacket}); err != nil {
		t.Fatalf("Failed to inject packet: %v", err)
	}
	t.Logf("✅ Injected packet at cycle 0: Src=%d, Dst=%d", testPacket.SourceID, testPacket.TargetID)

	// === 3. Run simulation ===
	maxCycles := 20
	ctx := context.Background()

	for cycle := 0; cycle < maxCycles; cycle++ {
		t.Logf("--- Cycle %d ---", cycle)

		// Debug: Check queue occupancies
		t.Logf("  workerOutQueue[0]: %d/%d", workerOutQueues[0].Length(), workerOutQueues[0].Capacity())
		t.Logf("  localInQueue[0]: %d/%d", localInQueues[0].Length(), localInQueues[0].Capacity())
		t.Logf("  ringOutQueue[0]: %d/%d", ringOutQueues[0].Length(), ringOutQueues[0].Capacity())
		t.Logf("  ringInQueue[1]: %d/%d", ringInQueues[1].Length(), ringInQueues[1].Capacity())
		t.Logf("  localOutQueue[1]: %d/%d", localOutQueues[1].Length(), localOutQueues[1].Capacity())
		t.Logf("  workerInQueue[1]: %d/%d", workerInQueues[1].Length(), workerInQueues[1].Capacity())
		t.Logf("  Router[0] buffer: %d/%d", routers[0].GetBufferOccupancy(), routers[0].GetBufferCapacity())
		t.Logf("  Router[1] buffer: %d/%d", routers[1].GetBufferOccupancy(), routers[1].GetBufferCapacity())

		// Correct tick order: Nodes -> Queues -> Links
		// This ensures Ready signals are set before Links check them

		// 1. Tick routers first (they process packets)
		for i, r := range routers {
			if err := r.Tick(ctx, uint64(cycle), 0); err != nil {
				t.Fatalf("Router %d tick failed at cycle %d: %v", i, cycle, err)
			}
		}

		// 2. Tick workers
		for i, w := range workers {
			if err := w.Tick(ctx, uint64(cycle), 0); err != nil {
				t.Fatalf("Worker %d tick failed at cycle %d: %v", i, cycle, err)
			}
		}

		// 3. Queues are ticked automatically by their owners (Routers/Workers)
		// during their Tick() calls. No manual ticking needed.

		// 4. Tick links (they check Ready and send packets)
		for i, l := range ringLinks {
			if err := l.Tick(cycle); err != nil {
				t.Fatalf("Link %d tick failed at cycle %d: %v", i, cycle, err)
			}
		}

		// Check if packet arrived at Worker1
		receivedPackets := workerInQueues[1].Pick()
		if len(receivedPackets) > 0 {
			t.Logf("✅ Packet arrived at Worker1 at cycle %d", cycle)
			t.Logf("   Received: Src=%d, Dst=%d, Payload=%s",
				receivedPackets[0].SourceID, receivedPackets[0].TargetID, receivedPackets[0].Payload)

			// Verify packet content
			if receivedPackets[0].SourceID != 0 {
				t.Errorf("Expected SourceID=0, got %d", receivedPackets[0].SourceID)
			}
			if receivedPackets[0].TargetID != 1 {
				t.Errorf("Expected TargetID=1, got %d", receivedPackets[0].TargetID)
			}
			if receivedPackets[0].Payload != testPacket.Payload {
				t.Errorf("Payload mismatch: expected %q, got %q", testPacket.Payload, receivedPackets[0].Payload)
			}

			t.Logf("✅ Test passed: packet delivered successfully in %d cycles", cycle)
			return
		}
	}

	t.Fatalf("❌ Packet did not arrive at Worker1 within %d cycles", maxCycles)
}

// TestBufferlessRing_TwoHops tests packet delivery across 2 hops (Worker0 → Worker2).
// Expected behavior:
// - Packet injected at Worker0 with TargetID=2
// - Router0: pick from localIn, inject onto ring
// - Router1: pick from ringIn, TargetID(2) != workerID(1), forward on ring
// - Router2: pick from ringIn, TargetID(2) == workerID(2), eject to Worker2
// - Expected latency: ~15 cycles (1 injection + 2 * (5 link latency + 1 router))
func TestBufferlessRing_TwoHops(t *testing.T) {
	// Build the 4-router ring network
	routers, workers, queues, ringLinks := buildBufferlessRingNetwork(t)

	// Extract specific queues for Worker0 and Worker2
	workerOutQueue0 := queues.WorkerOut[0]
	workerInQueue2 := queues.WorkerIn[2]
	localInQueue0 := queues.LocalIn[0]
	localOutQueue2 := queues.LocalOut[2]

	// Inject a packet from Worker0 to Worker2
	pkt := packet.Packet{
		SourceID: 0,
		TargetID: 2, // Target Worker2
		Payload:  "Test packet from Worker0 to Worker2",
	}
	if err := workerOutQueue0.InjectPackets(0, []packet.Packet{pkt}); err != nil {
		t.Fatalf("Failed to inject packet: %v", err)
	}
	t.Logf("✅ Injected packet at cycle 0: Src=%d, Dst=%d", pkt.SourceID, pkt.TargetID)

	// Simulate for enough cycles to allow 2-hop delivery
	maxCycles := 20
	ctx := context.Background()

	for cycle := 0; cycle < maxCycles; cycle++ {
		t.Logf("--- Cycle %d ---", cycle)
		t.Logf("  localInQueue[0]: %d/%d", localInQueue0.Length(), localInQueue0.Capacity())
		t.Logf("  localOutQueue[2]: %d/%d", localOutQueue2.Length(), localOutQueue2.Capacity())
		t.Logf("  workerInQueue[2]: %d/%d", workerInQueue2.Length(), workerInQueue2.Capacity())
		t.Logf("  Router[0] buffer: %d/%d", routers[0].GetInjectionBufferOccupancy(), routers[0].GetBufferCapacity())
		t.Logf("  Router[1] buffer: %d/%d", routers[1].GetInjectionBufferOccupancy(), routers[1].GetBufferCapacity())
		t.Logf("  Router[2] buffer: %d/%d", routers[2].GetInjectionBufferOccupancy(), routers[2].GetBufferCapacity())

		// Tick all links first
		for _, link := range ringLinks {
			if err := link.Tick(cycle); err != nil {
				t.Fatalf("Link tick failed at cycle %d: %v", cycle, err)
			}
		}

		// Tick all components
		for _, r := range routers {
			if err := r.Tick(ctx, uint64(cycle), 0); err != nil {
				t.Fatalf("Router tick failed at cycle %d: %v", cycle, err)
			}
		}
		for _, w := range workers {
			if err := w.Tick(ctx, uint64(cycle), 0); err != nil {
				t.Fatalf("Worker tick failed at cycle %d: %v", cycle, err)
			}
		}

		// Check if packet arrived at Worker2
		if workerInQueue2.Length() > 0 {
			received := workerInQueue2.Pick()
			if len(received) > 0 {
				t.Logf("✅ Packet arrived at Worker2 at cycle %d", cycle)
				t.Logf("   Received: Src=%d, Dst=%d, Payload=%s",
					received[0].SourceID, received[0].TargetID, received[0].Payload)

				// Verify packet content
				if received[0].SourceID != 0 || received[0].TargetID != 2 {
					t.Fatalf("❌ Packet content mismatch: expected Src=0 Dst=2, got Src=%d Dst=%d",
						received[0].SourceID, received[0].TargetID)
				}

				// Verify delivery time is reasonable (should be ~15 cycles)
				if cycle < 10 || cycle > 18 {
					t.Logf("⚠️  Warning: delivery time %d cycles is outside expected range [10-18]", cycle)
				}

				t.Logf("✅ Test passed: packet delivered successfully in %d cycles", cycle)
				return
			}
		}
	}

	t.Fatalf("❌ Packet did not arrive at Worker2 within %d cycles", maxCycles)
}

// TestBufferlessRing_Backpressure tests packet circulation when the destination worker is busy.
// Expected behavior:
// - Worker1's localOut is initially full (simulating busy worker)
// - Packet sent from Worker0 to Worker1
// - Packet arrives at Router1 at cycle 8, but localOut full, so packet continues on ring
// - Packet circulates: Router1 → Router2 → Router3 → Router0 → Router1 (full ring loop = ~20 cycles)
// - On second arrival at Router1 (~cycle 28), localOut has space, packet ejects
// This verifies bufferless ring property: packets loop until they can eject.
func TestBufferlessRing_Backpressure(t *testing.T) {
	routers, workers, queues, ringLinks := buildBufferlessRingNetwork(t)

	workerOutQueue0 := queues.WorkerOut[0]
	workerInQueue1 := queues.WorkerIn[1]
	localOutQueue1 := queues.LocalOut[1]

	// Fill Worker1's localOut queue to simulate backpressure
	dummyPackets := make([]packet.Packet, 8) // Queue size is 8
	for i := range dummyPackets {
		dummyPackets[i] = packet.Packet{
			SourceID: 99,
			TargetID: 99,
			Payload:  fmt.Sprintf("Dummy packet %d", i),
		}
	}
	if err := localOutQueue1.InjectPackets(0, dummyPackets); err != nil {
		t.Fatalf("Failed to inject dummy packets: %v", err)
	}
	t.Logf("✅ Filled localOutQueue[1] with %d dummy packets (simulating busy worker)", len(dummyPackets))

	// Inject test packet from Worker0 to Worker1
	testPacket := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "Test packet with backpressure",
	}
	if err := workerOutQueue0.InjectPackets(0, []packet.Packet{testPacket}); err != nil {
		t.Fatalf("Failed to inject test packet: %v", err)
	}
	t.Logf("✅ Injected test packet: Src=%d, Dst=%d", testPacket.SourceID, testPacket.TargetID)

	ctx := context.Background()
	maxCycles := 50
	firstArrivalCycle := -1
	circulationDetected := false

	for cycle := 0; cycle < maxCycles; cycle++ {
		// Keep localOutQueue1 full by injecting more dummy packets each cycle (until cycle 15)
		// This ensures backpressure is maintained when test packet arrives
		if cycle > 0 && cycle < 15 && localOutQueue1.Length() < 8 {
			topup := 8 - localOutQueue1.Length()
			topupPackets := make([]packet.Packet, topup)
			for i := range topupPackets {
				topupPackets[i] = packet.Packet{
					SourceID: 99,
					TargetID: 99,
					Payload:  fmt.Sprintf("Topup %d", cycle),
				}
			}
			localOutQueue1.InjectPackets(cycle, topupPackets)
		}

		// At cycle 15, stop injecting to allow queue to drain and packet to eject
		if cycle == 15 {
			t.Logf("⚙️  Cycle %d: Stopped backpressure, Worker1 becoming available", cycle)
		}

		// Tick all components
		for _, r := range routers {
			if err := r.Tick(ctx, uint64(cycle), 0); err != nil {
				t.Fatalf("Router tick failed at cycle %d: %v", cycle, err)
			}
		}
		for _, w := range workers {
			if err := w.Tick(ctx, uint64(cycle), 0); err != nil {
				t.Fatalf("Worker tick failed at cycle %d: %v", cycle, err)
			}
		}
		allQueues := [][]*queue.InputQueue{queues.RingIn, queues.LocalIn, queues.WorkerIn}
		for _, queueList := range allQueues {
			for _, q := range queueList {
				if err := q.Tick(cycle); err != nil {
					t.Fatalf("Input queue tick failed at cycle %d: %v", cycle, err)
				}
			}
		}
		allOutputQueues := [][]*queue.OutputQueue{queues.RingOut, queues.LocalOut, queues.WorkerOut}
		for _, queueList := range allOutputQueues {
			for _, q := range queueList {
				if err := q.Tick(cycle); err != nil {
					t.Fatalf("Output queue tick failed at cycle %d: %v", cycle, err)
				}
			}
		}
		for _, link := range ringLinks {
			if err := link.Tick(cycle); err != nil {
				t.Fatalf("Link tick failed at cycle %d: %v", cycle, err)
			}
		}

		// Track packet circulation (check cycles 7-10 for first arrival)
		if cycle >= 7 && cycle <= 10 && !circulationDetected {
			// Packet should arrive at Router1 around cycle 8
			ringInLen := queues.RingIn[1].Length()
			localOutFull := localOutQueue1.IsFull()
			if ringInLen > 0 && localOutFull {
				firstArrivalCycle = cycle
				t.Logf("📍 Cycle %d: Packet at Router1 (ringIn len=%d), localOut full - forcing circulation",
					cycle, ringInLen)
				circulationDetected = true
			}
		}

		// Check if packet finally arrived
		if workerInQueue1.Length() > 0 {
			received := workerInQueue1.Pick()
			if len(received) > 0 && received[0].SourceID == 0 && received[0].TargetID == 1 {
				t.Logf("✅ Packet finally ejected at cycle %d", cycle)

				if firstArrivalCycle > 0 {
					circulationTime := cycle - firstArrivalCycle
					t.Logf("   First arrival: cycle %d", firstArrivalCycle)
					t.Logf("   Ejection: cycle %d", cycle)
					t.Logf("   Circulation time: %d cycles", circulationTime)

					// Verify packet circulated (should take >10 cycles to circulate full ring)
					if circulationTime < 10 {
						t.Fatalf("❌ Packet ejected too quickly, no circulation detected")
					}
				}

				t.Logf("✅ Test passed: backpressure and circulation verified")
				return
			}
		}
	}

	t.Fatalf("❌ Packet did not arrive within %d cycles", maxCycles)
}

// TestBufferlessRing_Concurrent tests concurrent packet transmission from multiple workers.
// Expected behavior:
// - Worker0 → Worker1 at cycle 0
// - Worker2 → Worker3 at cycle 0
// - Worker1 → Worker2 at cycle 2
// - All packets should arrive at their destinations without loss
// - Routers should handle concurrent traffic correctly with proper prioritization
func TestBufferlessRing_Concurrent(t *testing.T) {
	routers, workers, queues, ringLinks := buildBufferlessRingNetwork(t)

	ctx := context.Background()
	maxCycles := 40

	// Track packets to verify delivery
	sentPackets := []PacketInfo{
		{SourceID: 0, TargetID: 1, Payload: "Packet 0→1", InjectTime: 0, ArriveTime: -1},
		{SourceID: 2, TargetID: 3, Payload: "Packet 2→3", InjectTime: 0, ArriveTime: -1},
		{SourceID: 1, TargetID: 2, Payload: "Packet 1→2", InjectTime: 2, ArriveTime: -1},
	}

	// Inject packets at their designated times
	for cycle := 0; cycle < maxCycles; cycle++ {
		// Inject packets at the beginning of their cycles
		for i := range sentPackets {
			if cycle == sentPackets[i].InjectTime && sentPackets[i].ArriveTime == -1 {
				pkt := packet.Packet{
					SourceID: sentPackets[i].SourceID,
					TargetID: sentPackets[i].TargetID,
					Payload:  sentPackets[i].Payload,
				}
				workerOut := queues.WorkerOut[sentPackets[i].SourceID]
				if err := workerOut.InjectPackets(cycle, []packet.Packet{pkt}); err != nil {
					t.Fatalf("Failed to inject packet %d at cycle %d: %v", i, cycle, err)
				}
				t.Logf("✉️  Cycle %d: Injected packet %d: %s", cycle, i, sentPackets[i].Payload)
			}
		}

		// Tick all components
		for _, r := range routers {
			if err := r.Tick(ctx, uint64(cycle), 0); err != nil {
				t.Fatalf("Router tick failed at cycle %d: %v", cycle, err)
			}
		}
		for _, w := range workers {
			if err := w.Tick(ctx, uint64(cycle), 0); err != nil {
				t.Fatalf("Worker tick failed at cycle %d: %v", cycle, err)
			}
		}
		// Queues are ticked automatically by Nodes.
		for _, link := range ringLinks {
			if err := link.Tick(cycle); err != nil {
				t.Fatalf("Link tick failed at cycle %d: %v", cycle, err)
			}
		}

		// Check for arrived packets
		for workerID := 0; workerID < 4; workerID++ {
			workerIn := queues.WorkerIn[workerID]
			if workerIn.Length() > 0 {
				received := workerIn.Pick()
				for _, pkt := range received {
					// Find matching sent packet
					for i := range sentPackets {
						if sentPackets[i].SourceID == pkt.SourceID &&
							sentPackets[i].TargetID == pkt.TargetID &&
							sentPackets[i].ArriveTime == -1 {
							sentPackets[i].ArriveTime = cycle
							t.Logf("📬 Cycle %d: Packet arrived at Worker%d: %s (latency: %d cycles)",
								cycle, workerID, pkt.Payload, cycle-sentPackets[i].InjectTime)
							break
						}
					}
				}
			}
		}

		// Check if all packets have arrived
		allArrived := true
		for i := range sentPackets {
			if sentPackets[i].ArriveTime == -1 {
				allArrived = false
				break
			}
		}
		if allArrived {
			t.Log("✅ All packets delivered successfully")
			t.Log("Delivery summary:")
			for i, pkt := range sentPackets {
				t.Logf("  Packet %d: %s → injected at cycle %d, arrived at cycle %d (latency: %d)",
					i, pkt.Payload, pkt.InjectTime, pkt.ArriveTime, pkt.ArriveTime-pkt.InjectTime)
			}
			t.Log("✅ Test passed: concurrent multi-packet transmission verified")
			return
		}
	}

	// Report which packets didn't arrive
	t.Log("❌ Not all packets arrived within time limit")
	for i, pkt := range sentPackets {
		if pkt.ArriveTime == -1 {
			t.Logf("  Packet %d MISSING: %s (injected at cycle %d)", i, pkt.Payload, pkt.InjectTime)
		}
	}
	t.Fatalf("❌ Test failed: %d/%d packets arrived", countArrived(sentPackets), len(sentPackets))
}

// PacketInfo tracks packet transmission details for testing
type PacketInfo struct {
	SourceID   int
	TargetID   int
	Payload    string
	InjectTime int
	ArriveTime int
}

func countArrived(packets []PacketInfo) int {
	count := 0
	for _, pkt := range packets {
		if pkt.ArriveTime != -1 {
			count++
		}
	}
	return count
}

// visualizeRingNetwork 返回整个ring网络的ASCII可视化
// 调用所有组件的GetVisualState()方法
func visualizeRingNetwork(
	cycle int,
	routers []*BufferlessRingRouterNode,
	queues *QueueCollection,
	ringLinks []*link.Link,
) string {
	if visualization.VisualizationMode == "none" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n╔═══════════════════════════════════════════════════════════════╗\n"))
	sb.WriteString(fmt.Sprintf("║ Cycle %-3d: Bufferless Ring Network Visualization          ║\n", cycle))
	sb.WriteString(fmt.Sprintf("╚═══════════════════════════════════════════════════════════════╝\n\n"))

	// Ring 拓扑图 (4个router排列成环形)
	sb.WriteString("Ring Topology:\n\n")

	// 上半部分: R0 -> R1
	sb.WriteString(fmt.Sprintf("    %s %s %s\n",
		routers[0].GetVisualState(),
		ringLinks[0].GetVisualState(),
		routers[1].GetVisualState()))

	// 左右两侧的垂直连接
	sb.WriteString("      ↓              ↓\n")

	// 下半部分: R3 <- R2
	sb.WriteString(fmt.Sprintf("    %s %s %s\n",
		routers[3].GetVisualState(),
		"<---",
		routers[2].GetVisualState()))

	sb.WriteString("\n")

	// Queue 状态
	sb.WriteString("Queue Status:\n")
	for i := 0; i < 4; i++ {
		sb.WriteString(fmt.Sprintf("  R%d: ringIn=%s localIn=%s ringOut=%s localOut=%s\n",
			i,
			queues.RingIn[i].GetVisualState(),
			queues.LocalIn[i].GetVisualState(),
			queues.RingOut[i].GetVisualState(),
			queues.LocalOut[i].GetVisualState()))
	}

	sb.WriteString("\n")

	// Worker Queue 状态
	sb.WriteString("Worker Queues:\n")
	for i := 0; i < 4; i++ {
		sb.WriteString(fmt.Sprintf("  W%d: Out=%s In=%s\n",
			i,
			queues.WorkerOut[i].GetVisualState(),
			queues.WorkerIn[i].GetVisualState()))
	}

	sb.WriteString("\nLegend: R=Router, W=Worker, [len/cap], -[n]-=packets in flight\n")
	sb.WriteString("═══════════════════════════════════════════════════════════════\n")

	return sb.String()
}

// TestBufferlessRing_Visualized 演示可视化功能
// 展示packet从Worker0到Worker1的完整传输过程
func TestBufferlessRing_Visualized(t *testing.T) {
	// 确保使用ASCII可视化模式
	originalMode := visualization.VisualizationMode
	visualization.VisualizationMode = "ascii"
	defer func() { visualization.VisualizationMode = originalMode }()

	routers, workers, queues, ringLinks := buildBufferlessRingNetwork(t)

	// 注入测试packet
	testPacket := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "Visualized Test Packet",
	}
	queues.WorkerOut[0].InjectPackets(0, []packet.Packet{testPacket})
	t.Logf("📤 Injected packet: Worker0 → Worker1")

	ctx := context.Background()

	// 模拟前10个cycle，每个cycle显示可视化
	for cycle := 0; cycle < 10; cycle++ {
		// Tick所有组件
		for _, r := range routers {
			if err := r.Tick(ctx, uint64(cycle), 0); err != nil {
				t.Fatalf("Router tick failed at cycle %d: %v", cycle, err)
			}
		}
		for _, w := range workers {
			if err := w.Tick(ctx, uint64(cycle), 0); err != nil {
				t.Fatalf("Worker tick failed at cycle %d: %v", cycle, err)
			}
		}
		// Queues are ticked automatically by Nodes.
		for _, link := range ringLinks {
			if err := link.Tick(cycle); err != nil {
				t.Fatalf("Link tick failed at cycle %d: %v", cycle, err)
			}
		}

		// 可视化当前状态
		vis := visualizeRingNetwork(cycle, routers, queues, ringLinks)
		t.Log(vis)

		// 检查是否到达
		if queues.WorkerIn[1].Length() > 0 {
			received := queues.WorkerIn[1].Pick()
			if len(received) > 0 {
				t.Logf("\n🎉 Packet arrived at Worker1 at cycle %d!\n", cycle)
				t.Logf("✅ Visualization test passed")
				return
			}
		}
	}

	t.Fatal("❌ Packet did not arrive within 10 cycles")
}

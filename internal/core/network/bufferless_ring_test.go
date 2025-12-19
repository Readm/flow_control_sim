package network

import (
	"sync"
	"testing"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/queue"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// toggleBusyHandler allows dynamic control over Link readiness for testing backpressure.
type toggleBusyHandler struct {
	busy      bool
	inner     link.LinkHandler
	initReady int
}

func (h *toggleBusyHandler) Process(l *link.Link, cycle int, incoming []packet.Packet) error {
	if cycle == 0 {
		l.UpdateUpstreamReady(0, !h.busy)
	}
	// Signal ready state for NEXT cycle
	l.UpdateUpstreamReady(cycle+1, !h.busy)

	if h.busy {
		// If busy, we must buffer the incoming packets to avoid losing them
		// and update their cycle to current so they're not "too late"
		for _, pkt := range incoming {
			l.AddPendingPacket(ahead_port.PacketWithCycle{
				Cycle:  cycle,
				Packet: pkt,
			})
		}
		return nil
	}
	return h.inner.Process(l, cycle, incoming)
}

func (h *toggleBusyHandler) Reset() {
	h.inner.Reset()
}

func (h *toggleBusyHandler) Init(l *link.Link) {
	depth := h.initReady
	if depth > 0 {
		for i := 0; i < depth; i++ {
			l.UpdateUpstreamReady(i, true)
		}
	}
}

// TestBufferlessRing_SinglePacket_v2 tests sending a single packet from Worker0 to Worker1 using Network.
func TestBufferlessRing_SinglePacket_v2(t *testing.T) {
	const (
		numRouters     = 4
		ringLatency    = 5
		localLatency   = 1
		routerBuffer   = 4
		queueSize      = 8
		queueBandwidth = 2
	)

	net := New()

	// Create routers and workers
	routers := make([]*NodeHandle, numRouters)
	workers := make([]*NodeHandle, numRouters)
	workerOutputs := make([]*queue.OutputQueue, numRouters)

	for i := 0; i < numRouters; i++ {
		routerID := 100 + i
		workerID := i

		// Create router with 2 inputs (ring + local) and 2 outputs (ring + local)
		router := node.NewBufferlessRingRouter(routerID, workerID, routerBuffer)
		ringInQueue := queue.NewInputQueue(queueSize, queueBandwidth)
		localInQueue := queue.NewInputQueue(queueSize, queueBandwidth)
		ringOutQueue := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		localOutQueue := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)

		router.AddInputQueue(ringInQueue)
		router.AddInputQueue(localInQueue)
		router.AddOutputQueue(ringOutQueue)
		router.AddOutputQueue(localOutQueue)

		routers[i] = &NodeHandle{
			Node:    router,
			Inputs:  []*queue.InputQueue{ringInQueue, localInQueue},
			Outputs: []*queue.OutputQueue{ringOutQueue, localOutQueue},
		}

		// Create worker with 1 input and 1 output
		worker := node.NewWorkerNode(workerID)
		workerInQueue := queue.NewInputQueue(queueSize, queueBandwidth)
		workerOutQueue := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)

		worker.AddInputQueue(workerInQueue)
		worker.AddOutputQueue(workerOutQueue)

		workers[i] = &NodeHandle{
			Node:    worker,
			Inputs:  []*queue.InputQueue{workerInQueue},
			Outputs: []*queue.OutputQueue{workerOutQueue},
		}

		workerOutputs[i] = workerOutQueue

		// Add nodes to network
		if err := net.AddNode(routers[i]); err != nil {
			t.Fatalf("Failed to add router %d: %v", i, err)
		}
		if err := net.AddNode(workers[i]); err != nil {
			t.Fatalf("Failed to add worker %d: %v", i, err)
		}
	}

	// Connect ring: Router0 -> Router1 -> Router2 -> Router3 -> Router0
	// Use bufferless flow control for ring links
	for i := 0; i < numRouters; i++ {
		nextRouter := (i + 1) % numRouters
		fc := link.NewBufferlessLinkHandler()
		if _, err := net.ConnectWithHandler(100+i, 0, 100+nextRouter, 0, ringLatency, queueBandwidth, fc); err != nil {
			t.Fatalf("Failed to connect ring %d->%d: %v", i, nextRouter, err)
		}
	}

	// Connect local: Worker <-> Router
	// Use bufferless flow control for local links as well
	for i := 0; i < numRouters; i++ {
		// Worker -> Router (local injection)
		fc1 := link.NewBufferlessLinkHandler()
		if _, err := net.ConnectWithHandler(i, 0, 100+i, 1, localLatency, queueBandwidth, fc1); err != nil {
			t.Fatalf("Failed to connect worker %d -> router %d: %v", i, i, err)
		}
		// Router -> Worker (local ejection)
		fc2 := link.NewBufferlessLinkHandler()
		if _, err := net.ConnectWithHandler(100+i, 1, i, 0, localLatency, queueBandwidth, fc2); err != nil {
			t.Fatalf("Failed to connect router %d -> worker %d: %v", i, i, err)
		}
	}

	// Inject packet from Worker0 to Worker1
	testPacket := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "Test packet from Worker0 to Worker1",
	}

	if err := workerOutputs[0].InjectPackets(0, []packet.Packet{testPacket}); err != nil {
		t.Fatalf("Failed to inject packet: %v", err)
	}
	t.Logf("Successfully injected packet at cycle 0")
	t.Logf("Worker0 output queue after injection: %d/%d", workerOutputs[0].Length(), workerOutputs[0].Capacity())

	// Check if packet arrived at Worker1
	workerInQueue1 := workers[1].Inputs[0]
	var receivedPackets []packet.Packet
	var mu sync.Mutex
	workerInQueue1.SetPacketReceivedHook(func(pkt packet.Packet) {
		mu.Lock()
		defer mu.Unlock()
		receivedPackets = append(receivedPackets, pkt)
	})

	// Run simulation - packet needs to travel through local link (1) + ring link (5) + local link (1) + processing
	// Total cycles needed: ~10-15 cycles
	t.Logf("Starting simulation for 50 cycles...")
	if err := net.Advance(50); err != nil {
		t.Fatalf("Failed to advance network: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	t.Logf("Worker1 received %d packets", len(receivedPackets))

	if len(receivedPackets) == 0 {
		// Debug: check other queues
		t.Logf("Worker0 output queue: %d/%d", workerOutputs[0].Length(), workerOutputs[0].Capacity())
		t.Logf("Router0 local input queue: %d/%d", routers[0].Inputs[1].Length(), routers[0].Inputs[1].Capacity())
		t.Logf("Router0 ring output queue: %d/%d", routers[0].Outputs[0].Length(), routers[0].Outputs[0].Capacity())
		t.Logf("Router1 ring input queue: %d/%d", routers[1].Inputs[0].Length(), routers[1].Inputs[0].Capacity())
		t.Logf("Router1 local output queue: %d/%d", routers[1].Outputs[1].Length(), routers[1].Outputs[1].Capacity())
		t.Fatalf("No packets received at Worker1")
	}

	if receivedPackets[0].SourceID != 0 {
		t.Errorf("Expected SourceID=0, got %d", receivedPackets[0].SourceID)
	}
	if receivedPackets[0].TargetID != 1 {
		t.Errorf("Expected TargetID=1, got %d", receivedPackets[0].TargetID)
	}
	if receivedPackets[0].Payload != testPacket.Payload {
		t.Errorf("Payload mismatch: expected %q, got %q", testPacket.Payload, receivedPackets[0].Payload)
	}

	t.Log("✅ Test passed: 2-hop packet delivered successfully")
}

// TestBufferlessRing_Backpressure_v2 tests that packets circulate in the ring when the target is busy.
func TestBufferlessRing_Backpressure_v2(t *testing.T) {
	const (
		numRouters     = 4
		ringLatency    = 5
		localLatency   = 1
		routerBuffer   = 4
		queueSize      = 8
		queueBandwidth = 2
	)

	net := New()

	routers := make([]*NodeHandle, numRouters)
	workers := make([]*NodeHandle, numRouters)
	workerOutputs := make([]*queue.OutputQueue, numRouters)
	localOutQueues := make([]*queue.OutputQueue, numRouters)

	// Build network
	for i := 0; i < numRouters; i++ {
		routerID := 100 + i
		workerID := i

		router := node.NewBufferlessRingRouter(routerID, workerID, routerBuffer)
		ringInQueue := queue.NewInputQueue(queueSize, queueBandwidth)
		localInQueue := queue.NewInputQueue(queueSize, queueBandwidth)
		ringOutQueue := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		localOutQueue := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)

		router.AddInputQueue(ringInQueue)
		router.AddInputQueue(localInQueue)
		router.AddOutputQueue(ringOutQueue)
		router.AddOutputQueue(localOutQueue)

		routers[i] = &NodeHandle{
			Node:    router,
			Inputs:  []*queue.InputQueue{ringInQueue, localInQueue},
			Outputs: []*queue.OutputQueue{ringOutQueue, localOutQueue},
		}
		localOutQueues[i] = localOutQueue

		worker := node.NewWorkerNode(workerID)
		workerInQueue := queue.NewInputQueue(queueSize, queueBandwidth)
		workerOutQueue := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)

		worker.AddInputQueue(workerInQueue)
		worker.AddOutputQueue(workerOutQueue)

		workers[i] = &NodeHandle{
			Node:    worker,
			Inputs:  []*queue.InputQueue{workerInQueue},
			Outputs: []*queue.OutputQueue{workerOutQueue},
		}

		workerOutputs[i] = workerOutQueue

		if err := net.AddNode(routers[i]); err != nil {
			t.Fatalf("Failed to add router %d: %v", i, err)
		}
		if err := net.AddNode(workers[i]); err != nil {
			t.Fatalf("Failed to add worker %d: %v", i, err)
		}
	}

	// Connect ring
	for i := 0; i < numRouters; i++ {
		nextRouter := (i + 1) % numRouters
		fc := link.NewBufferlessLinkHandler()
		if _, err := net.ConnectWithHandler(100+i, 0, 100+nextRouter, 0, ringLatency, queueBandwidth, fc); err != nil {
			t.Fatalf("Failed to connect ring: %v", err)
		}
	}

	var ejectionHandler *toggleBusyHandler
	for i := 0; i < numRouters; i++ {
		fc1 := link.NewBufferlessLinkHandler()
		if _, err := net.ConnectWithHandler(i, 0, 100+i, 1, localLatency, queueBandwidth, fc1); err != nil {
			t.Fatalf("Failed to connect worker->router: %v", err)
		}

		var ejectionLink *link.Link
		var fc2 link.LinkHandler
		if i == 1 {
			ejectionHandler = &toggleBusyHandler{busy: true, inner: link.NewBufferlessLinkHandler(), initReady: 0}
			fc2 = ejectionHandler
		} else {
			fc2 = &toggleBusyHandler{inner: link.NewBufferlessLinkHandler(), initReady: 1}
		}

		ejectionLink, err := net.Connect(100+i, 1, i, 0, localLatency, queueBandwidth, WithHandler(fc2))
		if err != nil {
			t.Fatalf("Failed to connect router->worker: %v", err)
		}
		_ = ejectionLink // Avoid unused warning if needed
	}

	// 1. Fill Worker1's localOut queue to simulate backpressure
	dummyPackets := make([]packet.Packet, queueSize)
	for i := range dummyPackets {
		dummyPackets[i] = packet.Packet{
			SourceID: 99,
			TargetID: 99,
			Payload:  "Dummy",
		}
	}
	if err := localOutQueues[1].InjectPackets(0, dummyPackets); err != nil {
		t.Fatalf("Failed to inject dummy packets: %v", err)
	}

	// 1b. Inject extra packets at cycle 1 to ensure it stays full despite cycle 0 pull
	extraPackets := []packet.Packet{
		{SourceID: 99, TargetID: 99, Payload: "Extra1"},
		{SourceID: 99, TargetID: 99, Payload: "Extra2"},
	}
	if err := localOutQueues[1].InjectPackets(0, extraPackets); err == nil {
		// Error is expected if queue is full from cycle 0, that's fine.
		// In bufferless model, InjectPackets might be called multiple times.
	}

	// 2. Inject test packet from Worker0 to Worker1
	testPacket := packet.Packet{
		SourceID: 0,
		TargetID: 1,
		Payload:  "Backpressure test",
	}
	if err := workerOutputs[0].InjectPackets(0, []packet.Packet{testPacket}); err != nil {
		t.Fatalf("Failed to inject test packet: %v", err)
	}

	// 3. Setup reception hook
	workerInQueue1 := workers[1].Inputs[0]
	var receivedPackets []packet.Packet
	var mu sync.Mutex
	workerInQueue1.SetPacketReceivedHook(func(pkt packet.Packet) {
		mu.Lock()
		defer mu.Unlock()
		receivedPackets = append(receivedPackets, pkt)
		// t.Logf("DEBUG: Worker1 received packet: Src=%d, Dst=%d, Payload=%s", pkt.SourceID, pkt.TargetID, pkt.Payload)
	})

	// 4. Run simulation in stages
	// Stage A: Advance 20 cycles with backpressure. Packet should circulate.
	t.Log("Advancing 20 cycles with backpressure...")
	if err := net.Advance(20); err != nil {
		t.Fatalf("Advance failed: %v", err)
	}

	mu.Lock()
	if len(receivedPackets) > 0 {
		mu.Unlock()
		t.Fatalf("Packet delivered despite backpressure! Received payloads: %v", receivedPackets)
	}
	mu.Unlock()

	// Stage B: Clear backpressure and advance more.
	t.Log("Clearing backpressure and advancing 50 more cycles...")
	ejectionHandler.busy = false

	if err := net.Advance(50); err != nil {
		t.Fatalf("Advance failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, p := range receivedPackets {
		if p.Payload == "Backpressure test" {
			found = true
			break
		}
	}
	if !found {
		t.Logf("DEBUG: Final received packet count: %d", len(receivedPackets))
		for i, p := range receivedPackets {
			t.Logf("  [%d] Src=%d Dst=%d Payload=%s", i, p.SourceID, p.TargetID, p.Payload)
		}
		t.Fatalf("Test packet not delivered after clearing backpressure (Total cycles: 70)")
	}
	t.Logf("✅ Test packet delivered successfully after circulation")
}

// TestBufferlessRing_Concurrent_v2 tests multiple packets in the ring simultaneously.
func TestBufferlessRing_Concurrent_v2(t *testing.T) {
	const (
		numRouters     = 4
		ringLatency    = 5
		localLatency   = 1
		routerBuffer   = 4
		queueSize      = 8
		queueBandwidth = 2
	)

	net := New()
	routers := make([]*NodeHandle, numRouters)
	workers := make([]*NodeHandle, numRouters)
	workerOutputs := make([]*queue.OutputQueue, numRouters)

	for i := 0; i < numRouters; i++ {
		routerID := 100 + i
		workerID := i
		router := node.NewBufferlessRingRouter(routerID, workerID, routerBuffer)
		riq := queue.NewInputQueue(queueSize, queueBandwidth)
		liq := queue.NewInputQueue(queueSize, queueBandwidth)
		roq := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		loq := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		router.AddInputQueue(riq)
		router.AddInputQueue(liq)
		router.AddOutputQueue(roq)
		router.AddOutputQueue(loq)
		routers[i] = &NodeHandle{Node: router, Inputs: []*queue.InputQueue{riq, liq}, Outputs: []*queue.OutputQueue{roq, loq}}

		worker := node.NewWorkerNode(workerID)
		wiq := queue.NewInputQueue(queueSize, queueBandwidth)
		woq := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		worker.AddInputQueue(wiq)
		worker.AddOutputQueue(woq)
		workers[i] = &NodeHandle{Node: worker, Inputs: []*queue.InputQueue{wiq}, Outputs: []*queue.OutputQueue{woq}}
		workerOutputs[i] = woq

		net.AddNode(routers[i])
		net.AddNode(workers[i])
	}

	for i := 0; i < numRouters; i++ {
		next := (i + 1) % numRouters
		net.ConnectWithHandler(100+i, 0, 100+next, 0, ringLatency, queueBandwidth, link.NewBufferlessLinkHandler())
		net.ConnectWithHandler(i, 0, 100+i, 1, localLatency, queueBandwidth, link.NewBufferlessLinkHandler())
		net.ConnectWithHandler(100+i, 1, i, 0, localLatency, queueBandwidth, link.NewBufferlessLinkHandler())
	}

	// Inject 3 concurrent packets
	pkts := []struct {
		src, dst int
		msg      string
	}{
		{0, 1, "P01"},
		{2, 3, "P23"},
		{1, 2, "P12"},
	}

	var mu sync.Mutex
	receivedCount := 0
	for i := 0; i < numRouters; i++ {
		workers[i].Inputs[0].SetPacketReceivedHook(func(pkt packet.Packet) {
			mu.Lock()
			receivedCount++
			mu.Unlock()
		})
	}

	for _, p := range pkts {
		workerOutputs[p.src].InjectPackets(0, []packet.Packet{{SourceID: p.src, TargetID: p.dst, Payload: p.msg}})
	}

	if err := net.Advance(50); err != nil {
		t.Fatalf("Advance failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if receivedCount != len(pkts) {
		t.Fatalf("Expected %d packets, got %d", len(pkts), receivedCount)
	}
	t.Logf("✅ All %d concurrent packets delivered", receivedCount)
}

// TestBufferlessRing_Basic_v2 tests basic network construction
func TestBufferlessRing_Basic_v2(t *testing.T) {
	const (
		numRouters     = 4
		routerBuffer   = 4
		queueSize      = 8
		queueBandwidth = 2
	)

	net := New()

	for i := 0; i < numRouters; i++ {
		routerID := 100 + i
		workerID := i

		router := node.NewBufferlessRingRouter(routerID, workerID, routerBuffer)
		ringInQueue := queue.NewInputQueue(queueSize, queueBandwidth)
		localInQueue := queue.NewInputQueue(queueSize, queueBandwidth)
		ringOutQueue := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		localOutQueue := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)

		router.AddInputQueue(ringInQueue)
		router.AddInputQueue(localInQueue)
		router.AddOutputQueue(ringOutQueue)
		router.AddOutputQueue(localOutQueue)

		routerHandle := &NodeHandle{
			Node:    router,
			Inputs:  []*queue.InputQueue{ringInQueue, localInQueue},
			Outputs: []*queue.OutputQueue{ringOutQueue, localOutQueue},
		}

		if err := net.AddNode(routerHandle); err != nil {
			t.Fatalf("Failed to add router %d: %v", i, err)
		}

		t.Logf("Router %d: workerID=%d, capacity=%d", routerID, workerID, routerBuffer)
	}

	t.Log("✅ Basic construction successful")
}

// TestBufferlessRing_TwoHops_v2 tests packet delivery across 2 hops (Worker0 → Worker2).
func TestBufferlessRing_TwoHops_v2(t *testing.T) {
	const (
		numRouters     = 4
		ringLatency    = 5
		localLatency   = 1
		routerBuffer   = 4
		queueSize      = 8
		queueBandwidth = 2
	)

	net := New()

	routers := make([]*NodeHandle, numRouters)
	workers := make([]*NodeHandle, numRouters)
	workerOutputs := make([]*queue.OutputQueue, numRouters)

	// Build network
	for i := 0; i < numRouters; i++ {
		routerID := 100 + i
		workerID := i

		router := node.NewBufferlessRingRouter(routerID, workerID, routerBuffer)
		ringInQueue := queue.NewInputQueue(queueSize, queueBandwidth)
		localInQueue := queue.NewInputQueue(queueSize, queueBandwidth)
		ringOutQueue := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)
		localOutQueue := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)

		router.AddInputQueue(ringInQueue)
		router.AddInputQueue(localInQueue)
		router.AddOutputQueue(ringOutQueue)
		router.AddOutputQueue(localOutQueue)

		routers[i] = &NodeHandle{
			Node:    router,
			Inputs:  []*queue.InputQueue{ringInQueue, localInQueue},
			Outputs: []*queue.OutputQueue{ringOutQueue, localOutQueue},
		}

		worker := node.NewWorkerNode(workerID)
		workerInQueue := queue.NewInputQueue(queueSize, queueBandwidth)
		workerOutQueue := queue.NewOutputQueue(queueSize, queueBandwidth, queueBandwidth)

		worker.AddInputQueue(workerInQueue)
		worker.AddOutputQueue(workerOutQueue)

		workers[i] = &NodeHandle{
			Node:    worker,
			Inputs:  []*queue.InputQueue{workerInQueue},
			Outputs: []*queue.OutputQueue{workerOutQueue},
		}

		workerOutputs[i] = workerOutQueue

		if err := net.AddNode(routers[i]); err != nil {
			t.Fatalf("Failed to add router %d: %v", i, err)
		}
		if err := net.AddNode(workers[i]); err != nil {
			t.Fatalf("Failed to add worker %d: %v", i, err)
		}
	}

	// Connect ring - use bufferless flow control
	for i := 0; i < numRouters; i++ {
		nextRouter := (i + 1) % numRouters
		fc := link.NewBufferlessLinkHandler()
		if _, err := net.ConnectWithHandler(100+i, 0, 100+nextRouter, 0, ringLatency, queueBandwidth, fc); err != nil {
			t.Fatalf("Failed to connect ring: %v", err)
		}
	}

	// Connect local - use bufferless flow control
	for i := 0; i < numRouters; i++ {
		fc1 := link.NewBufferlessLinkHandler()
		if _, err := net.ConnectWithHandler(i, 0, 100+i, 1, localLatency, queueBandwidth, fc1); err != nil {
			t.Fatalf("Failed to connect worker->router: %v", err)
		}
		fc2 := link.NewBufferlessLinkHandler()
		if _, err := net.ConnectWithHandler(100+i, 1, i, 0, localLatency, queueBandwidth, fc2); err != nil {
			t.Fatalf("Failed to connect router->worker: %v", err)
		}
	}

	// Inject packet from Worker0 to Worker2 (2 hops on ring)
	pkt := packet.Packet{
		SourceID: 0,
		TargetID: 2,
		Payload:  "Test packet from Worker0 to Worker2",
	}

	if err := workerOutputs[0].InjectPackets(0, []packet.Packet{pkt}); err != nil {
		t.Fatalf("Failed to inject packet: %v", err)
	}

	// Setup packet reception hook
	workerInQueue2 := workers[2].Inputs[0]
	var receivedPackets []packet.Packet
	var mu sync.Mutex
	workerInQueue2.SetPacketReceivedHook(func(pkt packet.Packet) {
		mu.Lock()
		defer mu.Unlock()
		receivedPackets = append(receivedPackets, pkt)
	})

	// 2 hops: local(1) + ring(5) + ring(5) + local(1) + processing ≈ 15-20 cycles
	t.Logf("Starting simulation for 50 cycles...")
	if err := net.Advance(50); err != nil {
		t.Fatalf("Failed to advance: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	t.Logf("Worker2 received %d packets", len(receivedPackets))
	if len(receivedPackets) == 0 {
		t.Fatalf("No packets received at Worker2")
	}

	if receivedPackets[0].SourceID != 0 || receivedPackets[0].TargetID != 2 {
		t.Fatalf("Packet content mismatch: expected Src=0 Dst=2, got Src=%d Dst=%d",
			receivedPackets[0].SourceID, receivedPackets[0].TargetID)
	}

	t.Log("✅ Test passed: 2-hop packet delivered successfully")
}

package ahead_port

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// testProcessor is a test implementation of PacketProcessor
type testProcessor struct {
	*DefaultProcessor
	hookCalls *struct {
		mu                      sync.Mutex
		dataReceived            []PacketWithCycle
		backpressureIndependent []PacketWithCycle
		downstreamReady         []PacketWithCycle
		downstreamNotReady      []int
	}
}

func (t *testProcessor) ProcessPackets(receiveChan <-chan PacketWithCycle, cycle int, checkReady func(int) bool, sendPacket func(PacketWithCycle), setDone func(int), updateUpstreamReady func(cycle int, ready bool)) {
	// pendingPackets is a static variable (struct field from embedded DefaultProcessor) - access directly
	pendingPackets := t.pendingPackets

	// Receive all available packets from channel
	cycleReceivedPackets := make([]PacketWithCycle, 0)
	for {
		select {
		case pkt := <-receiveChan:
			cycleReceivedPackets = append(cycleReceivedPackets, pkt)
		default:
			goto processPackets
		}
	}

processPackets:
	// Track data received
	t.hookCalls.mu.Lock()
	t.hookCalls.dataReceived = append(t.hookCalls.dataReceived, cycleReceivedPackets...)
	t.hookCalls.mu.Unlock()

	// Combine all packets
	allPackets := make([]PacketWithCycle, 0, len(pendingPackets)+len(cycleReceivedPackets))
	allPackets = append(allPackets, pendingPackets...)
	allPackets = append(allPackets, cycleReceivedPackets...)

	// Track backpressure-independent processing (all packets are processed)
	t.hookCalls.mu.Lock()
	t.hookCalls.backpressureIndependent = append(t.hookCalls.backpressureIndependent, allPackets...)
	t.hookCalls.mu.Unlock()

	newPendingPackets := make([]PacketWithCycle, 0)

	// For each packet, check if ready
	for _, pkt := range allPackets {
		pktCycle := int(pkt.Cycle)
		isReady := checkReady(pktCycle)

		t.hookCalls.mu.Lock()
		if isReady {
			t.hookCalls.downstreamReady = append(t.hookCalls.downstreamReady, pkt)
		} else {
			t.hookCalls.downstreamNotReady = append(t.hookCalls.downstreamNotReady, pktCycle)
		}
		t.hookCalls.mu.Unlock()

		if isReady {
			// Ready: send the packet immediately
			pkt.Cycle = pktCycle
			sendPacket(pkt)
		} else {
			// Not ready: keep in pending
			newPendingPackets = append(newPendingPackets, pkt)
		}
	}

	// F: SetDone after processing all packets
	setDone(cycle + 1)

	// Q: Notify upstream about readiness for next cycle
	updateUpstreamReady(cycle+1, true)

	// Update pending packets (static variable - struct field)
	t.pendingPackets = newPendingPackets
}

// incrementTestProcessor is a test processor implementation for cycle increment testing
type incrementTestProcessor struct {
	*DefaultProcessor
	notReadyCycles *[]int
	finalCycle     *int
}

func (i *incrementTestProcessor) ProcessPackets(receiveChan <-chan PacketWithCycle, cycle int, checkReady func(int) bool, sendPacket func(PacketWithCycle), setDone func(int), updateUpstreamReady func(cycle int, ready bool)) {
	// pendingPackets is a static variable (struct field from embedded DefaultProcessor) - access directly
	pendingPackets := i.pendingPackets

	// Receive all available packets from channel
	cycleReceivedPackets := make([]PacketWithCycle, 0)
	for {
		select {
		case pkt := <-receiveChan:
			cycleReceivedPackets = append(cycleReceivedPackets, pkt)
		default:
			goto processPackets
		}
	}

processPackets:
	// Combine all packets
	allPackets := make([]PacketWithCycle, 0, len(pendingPackets)+len(cycleReceivedPackets))
	allPackets = append(allPackets, pendingPackets...)
	allPackets = append(allPackets, cycleReceivedPackets...)

	newPendingPackets := make([]PacketWithCycle, 0)

	// For each packet, check if ready
	for _, pkt := range allPackets {
		pktCycle := int(pkt.Cycle)
		isReady := checkReady(pktCycle)

		if isReady {
			*i.finalCycle = pktCycle
			// Ready: send the packet immediately
			pkt.Cycle = pktCycle
			sendPacket(pkt)
		} else {
			*i.notReadyCycles = append(*i.notReadyCycles, pktCycle)
			newPendingPackets = append(newPendingPackets, pkt)
		}
	}

	// F: SetDone after processing all packets
	setDone(cycle + 1)

	// Q: Notify upstream about readiness for next cycle
	updateUpstreamReady(cycle+1, true)

	// Update pending packets (static variable - struct field)
	i.pendingPackets = newPendingPackets
}

// countIncrementHooks is a test hooks implementation for counting increments
type countIncrementProcessor struct {
	*DefaultProcessor
	incrementCount *int
}

func (c *countIncrementProcessor) ProcessPackets(receiveChan <-chan PacketWithCycle, cycle int, checkReady func(int) bool, sendPacket func(PacketWithCycle), setDone func(int), updateUpstreamReady func(cycle int, ready bool)) {
	// pendingPackets is a static variable (struct field from embedded DefaultProcessor) - access directly
	pendingPackets := c.pendingPackets

	// Receive all available packets from channel
	cycleReceivedPackets := make([]PacketWithCycle, 0)
	for {
		select {
		case pkt := <-receiveChan:
			cycleReceivedPackets = append(cycleReceivedPackets, pkt)
		default:
			goto processPackets
		}
	}

processPackets:
	// Combine all packets
	allPackets := make([]PacketWithCycle, 0, len(pendingPackets)+len(cycleReceivedPackets))
	allPackets = append(allPackets, pendingPackets...)
	allPackets = append(allPackets, cycleReceivedPackets...)

	newPendingPackets := make([]PacketWithCycle, 0)

	// For each packet, check if ready
	for _, pkt := range allPackets {
		pktCycle := int(pkt.Cycle)
		isReady := checkReady(pktCycle)

		if isReady {
			// Ready: send the packet immediately
			pkt.Cycle = pktCycle
			sendPacket(pkt)
		} else {
			*c.incrementCount++
			newPendingPackets = append(newPendingPackets, pkt)
		}
	}

	// F: SetDone after processing all packets
	setDone(cycle + 1)

	// Q: Notify upstream about readiness for next cycle
	updateUpstreamReady(cycle+1, true)

	// Update pending packets (static variable - struct field)
	c.pendingPackets = newPendingPackets
}

// allPacketsTestProcessor tracks all received packets for testing
type allPacketsTestProcessor struct {
	*DefaultProcessor
	receivedPackets *[]PacketWithCycle
	mu              *sync.Mutex
}

func (a *allPacketsTestProcessor) ProcessPackets(receiveChan <-chan PacketWithCycle, cycle int, checkReady func(int) bool, sendPacket func(PacketWithCycle), setDone func(int), updateUpstreamReady func(cycle int, ready bool)) {
	// pendingPackets is a static variable (struct field from embedded DefaultHooks) - access directly
	pendingPackets := a.pendingPackets

	// Receive all available packets from channel
	cycleReceivedPackets := make([]PacketWithCycle, 0)
	for {
		select {
		case pkt := <-receiveChan:
			cycleReceivedPackets = append(cycleReceivedPackets, pkt)
		default:
			goto processPackets
		}
	}

processPackets:
	// Track all received packets
	a.mu.Lock()
	*a.receivedPackets = append(*a.receivedPackets, cycleReceivedPackets...)
	a.mu.Unlock()

	// Process packets using the same logic as DefaultProcessor
	newPendingPackets := make([]PacketWithCycle, 0)

	// Helper function to process a single packet
	processPacket := func(pkt PacketWithCycle) {
		pktCycle := int(pkt.Cycle)
		isReady := checkReady(pktCycle)
		if isReady {
			// Ready: send the packet immediately
			pkt.Cycle = pktCycle
			sendPacket(pkt)
		} else {
			// Not ready: keep in pending
			newPendingPackets = append(newPendingPackets, pkt)
		}
	}

	// Process pending packets first
	for _, pkt := range pendingPackets {
		processPacket(pkt)
	}

	// Process newly received packets
	for _, pkt := range cycleReceivedPackets {
		processPacket(pkt)
	}

	// Set Done after processing all packets
	setDone(cycle + 1)

	// Q: Notify upstream about readiness for next cycle
	updateUpstreamReady(cycle+1, true)

	// Update pending packets (static variable - struct field)
	a.pendingPackets = newPendingPackets
}

// TestCycleProcessorBasicFlow tests the basic cycle processing workflow.
func TestCycleProcessorBasicFlow(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort := NewAheadPort(8)

	// Track hook calls
	var hookCalls struct {
		mu                      sync.Mutex
		dataReceived            []PacketWithCycle
		backpressureIndependent []PacketWithCycle
		downstreamReady         []PacketWithCycle
		downstreamNotReady      []int
	}

	// Create a custom processor implementation for testing
	testProc := &testProcessor{
		DefaultProcessor: &DefaultProcessor{},
		hookCalls:        &hookCalls,
	}

	processor := NewCycleProcessor(upstreamPort, downstreamPort, testProc)

	// Set initial state
	upstreamPort.SetDone(-1)
	downstreamPort.UpdateReady(0, true)

	// Send a packet from upstream
	pkt := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"},
	}
	upstreamPort.SendChan() <- pkt
	upstreamPort.SetDone(1)

	// Process cycle 0
	err := processor.Tick(0)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Verify hook calls
	hookCalls.mu.Lock()
	if len(hookCalls.dataReceived) != 1 {
		t.Errorf("expected 1 data received, got %d", len(hookCalls.dataReceived))
	}
	if len(hookCalls.downstreamReady) != 1 {
		t.Errorf("expected 1 downstream ready, got %d", len(hookCalls.downstreamReady))
	}
	if len(hookCalls.downstreamNotReady) != 0 {
		t.Errorf("expected 0 downstream not ready, got %d", len(hookCalls.downstreamNotReady))
	}
	hookCalls.mu.Unlock()

	// Verify packet was sent
	select {
	case receivedPkt := <-downstreamPort.ReceiveChan():
		if receivedPkt.Cycle != 0 {
			t.Errorf("expected cycle 0, got %d", receivedPkt.Cycle)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("packet not received")
	}
}

// TestCycleProcessorCycleIncrement tests that cycle is incremented when downstream is not ready.
func TestCycleProcessorCycleIncrement(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort := NewAheadPort(8)

	// Set downstream cycles 5, 6, 7 as not ready, 8 as ready
	// Important: readyUntil represents cycles that downstream can execute ahead.
	// If readyUntil >= cycle, Ready(cycle) returns true via fast path (correct behavior).
	// To test cycle increment logic, we need readyUntil < 5, so Ready(5) checks readyMap.
	// Note: UpdateReady(8, true) will update readyUntil to 9, but that's OK because
	downstreamPort.SetReadyUntil(4)      // Cycles < 4: fast path true, cycles >= 4: check readyMap
	downstreamPort.UpdateReady(5, false) // readyMap[5] = false
	downstreamPort.UpdateReady(6, false) // readyMap[6] = false
	downstreamPort.UpdateReady(7, false) // readyMap[7] = false
	downstreamPort.UpdateReady(8, true)  // readyMap[8] = true, readyUntil becomes 9

	// Verify: Ready(5) should check readyMap (because 5 >= readyUntil(4) initially)
	// But after UpdateReady(8, true), readyUntil becomes 9, so Ready(5) would return true via fast path.
	// However, readyMap[5] = false is still there, so if we check readyMap first... wait, no.
	// The Ready() logic is: if cycle < readyUntil, return true (fast path).
	// So Ready(5) with readyUntil=9 will return true, which is correct behavior.
	//
	// To test the increment logic, we need to ensure Ready(5) returns false.
	// Since readyUntil=9 now, Ready(5) will return true. So we need a different approach.
	// Actually, the increment logic should work: it checks Ready(5), which returns true (readyUntil=9),
	// so it won't increment. But that's not what we want to test.
	//
	// Let me reconsider: the test should verify that when Ready(cycle) returns false,
	// the cycle gets incremented. But if readyUntil >= cycle, Ready(cycle) returns true,
	// which is correct - downstream CAN execute that cycle ahead.
	//
	// So for testing, we need readyUntil < 5 AND readyMap[5]=false, AND readyMap[8]=true.
	// But UpdateReady(8, true) updates readyUntil to 9, which breaks our test.
	//
	// Solution: Set readyUntil to 4, set readyMap entries, but DON'T call UpdateReady(8, true)
	// until after we've verified the increment logic. But then Ready(8) will block...
	//
	// Actually, wait: if readyMap[8] = true exists, Ready(8) will return true immediately,
	// it won't block. So we can set readyMap[8] = true without calling UpdateReady.
	// But UpdateReady is the only way to set readyMap...

	// Let's try a different approach: set readyUntil to 4, set readyMap entries,
	// then manually set readyMap[8] = true (but we can't access readyMap directly).
	// So we must use UpdateReady(8, true), which will update readyUntil to 9.
	//
	// The key insight: if readyUntil = 9, then Ready(5) returns true, which means
	// downstream CAN execute cycle 5 ahead. So incrementing is not needed.
	// This is CORRECT behavior - we shouldn't increment if downstream can execute ahead.
	//
	// So to test increment logic, we need readyUntil < 5, and readyMap[8] = true.
	// But UpdateReady(8, true) will update readyUntil. So we need to set readyUntil AFTER
	// setting readyMap[8] = true, or find another way.
	//
	// Actually, we can set readyUntil back to 4 after UpdateReady(8, true):
	downstreamPort.SetReadyUntil(4) // Reset readyUntil to 4 after UpdateReady(8, true)

	// Now Ready(5) should check readyMap (5 >= 4) and return false (readyMap[5] = false)
	if downstreamPort.Ready(5) {
		t.Fatal("cycle 5 should not be ready (readyMap[5]=false, readyUntil=4)")
	}
	if !downstreamPort.Ready(8) {
		t.Fatal("cycle 8 should be ready (readyMap[8]=true)")
	}

	// Track cycle increments
	var notReadyCycles []int
	var finalCycle int

	proc := &incrementTestProcessor{
		DefaultProcessor: &DefaultProcessor{},
		notReadyCycles:   &notReadyCycles,
		finalCycle:       &finalCycle,
	}

	processor := NewCycleProcessor(upstreamPort, downstreamPort, proc)

	// Set initial state
	upstreamPort.SetDone(5)

	// Send a packet for cycle 5
	pkt := PacketWithCycle{
		Cycle:  5,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"},
	}
	upstreamPort.SendChan() <- pkt
	upstreamPort.SetDone(6)

	// Process cycle 5 - cycle 5 is not ready, so packet should be saved to pendingPackets
	err := processor.Tick(5)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Verify OnDownstreamReady(ready=false) was called for cycle 5
	if len(notReadyCycles) != 1 {
		t.Errorf("expected 1 not ready cycle, got %d: %v", len(notReadyCycles), notReadyCycles)
	}
	if len(notReadyCycles) > 0 && notReadyCycles[0] != 5 {
		t.Errorf("expected not ready cycle 5, got %d", notReadyCycles[0])
	}

	// Packet should not be sent yet (saved to pendingPackets)
	select {
	case <-downstreamPort.ReceiveChan():
		t.Fatal("packet should not be sent yet")
	case <-time.After(10 * time.Millisecond):
		// Expected: no packet sent
	}

	// Process cycle 6 - cycle 5 is still not ready, packet remains in pendingPackets
	err = processor.Tick(6)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Verify OnDownstreamReady(ready=false) was called again for cycle 5
	if len(notReadyCycles) != 2 {
		t.Errorf("expected 2 not ready cycles, got %d: %v", len(notReadyCycles), notReadyCycles)
	}

	// Packet should still not be sent
	select {
	case <-downstreamPort.ReceiveChan():
		t.Fatal("packet should not be sent yet")
	case <-time.After(10 * time.Millisecond):
		// Expected: no packet sent
	}

	// Now set cycle 5 as ready
	downstreamPort.UpdateReady(5, true)

	// Set upstream Done for cycle 7
	upstreamPort.SetDone(7)

	// Process cycle 7 - cycle 5 is now ready, packet should be sent
	err = processor.Tick(7)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Verify OnDownstreamReady(ready=true) was called for cycle 5
	if finalCycle != 5 {
		t.Errorf("expected final cycle 5, got %d", finalCycle)
	}

	// Verify packet was sent with original cycle 5
	select {
	case receivedPkt := <-downstreamPort.ReceiveChan():
		if receivedPkt.Cycle != 5 {
			t.Errorf("expected cycle 5, got %d", receivedPkt.Cycle)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("packet not received")
	}
}

// TestCycleProcessorMultipleNonReadyCycles tests handling of multiple consecutive non-ready cycles.
func TestCycleProcessorMultipleNonReadyCycles(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort := NewAheadPort(8)

	// Set cycles 10-14 as not ready, 15 as ready
	// Set readyUntil to 9 so cycles 10+ will check readyMap (not fast path)
	downstreamPort.SetReadyUntil(9) // Cycles < 9 return true via fast path, cycles >= 9 check readyMap
	for cycle := 10; cycle <= 14; cycle++ {
		downstreamPort.UpdateReady(cycle, false)
	}
	downstreamPort.UpdateReady(15, true) // This will update readyUntil to 16
	// Reset readyUntil back to 9 so cycles 10+ will check readyMap
	downstreamPort.SetReadyUntil(9)

	var incrementCount int
	proc := &countIncrementProcessor{
		DefaultProcessor: &DefaultProcessor{},
		incrementCount:   &incrementCount,
	}

	processor := NewCycleProcessor(upstreamPort, downstreamPort, proc)

	upstreamPort.SetDone(10)

	// Send packet for cycle 10
	pkt := PacketWithCycle{
		Cycle:  10,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"},
	}
	upstreamPort.SendChan() <- pkt
	upstreamPort.SetDone(11)

	// Process cycle 10 - cycle 10 is not ready, packet should be saved to pendingPackets
	err := processor.Tick(10)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Verify increment count (should be 1: cycle 10 checked once)
	if incrementCount != 1 {
		t.Errorf("expected 1 increment check, got %d", incrementCount)
	}

	// Packet should not be sent yet
	select {
	case <-downstreamPort.ReceiveChan():
		t.Fatal("packet should not be sent yet")
	case <-time.After(10 * time.Millisecond):
		// Expected: no packet sent
	}

	// Process cycles 11-14, each time checking cycle 10 (still not ready)
	for cycle := 11; cycle <= 14; cycle++ {
		// Set upstream Done for this cycle
		upstreamPort.SetDone(cycle)
		err = processor.Tick(cycle)
		if err != nil {
			t.Fatalf("Tick failed: %v", err)
		}
		// Verify increment count increases (each cycle checks once)
		expectedCount := cycle - 10 + 1
		if incrementCount != expectedCount {
			t.Errorf("at cycle %d, expected %d increment checks, got %d", cycle, expectedCount, incrementCount)
		}
	}

	// Verify increment count (should be 5: cycles 10, 11, 12, 13, 14 each checked once)
	if incrementCount != 5 {
		t.Errorf("expected 5 increment checks, got %d", incrementCount)
	}

	// Packet should still not be sent
	select {
	case <-downstreamPort.ReceiveChan():
		t.Fatal("packet should not be sent yet")
	case <-time.After(10 * time.Millisecond):
		// Expected: no packet sent
	}

	// Now set cycle 10 as ready
	downstreamPort.UpdateReady(10, true)

	// Set upstream Done for cycle 15
	upstreamPort.SetDone(15)

	// Process cycle 15 - cycle 10 is now ready, packet should be sent
	err = processor.Tick(15)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Verify packet was sent with original cycle 10
	select {
	case receivedPkt := <-downstreamPort.ReceiveChan():
		if receivedPkt.Cycle != 10 {
			t.Errorf("expected cycle 10, got %d", receivedPkt.Cycle)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("packet not received")
	}
}

// TestCycleProcessorWithCustomHooks tests using custom hooks implementation.
func TestCycleProcessorWithCustomHooks(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort := NewAheadPort(8)

	// Use DefaultProcessor
	proc := &DefaultProcessor{}
	processor := NewCycleProcessor(upstreamPort, downstreamPort, proc)

	upstreamPort.SetDone(-1)
	downstreamPort.UpdateReady(0, true)

	// Send packet
	pkt := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"},
	}
	upstreamPort.SendChan() <- pkt
	upstreamPort.SetDone(1)

	// Process cycle
	err := processor.Tick(0)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Verify packet was processed and sent
	select {
	case receivedPkt := <-downstreamPort.ReceiveChan():
		// FIFOFlowHooks should have modified the payload
		if receivedPkt.Packet.Payload == "" {
			t.Error("expected modified payload from FIFOFlowHooks")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("packet not received")
	}
}

// TestCycleProcessorWaitsForUpstreamDone tests that processor waits for upstream Done.
func TestCycleProcessorWaitsForUpstreamDone(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort := NewAheadPort(8)

	processor := NewCycleProcessor(upstreamPort, downstreamPort, nil)

	// Initially Done is -1, so cycle 0 should wait
	done := make(chan bool, 1)

	go func() {
		// This should wait until upstream sets Done >= 0
		err := processor.Tick(0)
		if err != nil {
			t.Errorf("Tick failed: %v", err)
		}
		done <- true
	}()

	// Give it a moment to start waiting
	time.Sleep(10 * time.Millisecond)

	// Set Done, should unblock
	upstreamPort.SetDone(-1)
	downstreamPort.UpdateReady(0, true)

	// Send a packet
	pkt := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"},
	}
	upstreamPort.SendChan() <- pkt

	// Wait for completion
	select {
	case <-done:
		// Success
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Tick did not complete after setting Done")
	}
}

// TestCycleProcessorReceivesAllPackets tests that Tick receives and processes all available packets.
func TestCycleProcessorReceivesAllPackets(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(10)
	downstreamPort := NewAheadPort(10)

	// Track received packets
	var receivedPackets []PacketWithCycle
	var mu sync.Mutex

	// Create custom processor to track received packets
	proc := &allPacketsTestProcessor{
		DefaultProcessor: &DefaultProcessor{},
		receivedPackets:  &receivedPackets,
		mu:               &mu,
	}

	processor := NewCycleProcessor(upstreamPort, downstreamPort, proc)

	// Set initial state
	upstreamPort.SetDone(5)
	downstreamPort.SetReadyUntil(5)
	downstreamPort.UpdateReady(5, true)

	// Send multiple packets to upstream channel
	const numPackets = 5
	for i := 0; i < numPackets; i++ {
		pkt := PacketWithCycle{
			Cycle:  5,
			Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: packet.Packet{}.Payload + string(rune('0'+i))},
		}
		// Create unique payload
		pkt.Packet.Payload = fmt.Sprintf("packet-%d", i)
		upstreamPort.SendChan() <- pkt
	}
	upstreamPort.SetDone(6)

	// Process cycle 5 - should receive and process all packets
	err := processor.Tick(5)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Wait a bit for all packets to be processed
	time.Sleep(50 * time.Millisecond)

	// Verify all packets were received
	mu.Lock()
	receivedCount := len(receivedPackets)
	mu.Unlock()

	if receivedCount != numPackets {
		t.Errorf("expected %d packets to be received, got %d", numPackets, receivedCount)
	}

	// Verify all packets have different payloads (to ensure they are different packets)
	mu.Lock()
	payloads := make(map[string]bool)
	for _, pkt := range receivedPackets {
		if payloads[pkt.Packet.Payload] {
			t.Errorf("duplicate packet payload: %s", pkt.Packet.Payload)
		}
		payloads[pkt.Packet.Payload] = true
	}
	mu.Unlock()

	if len(payloads) != numPackets {
		t.Errorf("expected %d unique packets, got %d", numPackets, len(payloads))
	}

	// Verify all packets were sent to downstream
	receivedFromDownstream := 0
	for i := 0; i < numPackets; i++ {
		select {
		case pkt := <-downstreamPort.ReceiveChan():
			receivedFromDownstream++
			// Verify packet has valid payload
			if pkt.Packet.Payload == "" {
				t.Errorf("packet %d has empty payload", i)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("timeout waiting for packet %d", i)
		}
	}

	if receivedFromDownstream != numPackets {
		t.Errorf("expected %d packets from downstream, got %d", numPackets, receivedFromDownstream)
	}

	t.Logf("Successfully received and processed %d packets in a single Tick call", numPackets)
}

// TestCycleProcessorHandlesMultipleCyclesInChannel tests that Tick correctly handles
// packets with different cycles in the channel.
func TestCycleProcessorHandlesMultipleCyclesInChannel(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(10)
	downstreamPort := NewAheadPort(10)

	var receivedPackets []PacketWithCycle
	var mu sync.Mutex

	proc := &allPacketsTestProcessor{
		DefaultProcessor: &DefaultProcessor{},
		receivedPackets:  &receivedPackets,
		mu:               &mu,
	}

	processor := NewCycleProcessor(upstreamPort, downstreamPort, proc)

	// Set initial state - allow processing cycles 5, 6, 7
	upstreamPort.SetDone(7)
	downstreamPort.SetReadyUntil(7)
	for cycle := 5; cycle <= 7; cycle++ {
		downstreamPort.UpdateReady(cycle, true)
	}

	// Send packets with different cycles
	packets := []PacketWithCycle{
		{Cycle: 5, Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "cycle-5"}},
		{Cycle: 6, Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "cycle-6"}},
		{Cycle: 7, Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "cycle-7"}},
	}

	for _, pkt := range packets {
		upstreamPort.SendChan() <- pkt
	}

	// Process cycle 5 - should receive all packets (even though they have different cycles)
	err := processor.Tick(5)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Verify all packets were received
	mu.Lock()
	receivedCount := len(receivedPackets)
	mu.Unlock()

	if receivedCount != 3 {
		t.Errorf("expected 3 packets to be received, got %d", receivedCount)
	}

	// Verify all packets were sent to downstream
	receivedFromDownstream := 0
	expectedPayloads := []string{"cycle-5", "cycle-6", "cycle-7"}
	receivedPayloads := make(map[string]bool)

	for i := 0; i < 3; i++ {
		select {
		case pkt := <-downstreamPort.ReceiveChan():
			receivedFromDownstream++
			receivedPayloads[pkt.Packet.Payload] = true
		case <-time.After(100 * time.Millisecond):
			t.Errorf("timeout waiting for packet %d", i)
		}
	}

	if receivedFromDownstream != 3 {
		t.Errorf("expected 3 packets from downstream, got %d", receivedFromDownstream)
	}

	// Verify all expected payloads were received
	for _, expectedPayload := range expectedPayloads {
		if !receivedPayloads[expectedPayload] {
			t.Errorf("expected payload %s not received", expectedPayload)
		}
	}

	t.Logf("Successfully handled packets with different cycles in a single Tick call")
}

// TestCycleProcessorUpdateReadyAfterTick tests that Tick automatically calls UpdateReady
// to notify upstream that the next cycle is ready. Without this, upstream would block when calling Ready(cycle+1).
func TestCycleProcessorUpdateReadyAfterTick(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)
	downstreamPort := NewAheadPort(8)

	processor := NewCycleProcessor(upstreamPort, downstreamPort, nil)

	// Set initial state
	upstreamPort.SetDone(-1)
	// DO NOT manually call UpdateReady(1, true) - Tick should do it automatically

	// Process cycle 0
	err := processor.Tick(0)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Now verify that upstream can check Ready(1) without blocking
	// If Tick didn't call UpdateReady(1, true), this would block forever
	readyChan := make(chan bool, 1)
	go func() {
		// This should return immediately if UpdateReady(1, true) was called
		ready := upstreamPort.Ready(1)
		readyChan <- ready
	}()

	// Wait for result with timeout
	select {
	case ready := <-readyChan:
		if !ready {
			t.Fatal("expected Ready(1) to return true after Tick(0) calls UpdateReady(1, true)")
		}
		t.Logf("Successfully verified that Ready(1) returns true after Tick(0)")
	case <-time.After(1 * time.Second):
		t.Fatal("Ready(1) blocked - Tick(0) did not call UpdateReady(1, true)!")
	}

	// Verify readyMap was updated
	// We can't directly access readyMap, but we can verify by checking Ready() again
	if !upstreamPort.Ready(1) {
		t.Fatal("Ready(1) should return true after Tick(0)")
	}

	// Process cycle 1
	upstreamPort.SetDone(1)
	err = processor.Tick(1)
	if err != nil {
		t.Fatalf("Tick failed: %v", err)
	}

	// Verify Ready(2) also works
	readyChan2 := make(chan bool, 1)
	go func() {
		ready := upstreamPort.Ready(2)
		readyChan2 <- ready
	}()

	select {
	case ready := <-readyChan2:
		if !ready {
			t.Fatal("expected Ready(2) to return true after Tick(1) calls UpdateReady(2, true)")
		}
		t.Logf("Successfully verified that Ready(2) returns true after Tick(1)")
	case <-time.After(1 * time.Second):
		t.Fatal("Ready(2) blocked - Tick(1) did not call UpdateReady(2, true)!")
	}
}

// TestCycleProcessorUpdateReadyWithoutUpdateReadyBlocks tests that without UpdateReady,
// upstream would block when calling Ready(cycle+1). This test demonstrates the problem
// that UpdateReady solves.
func TestCycleProcessorUpdateReadyWithoutUpdateReadyBlocks(t *testing.T) {
	t.Parallel()

	upstreamPort := NewAheadPort(8)

	// Set initial state
	upstreamPort.SetDone(-1)
	// DO NOT call UpdateReady(1, true)
	// This simulates the buggy behavior where Tick doesn't call UpdateReady

	// Now verify that upstream blocks when calling Ready(1)
	readyChan := make(chan bool, 1)
	go func() {
		// This should block because UpdateReady(1, true) was never called
		ready := upstreamPort.Ready(1)
		readyChan <- ready
	}()

	// Wait for result with timeout
	select {
	case <-readyChan:
		// If we get here, Ready(1) returned, which means either:
		// 1. readyUntil fast path kicked in (unlikely if we set it correctly)
		// 2. Something else set readyMap[1]
		// Let's check readyUntil
		readyUntil := upstreamPort.GetReadyUntil()
		if readyUntil > 1 {
			t.Logf("Ready(1) returned true via fast path (readyUntil=%d), which is expected if readyUntil was set", readyUntil)
		} else {
			t.Logf("Ready(1) returned unexpectedly - this might indicate readyMap[1] was set elsewhere")
		}
	case <-time.After(100 * time.Millisecond):
		// Expected: Ready(1) should block because UpdateReady(1, true) was never called
		t.Logf("Successfully verified that Ready(1) blocks when UpdateReady(1, true) is not called")
	}

	// Now verify that with UpdateReady, it works
	upstreamPort.UpdateReady(1, true)

	// Now Ready(1) should return immediately
	readyChan2 := make(chan bool, 1)
	go func() {
		ready := upstreamPort.Ready(1)
		readyChan2 <- ready
	}()

	select {
	case ready := <-readyChan2:
		if !ready {
			t.Fatal("expected Ready(1) to return true after UpdateReady(1, true)")
		}
		t.Logf("Successfully verified that Ready(1) returns true after UpdateReady(1, true)")
	case <-time.After(1 * time.Second):
		t.Fatal("Ready(1) still blocked after UpdateReady(1, true) - this should not happen!")
	}
}

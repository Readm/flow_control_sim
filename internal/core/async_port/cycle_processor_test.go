package async_port

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// testHooks is a test implementation of CycleProcessorHooks
type testHooks struct {
	*DefaultHooks
	hookCalls *struct {
		mu                      sync.Mutex
		cycleStarts             []int
		dataReceived            []PacketWithCycle
		backpressureIndependent []PacketWithCycle
		downstreamReady         []PacketWithCycle
		downstreamNotReady      []int
		cycleEnds               []int
	}
}

func (t *testHooks) OnCycleStart(cycle int) {
	t.hookCalls.mu.Lock()
	t.hookCalls.cycleStarts = append(t.hookCalls.cycleStarts, cycle)
	t.hookCalls.mu.Unlock()
}

func (t *testHooks) OnDataReceived(pkt PacketWithCycle, cycle int) {
	t.hookCalls.mu.Lock()
	t.hookCalls.dataReceived = append(t.hookCalls.dataReceived, pkt)
	t.hookCalls.mu.Unlock()
}

func (t *testHooks) OnDownstreamBackpressureIndependentLogic(pkt PacketWithCycle, cycle int) PacketWithCycle {
	t.hookCalls.mu.Lock()
	t.hookCalls.backpressureIndependent = append(t.hookCalls.backpressureIndependent, pkt)
	t.hookCalls.mu.Unlock()
	return pkt
}

func (t *testHooks) OnDownstreamReady(pkt PacketWithCycle, cycle int) {
	t.hookCalls.mu.Lock()
	t.hookCalls.downstreamReady = append(t.hookCalls.downstreamReady, pkt)
	t.hookCalls.mu.Unlock()
}

func (t *testHooks) OnDownstreamNotReady(pkt PacketWithCycle, cycle int) int {
	t.hookCalls.mu.Lock()
	t.hookCalls.downstreamNotReady = append(t.hookCalls.downstreamNotReady, cycle)
	t.hookCalls.mu.Unlock()
	return cycle + 1
}

func (t *testHooks) OnCycleEnd(cycle int) {
	t.hookCalls.mu.Lock()
	t.hookCalls.cycleEnds = append(t.hookCalls.cycleEnds, cycle)
	t.hookCalls.mu.Unlock()
}

// incrementTestHooks is a test hooks implementation for cycle increment testing
type incrementTestHooks struct {
	*DefaultHooks
	notReadyCycles *[]int
	finalCycle     *int
}

func (i *incrementTestHooks) OnDownstreamNotReady(pkt PacketWithCycle, cycle int) int {
	*i.notReadyCycles = append(*i.notReadyCycles, cycle)
	return cycle + 1
}

func (i *incrementTestHooks) OnDownstreamReady(pkt PacketWithCycle, cycle int) {
	*i.finalCycle = int(pkt.Cycle)
}

// countIncrementHooks is a test hooks implementation for counting increments
type countIncrementHooks struct {
	*DefaultHooks
	incrementCount *int
}

func (c *countIncrementHooks) OnDownstreamNotReady(pkt PacketWithCycle, cycle int) int {
	*c.incrementCount++
	return cycle + 1
}

// allPacketsTestHooks tracks all received packets for testing
type allPacketsTestHooks struct {
	*DefaultHooks
	receivedPackets *[]PacketWithCycle
	mu             *sync.Mutex
}

func (a *allPacketsTestHooks) OnDataReceived(pkt PacketWithCycle, cycle int) {
	a.mu.Lock()
	*a.receivedPackets = append(*a.receivedPackets, pkt)
	a.mu.Unlock()
}

// TestCycleProcessorBasicFlow tests the basic cycle processing workflow.
func TestCycleProcessorBasicFlow(t *testing.T) {
	t.Parallel()

	upstreamPort := NewPort(8)
	downstreamPort := NewPort(8)

	// Track hook calls
	var hookCalls struct {
		mu                      sync.Mutex
		cycleStarts             []int
		dataReceived            []PacketWithCycle
		backpressureIndependent []PacketWithCycle
		downstreamReady         []PacketWithCycle
		downstreamNotReady      []int
		cycleEnds               []int
	}

	// Create a custom hooks implementation for testing
	hooks := &testHooks{
		hookCalls: &hookCalls,
	}

	processor := NewCycleProcessor(upstreamPort, downstreamPort, hooks)

	// Set initial state
	upstreamPort.SetDoneUntil(0)
	downstreamPort.UpdateReady(0, true)

	// Send a packet from upstream
	pkt := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"},
	}
	upstreamPort.Chan() <- pkt
	upstreamPort.SetDoneUntil(1)

	// Process cycle 0
	err := processor.ProcessCycle(0)
	if err != nil {
		t.Fatalf("ProcessCycle failed: %v", err)
	}

	// Verify hook calls
	hookCalls.mu.Lock()
	if len(hookCalls.cycleStarts) != 1 || hookCalls.cycleStarts[0] != 0 {
		t.Errorf("expected OnCycleStart(0), got %v", hookCalls.cycleStarts)
	}
	if len(hookCalls.dataReceived) != 1 {
		t.Errorf("expected 1 data received, got %d", len(hookCalls.dataReceived))
	}
	if len(hookCalls.downstreamReady) != 1 {
		t.Errorf("expected 1 downstream ready, got %d", len(hookCalls.downstreamReady))
	}
	if len(hookCalls.downstreamNotReady) != 0 {
		t.Errorf("expected 0 downstream not ready, got %d", len(hookCalls.downstreamNotReady))
	}
	if len(hookCalls.cycleEnds) != 1 || hookCalls.cycleEnds[0] != 0 {
		t.Errorf("expected OnCycleEnd(0), got %v", hookCalls.cycleEnds)
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

	upstreamPort := NewPort(8)
	downstreamPort := NewPort(8)

	// Set downstream cycles 5, 6, 7 as not ready, 8 as ready
	// Important: readyUntil represents cycles that downstream can execute ahead.
	// If readyUntil >= cycle, Ready(cycle) returns true via fast path (correct behavior).
	// To test cycle increment logic, we need readyUntil < 5, so Ready(5) checks readyMap.
	// Note: UpdateReady(8, true) will update readyUntil to 9, but that's OK because
	// incrementCycleUntilReady will check Ready(5), Ready(6), Ready(7) before checking Ready(8).
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

	hooks := &incrementTestHooks{
		DefaultHooks:   &DefaultHooks{},
		notReadyCycles: &notReadyCycles,
		finalCycle:     &finalCycle,
	}

	processor := NewCycleProcessor(upstreamPort, downstreamPort, hooks)

	// Set initial state
	upstreamPort.SetDoneUntil(5)

	// Send a packet for cycle 5
	pkt := PacketWithCycle{
		Cycle:  5,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"},
	}
	upstreamPort.Chan() <- pkt
	upstreamPort.SetDoneUntil(6)

	// Process cycle 5 - this should increment cycle from 5 to 8
	// (because cycles 5, 6, 7 are not ready, but 8 will be set as ready)
	err := processor.ProcessCycle(5)
	if err != nil {
		t.Fatalf("ProcessCycle failed: %v", err)
	}

	// Now set cycle 8 as ready (after processing, to verify the packet was sent with cycle 8)
	// This will update readyUntil to 9, but that's OK since we've already processed
	downstreamPort.UpdateReady(8, true) // readyMap[8] = true, readyUntil becomes 9

	// Verify cycle was incremented
	if finalCycle != 8 {
		t.Errorf("expected final cycle 8, got %d", finalCycle)
	}

	// Verify OnDownstreamNotReady was called for cycles 5, 6, 7
	if len(notReadyCycles) != 3 {
		t.Errorf("expected 3 not ready cycles, got %d: %v", len(notReadyCycles), notReadyCycles)
	}

	// Verify packet was sent with incremented cycle
	select {
	case receivedPkt := <-downstreamPort.ReceiveChan():
		if receivedPkt.Cycle != 8 {
			t.Errorf("expected cycle 8, got %d", receivedPkt.Cycle)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("packet not received")
	}
}

// TestCycleProcessorMultipleNonReadyCycles tests handling of multiple consecutive non-ready cycles.
func TestCycleProcessorMultipleNonReadyCycles(t *testing.T) {
	t.Parallel()

	upstreamPort := NewPort(8)
	downstreamPort := NewPort(8)

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
	hooks := &countIncrementHooks{
		DefaultHooks:   &DefaultHooks{},
		incrementCount: &incrementCount,
	}

	processor := NewCycleProcessor(upstreamPort, downstreamPort, hooks)

	upstreamPort.SetDoneUntil(10)

	// Send packet for cycle 10
	pkt := PacketWithCycle{
		Cycle:  10,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"},
	}
	upstreamPort.Chan() <- pkt
	upstreamPort.SetDoneUntil(11)

	// Process cycle 10
	err := processor.ProcessCycle(10)
	if err != nil {
		t.Fatalf("ProcessCycle failed: %v", err)
	}

	// Verify increment count (should be 5: cycles 10, 11, 12, 13, 14)
	if incrementCount != 5 {
		t.Errorf("expected 5 increments, got %d", incrementCount)
	}

	// Verify packet sent with cycle 15
	select {
	case receivedPkt := <-downstreamPort.ReceiveChan():
		if receivedPkt.Cycle != 15 {
			t.Errorf("expected cycle 15, got %d", receivedPkt.Cycle)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("packet not received")
	}
}

// TestCycleProcessorWithCustomHooks tests using custom hooks implementation.
func TestCycleProcessorWithCustomHooks(t *testing.T) {
	t.Parallel()

	upstreamPort := NewPort(8)
	downstreamPort := NewPort(8)

	// Use FIFOFlowHooks as example
	hooks := NewFIFOFlowHooks(1)
	processor := NewCycleProcessor(upstreamPort, downstreamPort, hooks)

	upstreamPort.SetDoneUntil(0)
	downstreamPort.UpdateReady(0, true)

	// Send packet
	pkt := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"},
	}
	upstreamPort.Chan() <- pkt
	upstreamPort.SetDoneUntil(1)

	// Process cycle
	err := processor.ProcessCycle(0)
	if err != nil {
		t.Fatalf("ProcessCycle failed: %v", err)
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

// TestCycleProcessorWaitsForUpstreamDoneUntil tests that processor waits for upstream DoneUntil.
func TestCycleProcessorWaitsForUpstreamDoneUntil(t *testing.T) {
	t.Parallel()

	upstreamPort := NewPort(8)
	downstreamPort := NewPort(8)

	hooks := &DefaultHooks{}
	processor := NewCycleProcessor(upstreamPort, downstreamPort, hooks)

	// Initially DoneUntil is -1, so cycle 0 should wait
	done := make(chan bool, 1)

	go func() {
		// This should wait until upstream sets DoneUntil >= 0
		err := processor.ProcessCycle(0)
		if err != nil {
			t.Errorf("ProcessCycle failed: %v", err)
		}
		done <- true
	}()

	// Give it a moment to start waiting
	time.Sleep(10 * time.Millisecond)

	// Set DoneUntil, should unblock
	upstreamPort.SetDoneUntil(0)
	downstreamPort.UpdateReady(0, true)

	// Send a packet
	pkt := PacketWithCycle{
		Cycle:  0,
		Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: "test"},
	}
	upstreamPort.Chan() <- pkt

	// Wait for completion
	select {
	case <-done:
		// Success
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ProcessCycle did not complete after setting DoneUntil")
	}
}

// TestCycleProcessorReceivesAllPackets tests that ProcessCycle receives and processes all available packets.
func TestCycleProcessorReceivesAllPackets(t *testing.T) {
	t.Parallel()

	upstreamPort := NewPort(10)
	downstreamPort := NewPort(10)

	// Track received packets
	var receivedPackets []PacketWithCycle
	var mu sync.Mutex

	// Create custom hooks to track received packets
	hooks := &allPacketsTestHooks{
		DefaultHooks:     &DefaultHooks{},
		receivedPackets:  &receivedPackets,
		mu:               &mu,
	}

	processor := NewCycleProcessor(upstreamPort, downstreamPort, hooks)

	// Set initial state
	upstreamPort.SetDoneUntil(5)
	downstreamPort.SetReadyUntil(5)
	downstreamPort.UpdateReady(5, true)

	// Send multiple packets to upstream channel
	const numPackets = 5
	for i := 0; i < numPackets; i++ {
		pkt := PacketWithCycle{
			Cycle:  uint64(5),
			Packet: packet.Packet{SourceID: 0, TargetID: 1, Payload: packet.Packet{}.Payload + string(rune('0'+i))},
		}
		// Create unique payload
		pkt.Packet.Payload = fmt.Sprintf("packet-%d", i)
		upstreamPort.Chan() <- pkt
	}
	upstreamPort.SetDoneUntil(6)

	// Process cycle 5 - should receive and process all packets
	err := processor.ProcessCycle(5)
	if err != nil {
		t.Fatalf("ProcessCycle failed: %v", err)
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

	t.Logf("Successfully received and processed %d packets in a single ProcessCycle call", numPackets)
}

// TestCycleProcessorHandlesMultipleCyclesInChannel tests that ProcessCycle correctly handles
// packets with different cycles in the channel.
func TestCycleProcessorHandlesMultipleCyclesInChannel(t *testing.T) {
	t.Parallel()

	upstreamPort := NewPort(10)
	downstreamPort := NewPort(10)

	var receivedPackets []PacketWithCycle
	var mu sync.Mutex

	hooks := &allPacketsTestHooks{
		DefaultHooks:    &DefaultHooks{},
		receivedPackets: &receivedPackets,
		mu:              &mu,
	}

	processor := NewCycleProcessor(upstreamPort, downstreamPort, hooks)

	// Set initial state - allow processing cycles 5, 6, 7
	upstreamPort.SetDoneUntil(7)
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
		upstreamPort.Chan() <- pkt
	}

	// Process cycle 5 - should receive all packets (even though they have different cycles)
	err := processor.ProcessCycle(5)
	if err != nil {
		t.Fatalf("ProcessCycle failed: %v", err)
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

	t.Logf("Successfully handled packets with different cycles in a single ProcessCycle call")
}

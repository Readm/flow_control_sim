package link

import "testing"

// TestFlowControlInterface is a placeholder test for the FlowControlStrategy interface.
// Actual implementations will have their own test files.
func TestFlowControlInterface(t *testing.T) {
	t.Skip("Interface definition test - implementations tested separately")
}

// mockFlowControl is a mock implementation for testing purposes.
// This can be used by other tests to mock flow control behavior.
type mockFlowControl struct {
	canAccept bool
	canSend   bool
	ready     bool

	// Track calls for verification
	acceptedCount int
	blockedCount  int
	sentCount     int
}

func newMockFlowControl(canAccept, canSend, ready bool) *mockFlowControl {
	return &mockFlowControl{
		canAccept: canAccept,
		canSend:   canSend,
		ready:     ready,
	}
}

func (m *mockFlowControl) CanAcceptPacket(cycle, targetCycle int) bool {
	return m.canAccept
}

func (m *mockFlowControl) OnPacketAccepted(cycle, targetCycle int) {
	m.acceptedCount++
}

func (m *mockFlowControl) OnPacketBlocked(cycle, targetCycle int) {
	m.blockedCount++
}

func (m *mockFlowControl) CanSendPacket(cycle int, downstreamReady bool) bool {
	return m.canSend && downstreamReady
}

func (m *mockFlowControl) OnPacketSent(cycle int) {
	m.sentCount++
}

func (m *mockFlowControl) GetReadyForCycle(cycle int) bool {
	return m.ready
}

func (m *mockFlowControl) Reset() {
	m.acceptedCount = 0
	m.blockedCount = 0
	m.sentCount = 0
}

// TestMockFlowControl verifies the mock implementation.
func TestMockFlowControl(t *testing.T) {
	mock := newMockFlowControl(true, true, true)

	if !mock.CanAcceptPacket(0, 2) {
		t.Error("Mock should accept packets when configured")
	}

	mock.OnPacketAccepted(0, 2)
	if mock.acceptedCount != 1 {
		t.Errorf("Expected acceptedCount=1, got %d", mock.acceptedCount)
	}

	mock.OnPacketBlocked(0, 2)
	if mock.blockedCount != 1 {
		t.Errorf("Expected blockedCount=1, got %d", mock.blockedCount)
	}

	if !mock.CanSendPacket(0, true) {
		t.Error("Mock should allow sending when configured")
	}

	mock.OnPacketSent(0)
	if mock.sentCount != 1 {
		t.Errorf("Expected sentCount=1, got %d", mock.sentCount)
	}

	if !mock.GetReadyForCycle(5) {
		t.Error("Mock should be ready when configured")
	}

	mock.Reset()
	if mock.acceptedCount != 0 || mock.blockedCount != 0 || mock.sentCount != 0 {
		t.Error("Reset should clear counters")
	}
}

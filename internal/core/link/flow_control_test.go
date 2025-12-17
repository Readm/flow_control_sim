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

func (m *mockFlowControl) CanSendPacket(cycle int, downstreamReady bool) bool {
	return m.canSend && downstreamReady
}

func (m *mockFlowControl) IsReady(cycle int) bool {
	return m.ready
}

func (m *mockFlowControl) Reset() {
	// No-op for mock
}

// TestMockFlowControl verifies the mock implementation.
func TestMockFlowControl(t *testing.T) {
	mock := newMockFlowControl(true, true, true)

	if !mock.CanAcceptPacket(0, 2) {
		t.Error("Mock should accept packets when configured")
	}

	if !mock.CanSendPacket(0, true) {
		t.Error("Mock should allow sending when configured")
	}

	if !mock.IsReady(5) {
		t.Error("Mock should be ready when configured")
	}

	mock.Reset()
}

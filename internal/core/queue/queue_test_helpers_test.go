package queue

import (
	"testing"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
	"github.com/Readm/flow_sim/internal/testing/testutils"
)

type mockUpstream = testutils.MockUpstream
type mockDownstream = testutils.MockDownstream

func newMockUpstream() *mockUpstream {
	return testutils.NewMockUpstream()
}

func newMockDownstream() *mockDownstream {
	return testutils.NewMockDownstream()
}

func createTestConnection() (*ahead_port.Port, *mockUpstream, *mockDownstream) {
	upstream := newMockUpstream()
	downstream := newMockDownstream()
	port := ahead_port.Connect(upstream, downstream)
	return port, upstream, downstream
}

func assertPacketsEqual(t *testing.T, got, want []packet.Packet) {
	testutils.AssertPacketsEqual(t, got, want)
}

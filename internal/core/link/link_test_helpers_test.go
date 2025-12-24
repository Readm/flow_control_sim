package link

import (
	"testing"

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

func assertPacketsEqual(t *testing.T, got, want []packet.Packet) {
	testutils.AssertPacketsEqual(t, got, want)
}

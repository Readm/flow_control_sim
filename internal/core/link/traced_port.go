package link

import (
	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/trace"
)

// TracedInPort wraps an ahead_port.InPort to add tracing.
// It intercepts blocking calls like IsReady to record specific trace events.
type TracedInPort struct {
	original ahead_port.InPort
	link     *Link // Back reference to Link for access to tracer and buffer
}

// Ensure interface compliance
var _ ahead_port.InPort = (*TracedInPort)(nil)

func NewTracedInPort(original ahead_port.InPort, link *Link) *TracedInPort {
	return &TracedInPort{
		original: original,
		link:     link,
	}
}

// TrySend attempts to send a packet.
func (t *TracedInPort) TrySend(cycle int, pkt ahead_port.PacketWithCycle) bool {
	// Optional: We could trace "TrySend" duration here too if desired.
	// For now, focus on WaitReady.
	return t.original.TrySend(cycle, pkt)
}

// MarkDone marks the cycle as done.
func (t *TracedInPort) MarkDone(cycle int) {
	t.original.MarkDone(cycle)
}

// PeekReady checks readiness without blocking.
func (t *TracedInPort) PeekReady(cycle int) (bool, bool) {
	return t.original.PeekReady(cycle)
}

// IsReady blocks until downstream is ready. This is where we trace.
func (t *TracedInPort) IsReady(cycle int) bool {
	// 1. Pause Parent (Process)
	// We might be called from Process, so we pause it to create a hole.
	if t.link != nil {
		t.link.PauseProcess()
	}

	// 2. Do Blocking Call
	start := float64(trace.GetCPUCycles())
	ready := t.original.IsReady(cycle)
	end := float64(trace.GetCPUCycles())

	// 3. Record Wait Event (Exclusive)
	// Only record if significant duration (>0.5us)
	if end-start > 0.5 {
		if t.link != nil {
			t.link.RecordWait(start, end, cycle)
		}
	}

	// 4. Resume Parent (Process)
	// Only resume if we are still active (Link checks tracer inside Resume)
	if t.link != nil {
		t.link.ResumeProcess()
	}

	return ready
}

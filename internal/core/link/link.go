package link

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/debug"
	"github.com/Readm/flow_sim/internal/core/monitor"
	"github.com/Readm/flow_sim/internal/core/trace"
	"github.com/Readm/flow_sim/internal/core/visualization"
	"github.com/Readm/flow_sim/internal/core/visualization/protocol"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// CreateLinkType is a factory function that creates link types by name.
//
// Supported link types:
// - "buffered": BufferedLinkType with ring buffer and backpressure
// - "bufferless": BufferlessLinkType (always-ready, no physical buffering)
//
// Parameters:
// - typeName: type of link ("buffered", "bufferless")
// - latency: latency parameter (used by buffered, ignored by bufferless)
// - bandwidth: bandwidth parameter (used by buffered, ignored by bufferless)
//
// Returns:
// - LinkType instance, or panic if typeName is unknown
func CreateLinkType(typeName string, latency, bandwidth int) LinkType {
	switch typeName {
	case "buffered":
		return NewBufferedLinkType(latency, bandwidth)
	case "bufferless":
		return NewBufferlessLinkType()
	default:
		panic(fmt.Sprintf("unknown link type: %s", typeName))
	}
}

// CreateLinkHandler is deprecated. Use CreateLinkType instead.
// Deprecated: Use CreateLinkType.
func CreateLinkHandler(handlerType string, latency, bandwidth int) LinkHandler {
	return CreateLinkType(handlerType, latency, bandwidth)
}

// Link represents a directed edge in the topology.
// Link receives packets from upstream and forwards them to downstream with latency and bandwidth constraints.
type Link struct {
	sourceID     int
	sourcePortID int // 源端口 ID
	targetID     int
	targetPortID int // 目标端口 ID

	// ===== Port references (not owned by Link) =====
	fromUpstream ahead_port.OutPort // Receive from upstream
	toDownstream ahead_port.InPort  // Send to downstream

	// ===== Profiling: Port references for sync profiling =====
	upstreamPort   *ahead_port.Port // Port from OutputQueue to Link (for WaitDone profiling)
	downstreamPort *ahead_port.Port // Port from Link to InputQueue (for Ready profiling)

	// ===== Link type (buffered/bufferless strategy) =====
	linkType LinkType

	// ===== Link parameters =====
	latency      int
	bandwidth    int
	currentCycle int
	tickHook     func(cycle int)

	// ===== Protocol 配置引用 (只读,直接引用 protocol.Edge) =====
	configRef *protocol.Edge

	// ===== Business and Display data =====
	// Phase 2: 业务数据和 DisplayData 已移至 configRef
	// 移除了 edgeID, packetTypes, displayData 字段

	// ===== Monitor =====
	monitor             *monitor.LinkMonitor
	currentProcessStart float64
}

// NewLink creates a Link with BufferedLinkType by default.
func NewLink(sourceID, targetID, latency, bandwidth int) *Link {
	linkType := NewBufferedLinkType(latency, bandwidth)
	return NewLinkWithPortIDs(sourceID, 0, targetID, 0, latency, bandwidth, linkType)
}

// NewLinkWithType creates a new Link with a custom link type.
func NewLinkWithType(sourceID, targetID, latency, bandwidth int, linkType LinkType) *Link {
	return NewLinkWithPortIDs(sourceID, 0, targetID, 0, latency, bandwidth, linkType)
}

// NewLinkWithPortIDs creates a new Link with explicit port IDs.
func NewLinkWithPortIDs(sourceID, sourcePortID, targetID, targetPortID, latency, bandwidth int, linkType LinkType) *Link {
	if latency <= 0 {
		panic("latency must be positive")
	}
	if bandwidth <= 0 {
		panic("bandwidth must be positive")
	}
	if linkType == nil {
		panic("linkType must not be nil")
	}

	// Calculate unique link ID (as in original code)
	linkID := 200000 + sourceID*1000 + targetID

	return &Link{
		sourceID:     sourceID,
		sourcePortID: sourcePortID,
		targetID:     targetID,
		targetPortID: targetPortID,
		latency:      latency,
		bandwidth:    bandwidth,
		linkType:     linkType,
		currentCycle: 0,
		monitor:      monitor.NewLinkMonitor(linkID),
	}
}

// NewLinkWithHandler is deprecated. Use NewLinkWithType instead.
// Deprecated: Use NewLinkWithType.
func NewLinkWithHandler(sourceID, targetID, latency, bandwidth int, handler LinkHandler) *Link {
	return NewLinkWithType(sourceID, targetID, latency, bandwidth, handler)
}

// SetUpstreamPort sets the port for receiving data from upstream.
func (l *Link) SetUpstreamPort(port ahead_port.OutPort) {
	l.fromUpstream = port
	// Try to get the concrete Port for profiling
	if p, ok := port.(*ahead_port.Port); ok {
		l.upstreamPort = p
	}
	// No default ready signals. Handlers must provide them in their Process method.
}

// SetDownstreamPort sets the port for sending data to downstream.
func (l *Link) SetDownstreamPort(port ahead_port.InPort) {
	l.toDownstream = port
	// Try to get the concrete Port for profiling
	if p, ok := port.(*ahead_port.Port); ok {
		l.downstreamPort = p
	}
}

// UpdateUpstreamReady updates the ready state of the upstream port for a specific cycle.
// This is useful for custom LinkHandlers to signal backpressure.
func (l *Link) UpdateUpstreamReady(cycle int, ready bool) {
	if l.fromUpstream != nil {
		l.fromUpstream.UpdateReady(cycle, ready)
	}
}

// Init initializes the link after being connected to the network.
func (l *Link) Init() {
	l.linkType.Init(l)
}

// SourceID returns the ID of the upstream node.
func (l *Link) SourceID() int { return l.sourceID }

// TargetID returns the ID of the downstream node.
func (l *Link) TargetID() int { return l.targetID }

// Latency returns the configured delay in cycles.
func (l *Link) Latency() int { return l.latency }

// Bandwidth returns the maximum packets per cycle.
func (l *Link) Bandwidth() int { return l.bandwidth }

// GetHandler returns the handler for this link.
func (l *Link) GetHandler() LinkHandler {
	return l.linkType
}

// CurrentCycle returns the current simulation cycle of the link.
func (l *Link) CurrentCycle() int {
	return l.currentCycle
}

// SnapshotOccupancy reports the pending packet count per slot/offset.
func (l *Link) SnapshotOccupancy() []int {
	return l.linkType.GetOccupancy(l.currentCycle)
}

// Tick processes a single cycle.
// Template: Receive -> handler.Process -> MarkDone
func (l *Link) Tick(cycle int, targetCycle int) error {
	// ===== 1. Phase 1: Receive packets from upstream =====
	var incoming []packet.Packet
	waitCycle := cycle - l.latency
	if l.fromUpstream != nil && waitCycle >= 0 {
		ts := l.monitor.OnReceiveStart() // TRACE
		incoming = l.fromUpstream.Receive(waitCycle)
		l.monitor.OnReceiveEnd(ts, uint64(cycle), len(incoming)) // TRACE

		debug.Logf("Link %d->%d: Tick(%d) received %d packets from waitCycle=%d", l.sourceID, l.targetID, cycle, len(incoming), waitCycle)
	}

	// ===== 2. Phase 2: Process via Handler (Core Logic) =====
	l.currentCycle = cycle
	procToken := l.monitor.OnProcessStart() // TRACE

	if err := l.linkType.Process(l, cycle, targetCycle, incoming); err != nil {
		return fmt.Errorf("link %d->%d handler failed: %w", l.sourceID, l.targetID, err)
	}

	// If process was NOT paused, we end it normally.
	// How do we know if it was paused? LinkMonitor tracks state?
	// The original code checked l.currentProcessStart != 0.
	// We need logic here.
	// If LinkMonitor handles Pause, it should clear its internal state.
	// But `monitor.OnProcessEnd` doesn't know about pause state of `procToken`.
	// Let's assume for now Process is always atomic unless `PauseProcess` is called, which handles the End event itself.
	// But we need to know if we should call OnProcessEnd.
	// We can check if `l.monitor` thinks we are active? No.
	// Simplest: `PauseProcess` in monitor sets a flag?
	// Or we just call OnProcessEnd and Monitor ignores if invalid time?
	// The original code:
	// if l.currentProcessStart != 0 { l.traceProcessEnd(...) }
	// So `PauseProcess` sets `currentProcessStart = 0`.
	// We should expose this or check this.
	// Actually, `procToken` IS the start time.
	// If `PauseProcess` was called effectively, we probably don't want to call OnProcessEnd with the OLD token?
	// Let's assume standard behavior for now. If specialized LinkHandlers call PauseProcess, they interact with Monitor.
	// We'll unconditionally call OnProcessEnd, and let Monitor handle it?
	// But `OnProcessEnd` takes `procToken`.
	// We should probably rely on `LinkMonitor` to handle state if we want to be clean.
	// But `LinkMonitor` API `OnProcessEnd` is stateless regarding current running process.

	l.monitor.OnProcessEnd(procToken, uint64(cycle))

	// ===== 3. Phase 3: Mark this cycle as done for downstream =====
	if l.toDownstream != nil {
		tsSend := l.monitor.OnSendStart() // TRACE
		l.toDownstream.MarkDone(cycle)
		l.monitor.OnSendEnd(tsSend, uint64(cycle)) // TRACE
	}

	l.invokeTickHook(cycle)
	return nil
}

// PauseProcess ends the current process event temporarily.
func (l *Link) PauseProcess() {
	if l.currentProcessStart != 0 {
		l.monitor.PauseProcess(l.currentProcessStart, l.currentCycle)
		l.currentProcessStart = 0
	}
}

// AdvanceTo progresses the link up to and including the target cycle.
func (l *Link) AdvanceTo(targetCycle int) error {
	if targetCycle < l.currentCycle {
		return nil
	}

	debug.Logf("Link.AdvanceTo: link=%d->%d, target=%d, starting from cycle=%d", l.sourceID, l.targetID, targetCycle, l.currentCycle)

	for cycle := l.currentCycle; cycle <= targetCycle; cycle++ {
		debug.Logf("Link.AdvanceTo: link=%d->%d, executing cycle=%d", l.sourceID, l.targetID, cycle)

		if err := l.Tick(cycle, targetCycle); err != nil {
			debug.Logf("Link.AdvanceTo: link=%d->%d, cycle=%d failed: %v", l.sourceID, l.targetID, cycle, err)
			return err
		}
		l.currentCycle = cycle + 1
		debug.Logf("Link.AdvanceTo: link=%d->%d, cycle=%d completed", l.sourceID, l.targetID, cycle)
	}
	debug.Logf("Link.AdvanceTo: link=%d->%d, reached cycle=%d (next=%d)", l.sourceID, l.targetID, targetCycle, l.currentCycle)
	return nil
}

// SetTickHook registers a callback invoked after each successful Tick.
func (l *Link) SetTickHook(hook func(cycle int)) {
	l.tickHook = hook
}

func (l *Link) invokeTickHook(cycle int) {
	if l.tickHook != nil {
		l.tickHook(cycle)
	}
}

// GetVisualState returns the visual representation of this link.
func (l *Link) GetVisualState() string {
	if visualization.VisualizationMode == "none" {
		return ""
	}
	return ""
}

// PendingPacketCount returns the number of buffered packets.
func (l *Link) PendingPacketCount() int {
	occ := l.SnapshotOccupancy()
	count := 0
	for _, n := range occ {
		count += n
	}
	return count
}

// ===== Profiling Getters =====

// GetUpstreamPort returns the upstream Port for profiling.
func (l *Link) GetUpstreamPort() *ahead_port.Port {
	return l.upstreamPort
}

// GetDownstreamPort returns the downstream Port for profiling.
func (l *Link) GetDownstreamPort() *ahead_port.Port {
	return l.downstreamPort
}

// ===== TraceSource Implementation =====

// SetTracer registers the link with the global tracer.
func (l *Link) SetTracer(t *trace.TraceRecorder) {
	l.monitor.SetTracer(t)
	if t != nil {
		t.RegisterSource(l)
		// Apply Port Decorator for downstream tracing
		// This might need access to tracer?
		// We can keep this logic here as it modifies `l.toDownstream`.
		// But ideally `LinkMonitor` handles it?
		// No, `LinkMonitor` doesn't own `toDownstream`.
		if l.toDownstream != nil {
			l.toDownstream = NewTracedInPort(l.toDownstream, l)
		}
	}
}

func (l *Link) GetTraceEvents() []trace.TraceEvent {
	return l.monitor.GetTraceEvents()
}

func (l *Link) ID() int {
	return 200000 + l.sourceID*1000 + l.targetID
}

func (l *Link) Name() string {
	return fmt.Sprintf("Link %d->%d", l.sourceID, l.targetID)
}

func (l *Link) TraceID() int {
	return l.ID()
}

// ResumeProcess starts a new process event after a pause.
func (l *Link) ResumeProcess() {
	l.currentProcessStart = l.monitor.ResumeProcess()
}

// RecordWait records a wait event.
func (l *Link) RecordWait(start, end float64, cycle int) {
	l.monitor.OnWait(start, end, cycle)
}

// ===== Protocol Config 访问方法 (Phase 1) =====

// SetConfigRef 设置 Protocol 配置引用 (只读)
func (l *Link) SetConfigRef(config *protocol.Edge) {
	l.configRef = config
}

// GetConfigRef 获取 Protocol 配置引用 (只读)
func (l *Link) GetConfigRef() *protocol.Edge {
	return l.configRef
}

// SourcePortID returns the source port ID.
func (l *Link) SourcePortID() int {
	return l.sourcePortID
}

// TargetPortID returns the target port ID.
func (l *Link) TargetPortID() int {
	return l.targetPortID
}

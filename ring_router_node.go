package main

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/Readm/flow_sim/capabilities"
	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/hooks"
	"github.com/Readm/flow_sim/queue"
)

// RingRouterNode represents a lightweight routing node on the ring.
// It forwards packets based on Hook decisions without embedding routing logic.
type RingRouterNode struct {
	Node

	mu        sync.Mutex
	pipeline  *PacketPipeline
	broker    *hooks.PluginBroker
	bindings  *NodeCycleBindings
	txnMgr    *TransactionManager
	packetIDs *PacketIDAllocator

	capabilities          []capabilities.NodeCapability
	capabilityRegistry    map[string]struct{}
	defaultCapsRegistered bool
}

// NewRingRouterNode constructs a router node with default queue capacities.
func NewRingRouterNode(id int) *RingRouterNode {
	rr := &RingRouterNode{
		Node: Node{
			ID:   id,
			Type: core.NodeTypeRT,
		},
		capabilityRegistry: make(map[string]struct{}),
	}

	rr.AddQueue(string(pipelineStageIn), 0, queue.UnlimitedCapacity)
	rr.AddQueue(string(pipelineStageProcess), 0, queue.UnlimitedCapacity)
	rr.AddQueue(string(pipelineStageOut), 0, queue.UnlimitedCapacity)

	rr.pipeline = newPacketPipeline(rr.makeStageMutator, PipelineCapacities{
		In:      queue.UnlimitedCapacity,
		Process: queue.UnlimitedCapacity,
		Out:     queue.UnlimitedCapacity,
	}, PipelineHooks{})

	return rr
}

func (rr *RingRouterNode) makeStageMutator(stage pipelineStageName) queue.MutateFunc {
	return func(length int, capacity int) {
		rr.UpdateQueueState(string(stage), length, capacity)
	}
}

// SetPluginBroker assigns the hook broker and registers capabilities.
func (rr *RingRouterNode) SetPluginBroker(b *hooks.PluginBroker) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.broker = b
	for _, cap := range rr.capabilities {
		if err := cap.Register(rr.broker); err != nil {
			GetLogger().Warnf("RingRouterNode %d capability registration failed: %v", rr.ID, err)
		}
	}
}

// RegisterCapability attaches a capability to the router.
func (rr *RingRouterNode) RegisterCapability(cap capabilities.NodeCapability) {
	if cap == nil {
		return
	}
	rr.mu.Lock()
	defer rr.mu.Unlock()

	desc := cap.Descriptor()
	if desc.Name == "" {
		desc.Name = fmt.Sprintf("ring-router-cap-%d-%d", rr.ID, len(rr.capabilities))
	}
	if _, exists := rr.capabilityRegistry[desc.Name]; exists {
		return
	}
	if rr.broker != nil {
		if err := cap.Register(rr.broker); err != nil {
			GetLogger().Warnf("RingRouterNode %d capability %s registration failed: %v", rr.ID, desc.Name, err)
			return
		}
	}
	rr.capabilityRegistry[desc.Name] = struct{}{}
	rr.capabilities = append(rr.capabilities, cap)
}

// SetTransactionManager sets transaction manager for instrumentation.
func (rr *RingRouterNode) SetTransactionManager(txnMgr *TransactionManager) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.txnMgr = txnMgr
}

// SetPacketIDAllocator is kept for API parity; routers don't allocate packets currently.
func (rr *RingRouterNode) SetPacketIDAllocator(allocator *PacketIDAllocator) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.packetIDs = allocator
}

func (rr *RingRouterNode) CapabilityNames() []string {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return capabilityNameList(rr.capabilities)
}

// CanReceive checks whether the router can accept additional packets on the incoming edge.
func (rr *RingRouterNode) CanReceive(edgeKey EdgeKey, _ int) bool {
	q := rr.inQueue()
	if q == nil {
		return false
	}
	capacity := q.Capacity()
	if capacity < 0 {
		return true
	}
	ready := q.Len() < capacity
	return ready
}

// OnPackets enqueues incoming messages into the router pipeline.
func (rr *RingRouterNode) OnPackets(messages []*InFlightMessage, cycle int) {
	if len(messages) == 0 {
		return
	}
	rr.mu.Lock()
	defer rr.mu.Unlock()

	inQ := rr.inQueue()
	if inQ == nil {
		return
	}

	for _, msg := range messages {
		if msg == nil || msg.Packet == nil {
			continue
		}
		msg.Packet.ReceivedAt = cycle
		finalTarget := rr.determineFinalTarget(msg.Packet)
		pipelineMsg := &PipelineMessage{
			Packet:        msg.Packet,
			Kind:          "forward",
			TargetID:      msg.Packet.DstID,
			DefaultTarget: finalTarget,
		}
		rr.ensureFinalTargetMetadata(msg.Packet, finalTarget)
		if _, ok := inQ.Enqueue(pipelineMsg, cycle); !ok {
			GetLogger().Warnf("RingRouterNode %d: in_queue full, dropping packet %d", rr.ID, msg.Packet.ID)
		}
	}
}

// GetQueuePackets collects packet snapshots for visualization.
func (rr *RingRouterNode) GetQueuePackets() []core.PacketInfo {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	var packets []core.PacketInfo
	packets = rr.appendStagePackets(packets, rr.inQueue())
	packets = rr.appendStagePackets(packets, rr.processQueue())
	packets = rr.appendStagePackets(packets, rr.outQueue())
	return packets
}

func (rr *RingRouterNode) appendStagePackets(dst []core.PacketInfo, q *queue.StageQueue[*PipelineMessage]) []core.PacketInfo {
	if q == nil {
		return dst
	}
	stageName := q.Name()
	q.ForEach(func(_ queue.EntryID, msg *PipelineMessage, ready bool) {
		if msg == nil || msg.Packet == nil {
			return
		}
		metadata := core.CloneMetadata(msg.Packet.Metadata)
		if metadata == nil {
			metadata = map[string]string{}
		}
		metadata["node_queue_ready"] = fmt.Sprintf("%t", ready)
		metadata["node_queue_stage"] = stageName
		metadata["node_message_kind"] = msg.Kind
		dst = append(dst, core.PacketInfo{
			ID:              msg.Packet.ID,
			RequestID:       msg.Packet.RequestID,
			Type:            msg.Packet.Type,
			SrcID:           msg.Packet.SrcID,
			DstID:           msg.Packet.DstID,
			GeneratedAt:     msg.Packet.GeneratedAt,
			SentAt:          msg.Packet.SentAt,
			ReceivedAt:      msg.Packet.ReceivedAt,
			CompletedAt:     msg.Packet.CompletedAt,
			MasterID:        msg.Packet.MasterID,
			TransactionType: msg.Packet.TransactionType,
			MessageType:     msg.Packet.MessageType,
			ResponseType:    msg.Packet.ResponseType,
			Address:         msg.Packet.Address,
			DataSize:        msg.Packet.DataSize,
			TransactionID:   msg.Packet.TransactionID,
			Metadata:        metadata,
		})
	})
	return dst
}

// ConfigureCycleRuntime binds the router to the cycle coordinator.
func (rr *RingRouterNode) ConfigureCycleRuntime(componentID string, coord *CycleCoordinator) {
	if rr.bindings == nil {
		rr.bindings = NewNodeCycleBindings()
	}
	rr.bindings.SetComponent(componentID)
	rr.bindings.SetCoordinator(coord)
}

// RegisterIncomingSignal registers the receive-finished signal for an incoming edge.
func (rr *RingRouterNode) RegisterIncomingSignal(edge EdgeKey, signal *CycleSignal) {
	if rr.bindings == nil {
		rr.bindings = NewNodeCycleBindings()
	}
	rr.bindings.RegisterIncoming(edge, signal)
}

// RegisterOutgoingSignal registers the send-finished signal for an outgoing edge.
func (rr *RingRouterNode) RegisterOutgoingSignal(edge EdgeKey, signal *CycleSignal) {
	if rr.bindings == nil {
		rr.bindings = NewNodeCycleBindings()
	}
	rr.bindings.RegisterOutgoing(edge, signal)
}

// RingRouterRuntime carries runtime dependencies for router execution.
type RingRouterRuntime struct {
	Link            *Link
	ResolveLatency  func(fromID, toID int) int
	TransactionMgr  *TransactionManager
	PacketAllocator *PacketIDAllocator
}

// RunRuntime executes cycle-driven logic for the router.
func (rr *RingRouterNode) RunRuntime(ctx *RingRouterRuntime) {
	if rr.bindings == nil {
		return
	}
	coord := rr.bindings.Coordinator()
	if coord == nil {
		return
	}
	componentID := rr.bindings.ComponentID()
	for {
		cycle := coord.WaitForCycle(componentID)
		if cycle < 0 {
			return
		}
		rr.bindings.WaitIncoming(cycle)
		rr.bindings.SignalReceive(cycle)
		rr.Tick(cycle, ctx)
		rr.bindings.SignalSend(cycle)
		coord.MarkDone(componentID, cycle)
	}
}

// Tick advances router state for a single cycle.
func (rr *RingRouterNode) Tick(cycle int, ctx *RingRouterRuntime) {
	rr.mu.Lock()
	defer rr.mu.Unlock()

	rr.releaseToProcess(cycle)
	rr.processStage(cycle, ctx)
	rr.sendFromOutQueue(cycle, ctx)
}

func (rr *RingRouterNode) releaseToProcess(cycle int) {
	inQ := rr.inQueue()
	procQ := rr.processQueue()
	if inQ == nil || procQ == nil {
		return
	}
	for {
		entryID, msg, ok := inQ.PeekNext()
		if !ok {
			break
		}
		if msg == nil || msg.Packet == nil {
			inQ.Complete(entryID, cycle)
			continue
		}
		if _, ok := procQ.Enqueue(msg, cycle); !ok {
			inQ.ResetPending(entryID)
			break
		}
		inQ.Complete(entryID, cycle)
	}
}

func (rr *RingRouterNode) processStage(cycle int, ctx *RingRouterRuntime) {
	procQ := rr.processQueue()
	outQ := rr.outQueue()
	if procQ == nil || outQ == nil {
		return
	}
	for {
		entryID, msg, ok := procQ.PeekNext()
		if !ok {
			break
		}
		if msg == nil || msg.Packet == nil {
			procQ.Complete(entryID, cycle)
			continue
		}

		packet := msg.Packet
		defaultTarget := msg.DefaultTarget
		if defaultTarget < 0 {
			defaultTarget = rr.determineFinalTarget(packet)
		}

		rr.emitProcessHook(packet, cycle, true)

		targetID, ok := rr.resolveRoute(packet, defaultTarget)
		if !ok {
			procQ.ResetPending(entryID)
			rr.emitProcessHook(packet, cycle, false)
			continue
		}

		msg.TargetID = targetID
		msg.Kind = "forward_prepared"
		if ctx != nil && ctx.ResolveLatency != nil {
			msg.Latency = ctx.ResolveLatency(rr.ID, targetID)
		}
		if msg.Latency <= 0 {
			msg.Latency = 1
		}

		procQ.Complete(entryID, cycle)
		if _, ok := outQ.Enqueue(msg, cycle); !ok {
			GetLogger().Warnf("RingRouterNode %d: out_queue full, dropping packet %d", rr.ID, packet.ID)
		}
		rr.emitProcessHook(packet, cycle, false)
	}
}

func (rr *RingRouterNode) sendFromOutQueue(cycle int, ctx *RingRouterRuntime) {
	outQ := rr.outQueue()
	if outQ == nil || ctx == nil || ctx.Link == nil {
		return
	}
	for {
		entryID, msg, ok := outQ.PeekNext()
		if !ok {
			break
		}
		if msg == nil || msg.Packet == nil {
			outQ.Complete(entryID, cycle)
			continue
		}

		packet := msg.Packet
		targetID := msg.TargetID
		if targetID < 0 {
			targetID = msg.DefaultTarget
		}
		if targetID < 0 {
			targetID = rr.determineFinalTarget(packet)
		}
		if targetID < 0 {
			GetLogger().Warnf("RingRouterNode %d: unable to determine target for packet %d", rr.ID, packet.ID)
			outQ.Complete(entryID, cycle)
			continue
		}

		latency := msg.Latency
		if latency <= 0 && ctx.ResolveLatency != nil {
			latency = ctx.ResolveLatency(rr.ID, targetID)
		}
		if latency <= 0 {
			latency = 1
		}

		finalTarget := msg.DefaultTarget
		if finalTarget < 0 {
			finalTarget = rr.determineFinalTarget(packet)
		}
		rr.ensureFinalTargetMetadata(packet, finalTarget)

		if !rr.emitBeforeSend(packet, targetID, cycle) {
			outQ.ResetPending(entryID)
			continue
		}

		packet.SrcID = rr.ID
		packet.DstID = targetID
		ctx.Link.Send(packet, rr.ID, targetID, cycle, latency)
		packet.SentAt = cycle
		rr.emitAfterSend(packet, targetID, cycle)
		outQ.Complete(entryID, cycle)
	}
}

func (rr *RingRouterNode) resolveRoute(packet *core.Packet, defaultTarget int) (int, bool) {
	targetID := defaultTarget
	if rr.broker == nil || packet == nil {
		if targetID < 0 {
			return 0, false
		}
		return targetID, true
	}
	beforeCtx := &hooks.RouteContext{
		Packet:        packet,
		SourceNodeID:  rr.ID,
		DefaultTarget: defaultTarget,
		TargetID:      defaultTarget,
	}
	if err := rr.broker.EmitBeforeRoute(beforeCtx); err != nil {
		GetLogger().Warnf("RingRouterNode %d OnBeforeRoute hook failed: %v", rr.ID, err)
		return 0, false
	}
	targetID = beforeCtx.TargetID
	afterCtx := &hooks.RouteContext{
		Packet:        packet,
		SourceNodeID:  rr.ID,
		DefaultTarget: defaultTarget,
		TargetID:      targetID,
	}
	if err := rr.broker.EmitAfterRoute(afterCtx); err != nil {
		GetLogger().Warnf("RingRouterNode %d OnAfterRoute hook failed: %v", rr.ID, err)
	}
	if afterCtx.TargetID >= 0 {
		targetID = afterCtx.TargetID
	}
	if targetID < 0 {
		targetID = defaultTarget
	}
	if targetID < 0 {
		return 0, false
	}
	return targetID, true
}

func (rr *RingRouterNode) emitBeforeSend(packet *core.Packet, targetID int, cycle int) bool {
	if rr.broker == nil || packet == nil {
		return true
	}
	ctx := &hooks.MessageContext{
		Packet:   packet,
		NodeID:   rr.ID,
		TargetID: targetID,
		Cycle:    cycle,
	}
	if err := rr.broker.EmitBeforeSend(ctx); err != nil {
		GetLogger().Warnf("RingRouterNode %d OnBeforeSend hook failed: %v", rr.ID, err)
		return false
	}
	return true
}

func (rr *RingRouterNode) emitAfterSend(packet *core.Packet, targetID int, cycle int) {
	if rr.broker == nil || packet == nil {
		return
	}
	ctx := &hooks.MessageContext{
		Packet:   packet,
		NodeID:   rr.ID,
		TargetID: targetID,
		Cycle:    cycle,
	}
	if err := rr.broker.EmitAfterSend(ctx); err != nil {
		GetLogger().Warnf("RingRouterNode %d OnAfterSend hook failed: %v", rr.ID, err)
	}
}

func (rr *RingRouterNode) emitProcessHook(packet *core.Packet, cycle int, before bool) {
	if rr.broker == nil || packet == nil {
		return
	}
	ctx := &hooks.ProcessContext{
		Packet:      packet,
		Node:        &rr.Node,
		NodeID:      rr.ID,
		Cycle:       cycle,
		Transaction: nil,
	}
	var err error
	if before {
		err = rr.broker.EmitBeforeProcess(ctx)
	} else {
		err = rr.broker.EmitAfterProcess(ctx)
	}
	if err != nil {
		GetLogger().Warnf("RingRouterNode %d process hook failed: %v", rr.ID, err)
	}
}

func (rr *RingRouterNode) inQueue() *queue.StageQueue[*PipelineMessage] {
	if rr.pipeline == nil {
		return nil
	}
	return rr.pipeline.queue(pipelineStageIn)
}

func (rr *RingRouterNode) processQueue() *queue.StageQueue[*PipelineMessage] {
	if rr.pipeline == nil {
		return nil
	}
	return rr.pipeline.queue(pipelineStageProcess)
}

func (rr *RingRouterNode) outQueue() *queue.StageQueue[*PipelineMessage] {
	if rr.pipeline == nil {
		return nil
	}
	return rr.pipeline.queue(pipelineStageOut)
}

func (rr *RingRouterNode) determineFinalTarget(packet *core.Packet) int {
	if packet == nil {
		return 0
	}
	if value, ok := packet.GetMetadata(capabilities.RingFinalTargetMetadataKey); ok {
		if id, err := strconv.Atoi(value); err == nil {
			return id
		}
	}
	if packet.DstID != 0 {
		return packet.DstID
	}
	return 0
}

func (rr *RingRouterNode) ensureFinalTargetMetadata(packet *core.Packet, finalTarget int) {
	if packet == nil || finalTarget < 0 {
		return
	}
	packet.SetMetadata(capabilities.RingFinalTargetMetadataKey, strconv.Itoa(finalTarget))
}

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

// SlaveNode (SN) represents a CHI Slave Node that processes requests and provides data.
// It processes CHI protocol requests in FIFO order and generates appropriate CHI responses.
type SlaveNode struct {
	Node        // embedded Node base class
	ProcessRate int
	pipeline    *PacketPipeline
	txnMgr      *TransactionManager // for recording packet events
	broker      *hooks.PluginBroker
	routerID    int

	// stats
	ProcessedCount int
	MaxQueueLength int
	TotalQueueSum  int64 // cumulative length for avg
	Samples        int64

	mu       sync.Mutex
	bindings *NodeCycleBindings

	capabilities          []capabilities.NodeCapability
	capabilityRegistry    map[string]struct{}
	defaultCapsRegistered bool
}

func NewSlaveNode(id int, rate int) *SlaveNode {
	sn := &SlaveNode{
		Node: Node{
			ID:   id,
			Type: NodeTypeSN,
		},
		ProcessRate: rate,
		txnMgr:      nil, // will be set by simulator
		bindings:    NewNodeCycleBindings(),
	}

	sn.AddQueue(string(pipelineStageIn), 0, queue.UnlimitedCapacity)
	sn.AddQueue(string(pipelineStageProcess), 0, DefaultSlaveQueueCapacity)
	sn.AddQueue(string(pipelineStageOut), 0, queue.UnlimitedCapacity)

	sn.pipeline = newPacketPipeline(sn.makeStageMutator, PipelineCapacities{
		In:      DefaultSlaveQueueCapacity,
		Process: DefaultSlaveQueueCapacity,
		Out:     queue.UnlimitedCapacity,
	}, PipelineHooks{
		Process: queue.StageQueueHooks[*PipelineMessage]{
			OnEnqueue: sn.enqueueHook,
			OnDequeue: sn.dequeueHook,
		},
	})

	return sn
}

func (sn *SlaveNode) makeStageMutator(stage pipelineStageName) queue.MutateFunc {
	return func(length int, capacity int) {
		sn.UpdateQueueState(string(stage), length, capacity)
		if stage == pipelineStageProcess && length > sn.MaxQueueLength {
			sn.MaxQueueLength = length
		}
	}
}

func (sn *SlaveNode) enqueueHook(entryID queue.EntryID, msg *PipelineMessage, cycle int) {
	if sn.txnMgr == nil || msg == nil || msg.Packet == nil || msg.Packet.TransactionID == 0 {
		return
	}
	event := &PacketEvent{
		TransactionID:  msg.Packet.TransactionID,
		PacketID:       msg.Packet.ID,
		ParentPacketID: msg.Packet.ParentPacketID,
		NodeID:         sn.ID,
		EventType:      PacketEnqueued,
		Cycle:          cycle,
		EdgeKey:        nil,
	}
	sn.txnMgr.RecordPacketEvent(event)
}

func (sn *SlaveNode) dequeueHook(entryID queue.EntryID, msg *PipelineMessage, cycle int) {
	if sn.txnMgr == nil || msg == nil || msg.Packet == nil || msg.Packet.TransactionID == 0 {
		return
	}
	event := &PacketEvent{
		TransactionID:  msg.Packet.TransactionID,
		PacketID:       msg.Packet.ID,
		ParentPacketID: msg.Packet.ParentPacketID,
		NodeID:         sn.ID,
		EventType:      PacketDequeued,
		Cycle:          cycle,
		EdgeKey:        nil,
	}
	sn.txnMgr.RecordPacketEvent(event)
}

// SetTransactionManager sets the transaction manager for event recording
func (sn *SlaveNode) SetTransactionManager(txnMgr *TransactionManager) {
	sn.txnMgr = txnMgr
	sn.ensureDefaultCapabilities()
}

// SetPluginBroker assigns the hook broker for process lifecycle callbacks.
func (sn *SlaveNode) SetPluginBroker(b *hooks.PluginBroker) {
	sn.broker = b
	sn.ensureDefaultCapabilities()
}

// SetRouterID configures the router used for forwarding responses when ring is enabled.
func (sn *SlaveNode) SetRouterID(id int) {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	sn.routerID = id
}

func (sn *SlaveNode) CapabilityNames() []string {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	return capabilityNameList(sn.capabilities)
}

func (sn *SlaveNode) ensureDefaultCapabilities() {
	if sn.defaultCapsRegistered {
		return
	}
	if sn.broker == nil {
		return
	}
	before := func(ctx *hooks.ProcessContext) error {
		if sn.txnMgr == nil || ctx == nil || ctx.Packet == nil || ctx.Packet.TransactionID == 0 {
			return nil
		}
		event := &PacketEvent{
			TransactionID:  ctx.Packet.TransactionID,
			PacketID:       ctx.Packet.ID,
			ParentPacketID: ctx.Packet.ParentPacketID,
			NodeID:         ctx.NodeID,
			EventType:      PacketProcessingStart,
			Cycle:          ctx.Cycle,
			EdgeKey:        nil,
		}
		sn.txnMgr.RecordPacketEvent(event)
		return nil
	}
	after := func(ctx *hooks.ProcessContext) error {
		if sn.txnMgr == nil || ctx == nil || ctx.Packet == nil || ctx.Packet.TransactionID == 0 {
			return nil
		}
		event := &PacketEvent{
			TransactionID:  ctx.Packet.TransactionID,
			PacketID:       ctx.Packet.ID,
			ParentPacketID: ctx.Packet.ParentPacketID,
			NodeID:         ctx.NodeID,
			EventType:      PacketProcessingEnd,
			Cycle:          ctx.Cycle,
			EdgeKey:        nil,
		}
		sn.txnMgr.RecordPacketEvent(event)
		return nil
	}
	cap := capabilities.NewHookCapability(
		fmt.Sprintf("slave-processing-%d", sn.ID),
		hooks.PluginCategoryInstrumentation,
		"default slave processing instrumentation",
		hooks.HookBundle{
			BeforeProcess: []hooks.BeforeProcessHook{before},
			AfterProcess:  []hooks.AfterProcessHook{after},
		},
	)
	sn.registerCapability(cap)
	sn.defaultCapsRegistered = true
}

func (sn *SlaveNode) registerCapability(cap capabilities.NodeCapability) {
	if cap == nil || sn.broker == nil {
		return
	}
	desc := cap.Descriptor()
	if desc.Name == "" {
		desc.Name = fmt.Sprintf("slave-capability-%d-%d", sn.ID, len(sn.capabilities))
	}
	if sn.capabilityRegistry == nil {
		sn.capabilityRegistry = make(map[string]struct{})
	}
	if _, exists := sn.capabilityRegistry[desc.Name]; exists {
		return
	}
	if err := cap.Register(sn.broker); err != nil {
		GetLogger().Warnf("SlaveNode %d capability %s registration failed: %v", sn.ID, desc.Name, err)
		return
	}
	sn.capabilityRegistry[desc.Name] = struct{}{}
	sn.capabilities = append(sn.capabilities, cap)
}

// ConfigureCycleRuntime sets the coordinator bindings for the node.
func (sn *SlaveNode) ConfigureCycleRuntime(componentID string, coord *CycleCoordinator) {
	if sn.bindings == nil {
		sn.bindings = NewNodeCycleBindings()
	}
	sn.bindings.SetComponent(componentID)
	sn.bindings.SetCoordinator(coord)
}

// RegisterIncomingSignal registers the receive-finished signal for an incoming edge.
func (sn *SlaveNode) RegisterIncomingSignal(edge EdgeKey, signal *CycleSignal) {
	if sn.bindings == nil {
		sn.bindings = NewNodeCycleBindings()
	}
	sn.bindings.RegisterIncoming(edge, signal)
}

// RegisterOutgoingSignal registers the send-finished signal for an outgoing edge.
func (sn *SlaveNode) RegisterOutgoingSignal(edge EdgeKey, signal *CycleSignal) {
	if sn.bindings == nil {
		sn.bindings = NewNodeCycleBindings()
	}
	sn.bindings.RegisterOutgoing(edge, signal)
}

// CanReceive checks if the SlaveNode can receive packets from the given edge.
// Checks if the in_queue has capacity.
func (sn *SlaveNode) CanReceive(edgeKey EdgeKey, packetCount int) bool {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	inQ := sn.inQueue()
	if inQ == nil {
		return true
	}
	capacity := inQ.Capacity()
	if capacity < 0 {
		return true
	}
	return inQ.Len()+packetCount <= capacity
}

// OnPackets receives packets from the channel and enqueues them.
func (sn *SlaveNode) OnPackets(messages []*InFlightMessage, cycle int) {
	for _, msg := range messages {
		sn.processIncomingMessage(msg, cycle)
	}
}

func (sn *SlaveNode) EnqueueRequest(p *Packet) {
	if p == nil {
		return
	}
	sn.mu.Lock()
	defer sn.mu.Unlock()
	if inQ := sn.inQueue(); inQ != nil {
		msg := &PipelineMessage{Packet: p, Kind: "incoming"}
		if _, ok := inQ.Enqueue(msg, p.ReceivedAt); !ok {
			GetLogger().Warnf("SlaveNode %d: in_queue full, dropping packet %d", sn.ID, p.ID)
		}
	}
}

// Tick processes up to ProcessRate requests from the pipeline and returns generated responses.
func (sn *SlaveNode) Tick(cycle int, packetIDs *PacketIDAllocator) []*Packet {
	sn.mu.Lock()
	defer sn.mu.Unlock()

	queueLen := 0
	if inQ := sn.inQueue(); inQ != nil {
		queueLen += inQ.Len()
	}
	if procQ := sn.processQueue(); procQ != nil {
		queueLen += procQ.Len()
	}
	sn.TotalQueueSum += int64(queueLen)
	sn.Samples++
	if queueLen > sn.MaxQueueLength {
		sn.MaxQueueLength = queueLen
	}

	sn.releaseToProcess(sn.ProcessRate, cycle)
	sn.processStage(cycle, packetIDs)

	responses := sn.flushOutQueue(cycle)
	return responses
}

func (sn *SlaveNode) releaseToProcess(limit int, cycle int) {
	inQ := sn.inQueue()
	procQ := sn.processQueue()
	if inQ == nil || procQ == nil {
		return
	}
	if limit == 0 {
		return
	}
	moved := 0
	for limit < 0 || moved < limit {
		entryID, msg, ok := inQ.PeekNext()
		if !ok {
			break
		}
		if msg == nil || msg.Packet == nil {
			inQ.Complete(entryID, cycle)
			moved++
			continue
		}
		if _, ok := procQ.Enqueue(msg, cycle); !ok {
			inQ.ResetPending(entryID)
			break
		}
		inQ.Complete(entryID, cycle)
		moved++
	}
}

func (sn *SlaveNode) processStage(cycle int, packetIDs *PacketIDAllocator) {
	procQ := sn.processQueue()
	if procQ == nil || sn.ProcessRate <= 0 {
		return
	}
	processed := 0
	outQ := sn.outQueue()
	for processed < sn.ProcessRate {
		entryID, msg, ok := procQ.PeekNext()
		if !ok {
			break
		}
		if msg == nil || msg.Packet == nil {
			procQ.Complete(entryID, cycle)
			continue
		}
		pkt := msg.Packet
		req := (*Packet)(pkt)

		var txn *Transaction
		if sn.txnMgr != nil && pkt.TransactionID > 0 {
			txn = sn.txnMgr.GetTransaction(pkt.TransactionID)
		}
		if sn.broker != nil {
			ctx := &hooks.ProcessContext{
				Packet:      req,
				Transaction: txn,
				Node:        &sn.Node,
				NodeID:      sn.ID,
				Cycle:       cycle,
			}
			if err := sn.broker.EmitBeforeProcess(ctx); err != nil {
				GetLogger().Warnf("SlaveNode %d OnBeforeProcess hook failed: %v", sn.ID, err)
			}
		}

		procQ.Complete(entryID, cycle)

		req.CompletedAt = cycle
		sn.ProcessedCount++

		resp := sn.generateCHIResponse(req, cycle, packetIDs)
		if sn.txnMgr != nil && resp != nil && resp.TransactionID > 0 {
			event := &PacketEvent{
				TransactionID:  resp.TransactionID,
				PacketID:       resp.ID,
				ParentPacketID: req.ID,
				NodeID:         sn.ID,
				EventType:      PacketGenerated,
				Cycle:          cycle,
				EdgeKey:        nil,
			}
			sn.txnMgr.RecordPacketEvent(event)
		}

		if sn.broker != nil {
			ctx := &hooks.ProcessContext{
				Packet:      req,
				Transaction: txn,
				Node:        &sn.Node,
				NodeID:      sn.ID,
				Cycle:       cycle,
			}
			if err := sn.broker.EmitAfterProcess(ctx); err != nil {
				GetLogger().Warnf("SlaveNode %d OnAfterProcess hook failed: %v", sn.ID, err)
			}
		}

		if resp != nil && outQ != nil {
			if _, ok := outQ.Enqueue(&PipelineMessage{Packet: resp, Kind: "response_ready"}, cycle); !ok {
				GetLogger().Warnf("SlaveNode %d: out_queue full, dropping response %d", sn.ID, resp.ID)
			}
		}

		processed++
	}
}

func (sn *SlaveNode) flushOutQueue(cycle int) []*Packet {
	outQ := sn.outQueue()
	if outQ == nil {
		return nil
	}
	responses := make([]*Packet, 0, outQ.Len())
	for {
		entryID, msg, ok := outQ.PeekNext()
		if !ok {
			break
		}
		if msg == nil || msg.Packet == nil {
			outQ.Complete(entryID, cycle)
			continue
		}
		responses = append(responses, (*Packet)(msg.Packet))
		outQ.Complete(entryID, cycle)
	}
	return responses
}

// SlaveNodeRuntime contains dependencies used during runtime execution.
type SlaveNodeRuntime struct {
	PacketAllocator *PacketIDAllocator
	Link            *Link
	RelayID         int
	RelayLatency    int
	RouterID        int
	RouterLatency   int
}

// RunRuntime executes the slave node logic driven by the coordinator.
func (sn *SlaveNode) RunRuntime(ctx *SlaveNodeRuntime) {
	if sn.bindings == nil {
		return
	}

	coord := sn.bindings.Coordinator()
	if coord == nil {
		return
	}
	componentID := sn.bindings.ComponentID()
	for {
		cycle := coord.WaitForCycle(componentID)
		if cycle < 0 {
			return
		}
		sn.bindings.WaitIncoming(cycle)
		sn.bindings.SignalReceive(cycle)
		responses := sn.Tick(cycle, ctx.PacketAllocator)
		for _, resp := range responses {
			if resp == nil {
				continue
			}
			targetID := ctx.RelayID
			latency := ctx.RelayLatency
			if ctx.RouterID > 0 {
				targetID = ctx.RouterID
				if ctx.RouterLatency > 0 {
					latency = ctx.RouterLatency
				}
			}
			if latency <= 0 {
				latency = 1
			}
			if ctx.Link != nil && targetID >= 0 {
				ctx.Link.Send(resp, sn.ID, targetID, cycle, latency)
			}
		}
		sn.bindings.SignalSend(cycle)
		coord.MarkDone(componentID, cycle)
	}
}

// generateCHIResponse creates a CHI protocol response packet based on the request.
// For ReadNoSnp transactions, returns CompData response.
func (sn *SlaveNode) generateCHIResponse(req *Packet, cycle int, packetIDs *PacketIDAllocator) *Packet {
	resp := &Packet{
		ID:             packetIDs.Allocate(),
		Type:           "response", // legacy field
		SrcID:          sn.ID,
		DstID:          req.MasterID, // return to Request Node via Home Node
		SentAt:         cycle,
		RequestID:      req.RequestID,
		MasterID:       req.MasterID,
		Address:        req.Address,       // preserve address from request
		DataSize:       req.DataSize,      // preserve data size from request
		TransactionID:  req.TransactionID, // preserve transaction ID from request
		ParentPacketID: req.ID,            // parent is the request packet
	}

	// Set CHI protocol fields based on transaction type
	if req.TransactionType == CHITxnReadNoSnp {
		resp.TransactionType = CHITxnReadNoSnp
		resp.MessageType = CHIMsgComp
		resp.ResponseType = CHIRespCompData // Completion with Data for read
	} else if req.MessageType == CHIMsgReq {
		// Generic CHI request, generate CompData response
		resp.TransactionType = req.TransactionType
		resp.MessageType = CHIMsgComp
		resp.ResponseType = CHIRespCompData
	} else {
		// Legacy support
		resp.MessageType = CHIMsgResp
	}

	resp.SetMetadata(capabilities.RingFinalTargetMetadataKey, strconv.Itoa(resp.MasterID))

	return resp
}

func (sn *SlaveNode) QueueLength() int {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	total := 0
	if inQ := sn.inQueue(); inQ != nil {
		total += inQ.Len()
	}
	if procQ := sn.processQueue(); procQ != nil {
		total += procQ.Len()
	}
	return total
}

// GetQueuePackets returns packet information across in/process/out queues
func (sn *SlaveNode) GetQueuePackets() []PacketInfo {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	packets := make([]PacketInfo, 0)
	packets = sn.appendStagePackets(packets, sn.inQueue())
	packets = sn.appendStagePackets(packets, sn.processQueue())
	packets = sn.appendStagePackets(packets, sn.outQueue())
	return packets
}

func (sn *SlaveNode) appendStagePackets(dst []PacketInfo, q *queue.StageQueue[*PipelineMessage]) []PacketInfo {
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
		dst = append(dst, PacketInfo{
			ID:              msg.Packet.ID,
			Type:            msg.Packet.Type,
			SrcID:           msg.Packet.SrcID,
			DstID:           msg.Packet.DstID,
			GeneratedAt:     msg.Packet.GeneratedAt,
			SentAt:          msg.Packet.SentAt,
			ReceivedAt:      msg.Packet.ReceivedAt,
			CompletedAt:     msg.Packet.CompletedAt,
			MasterID:        msg.Packet.MasterID,
			RequestID:       msg.Packet.RequestID,
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

type SlaveNodeStats struct {
	TotalProcessed int
	MaxQueueLength int
	AvgQueueLength float64
}

func (sn *SlaveNode) SnapshotStats() *SlaveNodeStats {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	var avg float64
	if sn.Samples > 0 {
		avg = float64(sn.TotalQueueSum) / float64(sn.Samples)
	}
	return &SlaveNodeStats{
		TotalProcessed: sn.ProcessedCount,
		MaxQueueLength: sn.MaxQueueLength,
		AvgQueueLength: avg,
	}
}

// Legacy type aliases for backward compatibility during transition
// These are kept temporarily for compatibility but should be migrated to SlaveNode
type Slave = SlaveNode
type SlaveStats = SlaveNodeStats

// NewSlave creates a new SlaveNode (legacy compatibility function)
// Deprecated: Use NewSlaveNode instead
func NewSlave(id int, rate int) *Slave {
	return NewSlaveNode(id, rate)
}

func (sn *SlaveNode) processIncomingMessage(msg *InFlightMessage, cycle int) {
	if msg == nil || msg.Packet == nil {
		return
	}
	packet := msg.Packet
	packet.ReceivedAt = cycle

	sn.mu.Lock()
	defer sn.mu.Unlock()

	// Record PacketReceived event
	if sn.txnMgr != nil && packet.TransactionID > 0 {
		event := &PacketEvent{
			TransactionID:  packet.TransactionID,
			PacketID:       packet.ID,
			ParentPacketID: packet.ParentPacketID,
			NodeID:         sn.ID,
			EventType:      PacketReceived,
			Cycle:          cycle,
			EdgeKey:        nil, // in-node event
		}
		sn.txnMgr.RecordPacketEvent(event)
	}

	if inQ := sn.inQueue(); inQ != nil {
		msg := &PipelineMessage{Packet: packet, Kind: "incoming"}
		if _, ok := inQ.Enqueue(msg, cycle); !ok {
			GetLogger().Warnf("SlaveNode %d: in_queue full, dropping packet %d", sn.ID, packet.ID)
		}
	}
}

func (sn *SlaveNode) stageQueue(stage pipelineStageName) *queue.StageQueue[*PipelineMessage] {
	if sn == nil || sn.pipeline == nil {
		return nil
	}
	return sn.pipeline.queue(stage)
}

func (sn *SlaveNode) inQueue() *queue.StageQueue[*PipelineMessage] {
	return sn.stageQueue(pipelineStageIn)
}

func (sn *SlaveNode) processQueue() *queue.StageQueue[*PipelineMessage] {
	return sn.stageQueue(pipelineStageProcess)
}

func (sn *SlaveNode) outQueue() *queue.StageQueue[*PipelineMessage] {
	return sn.stageQueue(pipelineStageOut)
}

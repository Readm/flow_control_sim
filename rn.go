package main

import (
	"math/rand"
	"sync"

	"flow_sim/core"
	"flow_sim/hooks"
	"flow_sim/policy"
	"flow_sim/queue"
)

// RequestNode (RN) represents a CHI Request Node that initiates transactions.
// It generates CHI protocol requests and collects responses.
type RequestNode struct {
	Node        // embedded Node base class
	generator   RequestGenerator
	masterIndex int // index of this master in the masters array (for generator)
	txFactory   *TxFactory
	broker      *hooks.PluginBroker
	policyMgr   policy.Manager

	// queues for request management
	stimulusQueue *queue.TrackedQueue[*core.Packet] // stimulus_queue: infinite capacity, stores generated requests
	dispatchQueue *queue.TrackedQueue[*core.Packet] // dispatch_queue: limited capacity, stores requests ready to send

	// track request generation times by request id (for statistics)
	generatedAtByReq map[int64]int

	// stats
	TotalRequests  int
	CompletedCount int
	TotalDelay     int64
	MaxDelay       int
	MinDelay       int

	// address generator for CHI transactions
	nextAddress uint64

	// Transaction manager reference (set by Simulator)
	txnMgr *TransactionManager

	mu       sync.Mutex
	bindings *NodeCycleBindings
}

func NewRequestNode(id int, masterIndex int, generator RequestGenerator) *RequestNode {
	rn := &RequestNode{
		Node: Node{
			ID:   id,
			Type: core.NodeTypeRN,
		},
		generator:        generator,
		masterIndex:      masterIndex,
		generatedAtByReq: make(map[int64]int),
		MinDelay:         int(^uint(0) >> 1), // max int
		nextAddress:      DefaultAddressBase,
	}
	rn.AddQueue("stimulus_queue", 0, UnlimitedQueueCapacity)
	rn.AddQueue("dispatch_queue", 0, DefaultDispatchQueueCapacity)
	rn.bindings = NewNodeCycleBindings()
	rn.stimulusQueue = queue.NewTrackedQueue("stimulus_queue", queue.UnlimitedCapacity, rn.makeQueueMutator("stimulus_queue"), queue.QueueHooks[*core.Packet]{})
	rn.dispatchQueue = queue.NewTrackedQueue("dispatch_queue", DefaultDispatchQueueCapacity, rn.makeQueueMutator("dispatch_queue"), queue.QueueHooks[*core.Packet]{
		OnEnqueue: rn.dispatchEnqueueHook,
		OnDequeue: rn.dispatchDequeueHook,
	})
	return rn
}

func (rn *RequestNode) makeQueueMutator(queueName string) queue.MutateFunc {
	return func(length int, capacity int) {
		rn.UpdateQueueState(queueName, length, capacity)
	}
}

func (rn *RequestNode) dispatchEnqueueHook(p *core.Packet, cycle int) {
	if rn.txnMgr == nil || p == nil || p.TransactionID == 0 {
		return
	}
	event := &core.PacketEvent{
		TransactionID:  p.TransactionID,
		PacketID:       p.ID,
		ParentPacketID: p.ParentPacketID,
		NodeID:         rn.ID,
		EventType:      core.PacketEnqueued,
		Cycle:          cycle,
		EdgeKey:        nil,
	}
	rn.txnMgr.RecordPacketEvent(event)
}

func (rn *RequestNode) dispatchDequeueHook(p *core.Packet, cycle int) {
	if rn.txnMgr == nil || p == nil || p.TransactionID == 0 {
		return
	}
	event := &core.PacketEvent{
		TransactionID:  p.TransactionID,
		PacketID:       p.ID,
		ParentPacketID: p.ParentPacketID,
		NodeID:         rn.ID,
		EventType:      core.PacketDequeued,
		Cycle:          cycle,
		EdgeKey:        nil,
	}
	rn.txnMgr.RecordPacketEvent(event)
}

// SetTxFactory assigns the transaction factory for request creation.
func (rn *RequestNode) SetTxFactory(factory *TxFactory) {
	rn.txFactory = factory
}

// SetPluginBroker assigns the hook broker for lifecycle callbacks.
func (rn *RequestNode) SetPluginBroker(b *hooks.PluginBroker) {
	rn.broker = b
}

// SetPolicyManager assigns the policy manager for routing decisions.
func (rn *RequestNode) SetPolicyManager(m policy.Manager) {
	rn.policyMgr = m
}

// GenerateReadNoSnpRequest creates a CHI ReadNoSnp transaction request packet.
// ReadNoSnp is a simple read request that does not require snoop operations.
func (rn *RequestNode) GenerateReadNoSnpRequest(reqID int64, cycle int, dstSNID int, homeNodeID int) *core.Packet {
	address := rn.nextAddress
	rn.nextAddress += DefaultCacheLineSize

	return &core.Packet{
		ID:              reqID,
		Type:            "request", // legacy field for compatibility
		SrcID:           rn.ID,
		DstID:           dstSNID, // final destination is Slave Node
		GeneratedAt:     cycle,
		SentAt:          cycle,
		MasterID:        rn.ID, // legacy field
		RequestID:       reqID,
		TransactionType: core.CHITxnReadNoSnp,
		MessageType:     core.CHIMsgReq,
		Address:         address,
		DataSize:        DefaultCacheLineSize,
	}
}

// Tick may generate request(s) per cycle based on the configured RequestGenerator.
// Supports generating multiple requests in the same cycle.
// Implements three-phase logic: generate -> send -> release
func (rn *RequestNode) Tick(cycle int, cfg *Config, homeNodeID int, ch *Link, packetIDs *PacketIDAllocator, slaves []*SlaveNode, txnMgr *TransactionManager) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	if homeNodeID < 0 {
		return
	}

	// Initialize dispatch queue capacity from config (first time or if config changed)
	capacity := cfg.DispatchQueueCapacity
	if capacity <= 0 || capacity == -1 {
		capacity = DefaultDispatchQueueCapacity
	}
	rn.dispatchQueue.SetCapacity(capacity)

	// Phase 1: Generate requests and add to stimulus_queue
	if rn.generator != nil {
		results := rn.generator.ShouldGenerate(cycle, rn.masterIndex, len(slaves))
		for _, result := range results {
			if !result.ShouldGenerate {
				continue
			}
			if result.SlaveIndex < 0 || result.SlaveIndex >= len(slaves) {
				continue
			}

			// Get actual slave node ID from the slaves array
			dstSNID := slaves[result.SlaveIndex].ID

			// Determine address and data size
			address := result.Address
			if address == 0 {
				address = rn.nextAddress
				rn.nextAddress += DefaultCacheLineSize
			}

			dataSize := result.DataSize
			if dataSize == 0 {
				dataSize = DefaultCacheLineSize
			}

			// Determine transaction type
			txnType := result.TransactionType
			if txnType == "" {
				txnType = core.CHITxnReadNoSnp
			}

			var packet *core.Packet
			if rn.txFactory != nil {
				params := TxRequestParams{
					Cycle:           cycle,
					SrcID:           rn.ID,
					MasterID:        rn.ID,
					DstID:           dstSNID,
					TransactionType: txnType,
					Address:         address,
					DataSize:        dataSize,
				}
				packet, _ = rn.txFactory.CreateRequest(params)
			} else {
				reqID := packetIDs.Allocate()

				// Create transaction if TransactionManager is available
				var txnID int64
				if txnMgr != nil {
					txn := txnMgr.CreateTransaction(txnType, address, cycle)
					txnID = txn.Context.TransactionID
				}

				// Generate request packet
				// SentAt is initially 0, will be set when actually sent to Link
				packet = &core.Packet{
					ID:              reqID,
					Type:            "request", // legacy field
					SrcID:           rn.ID,
					DstID:           dstSNID,
					GeneratedAt:     cycle,
					SentAt:          0, // Will be set when sent to Link
					MasterID:        rn.ID,
					RequestID:       reqID,
					TransactionType: txnType,
					MessageType:     core.CHIMsgReq,
					Address:         address,
					DataSize:        dataSize,
					TransactionID:   txnID,
					ParentPacketID:  0, // Initial request has no parent packet
				}
			}
			if packet == nil {
				continue
			}

			rn.TotalRequests++
			rn.generatedAtByReq[packet.RequestID] = cycle

			// Mark transaction as in-flight when packet is sent
			if txnMgr != nil && packet.TransactionID > 0 {
				// Will be marked in-flight when actually sent
			}

			// Add to stimulus_queue
			rn.stimulusQueue.Enqueue(packet, cycle)
		}
	}

	// Phase 2: Send requests from dispatch_queue to Link (max BandwidthLimit per cycle)
	// Only send packets that haven't been sent yet (SentAt == 0)
	bandwidth := cfg.BandwidthLimit
	if bandwidth <= 0 {
		bandwidth = 1
	}
	sentThisCycle := 0
	dispatchItems := rn.dispatchQueue.Items()
	for i := 0; i < len(dispatchItems) && sentThisCycle < bandwidth; i++ {
		p := dispatchItems[i]
		if p.SentAt > 0 {
			continue
		}

		targetID := homeNodeID
		if rn.broker != nil {
			routeCtx := &hooks.RouteContext{
				Packet:        p,
				SourceNodeID:  rn.ID,
				DefaultTarget: homeNodeID,
				TargetID:      homeNodeID,
			}
			if err := rn.broker.EmitBeforeRoute(routeCtx); err != nil {
				GetLogger().Warnf("RequestNode %d OnBeforeRoute hook failed: %v", rn.ID, err)
				continue
			}
			targetID = routeCtx.TargetID
		}

		if rn.policyMgr != nil {
			resolvedTarget, err := rn.policyMgr.ResolveRoute(p, rn.ID, targetID)
			if err != nil {
				GetLogger().Warnf("RequestNode %d policy route failed: %v", rn.ID, err)
				continue
			}
			targetID = resolvedTarget
		}

		if rn.broker != nil {
			routeCtx := &hooks.RouteContext{
				Packet:        p,
				SourceNodeID:  rn.ID,
				DefaultTarget: homeNodeID,
				TargetID:      targetID,
			}
			if err := rn.broker.EmitAfterRoute(routeCtx); err != nil {
				GetLogger().Warnf("RequestNode %d OnAfterRoute hook failed: %v", rn.ID, err)
			}
			targetID = routeCtx.TargetID
		}

		if rn.broker != nil {
			ctx := &hooks.MessageContext{
				Packet: p,
				NodeID: rn.ID,
				Cycle:  cycle,
			}
			if err := rn.broker.EmitBeforeSend(ctx); err != nil {
				GetLogger().Warnf("RequestNode %d OnBeforeSend hook failed: %v", rn.ID, err)
				continue
			}
		}

		if rn.policyMgr != nil {
			if err := rn.policyMgr.CheckFlowControl(p, rn.ID, targetID); err != nil {
				GetLogger().Warnf("RequestNode %d flow control blocked packet: %v", rn.ID, err)
				continue
			}
		}

		if targetID <= 0 {
			targetID = homeNodeID
		}

		ch.Send(p, rn.ID, targetID, cycle, cfg.MasterRelayLatency)
		p.SentAt = cycle
		// Mark transaction as in-flight when packet is sent
		if txnMgr != nil && p.TransactionID > 0 {
			txnMgr.MarkTransactionInFlight(p.TransactionID, cycle)
		}
		if rn.broker != nil {
			ctx := &hooks.MessageContext{
				Packet: p,
				NodeID: rn.ID,
				Cycle:  cycle,
			}
			if err := rn.broker.EmitAfterSend(ctx); err != nil {
				GetLogger().Warnf("RequestNode %d OnAfterSend hook failed: %v", rn.ID, err)
			}
		}
		sentThisCycle++
		// Packet remains in dispatchQueue, will be removed when Comp response arrives
	}

	// Phase 3: Release requests from stimulus_queue to dispatch_queue (max BandwidthLimit per cycle)
	// Check dispatch_queue capacity before releasing
	dispatchLen := rn.dispatchQueue.Len()
	dispatchCap := rn.dispatchQueue.Capacity()
	availableCapacity := rn.stimulusQueue.Len()
	if dispatchCap >= 0 {
		availableCapacity = dispatchCap - dispatchLen
		if availableCapacity < 0 {
			availableCapacity = 0
		}
	}
	if availableCapacity > 0 {
		releaseCount := bandwidth
		if releaseCount > rn.stimulusQueue.Len() {
			releaseCount = rn.stimulusQueue.Len()
		}
		if releaseCount > availableCapacity {
			releaseCount = availableCapacity
		}

		// Move packets from stimulus_queue to dispatch_queue
		for i := 0; i < releaseCount; i++ {
			p, ok := rn.stimulusQueue.PopFront(cycle)
			if !ok {
				break
			}
			if !rn.dispatchQueue.Enqueue(p, cycle) {
				break
			}
		}
	}
}

// CanReceive checks if the RequestNode can receive packets from the given edge.
// RequestNode always can receive (unlimited capacity for receiving Comp responses).
func (rn *RequestNode) CanReceive(edgeKey EdgeKey, packetCount int) bool {
	// RequestNode can always receive Comp responses
	return true
}

// OnPackets receives packets from the channel and processes them as responses.
func (rn *RequestNode) OnPackets(messages []*InFlightMessage, cycle int) {
	for _, msg := range messages {
		rn.processIncomingMessage(msg, cycle)
	}
}

// SetTransactionManager sets the transaction manager for this node
func (rn *RequestNode) SetTransactionManager(txnMgr *TransactionManager) {
	rn.txnMgr = txnMgr
}

// OnResponse processes a CHI response arriving to the request node at given cycle.
// Handles CompData responses for ReadNoSnp transactions.
// Removes the corresponding request packet from dispatch_queue when Comp response arrives.
func (rn *RequestNode) OnResponse(p *core.Packet, cycle int, txnMgr *TransactionManager) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.handleResponseLocked(p, cycle, txnMgr)
}

func (rn *RequestNode) handleResponseLocked(p *core.Packet, cycle int, txnMgr *TransactionManager) {
	if p == nil {
		return
	}
	// Check for CHI response or legacy response type
	isCHIResponse := p.MessageType == core.CHIMsgComp || p.MessageType == core.CHIMsgResp
	isLegacyResponse := p.Type == "response"
	if !isCHIResponse && !isLegacyResponse {
		return
	}

	requestID := p.RequestID
	_, found := rn.dispatchQueue.RemoveMatch(func(reqPkt *core.Packet) bool {
		return reqPkt != nil && reqPkt.RequestID == requestID
	}, cycle)

	if !found {
		// Packet not found in dispatch_queue, might have been already removed or never added
		// This can happen in edge cases, just return
		return
	}

	// Update statistics
	gen, ok := rn.generatedAtByReq[requestID]
	if ok {
		delay := cycle - gen
		rn.CompletedCount++
		rn.TotalDelay += int64(delay)
		if delay > rn.MaxDelay {
			rn.MaxDelay = delay
		}
		if delay < rn.MinDelay {
			rn.MinDelay = delay
		}
		delete(rn.generatedAtByReq, requestID)
	}

	// Mark transaction as completed
	if txnMgr != nil && p.TransactionID > 0 {
		txnMgr.MarkTransactionCompleted(p.TransactionID, cycle)
	}

}

type RequestNodeStats struct {
	TotalRequests     int
	CompletedRequests int
	AvgDelay          float64
	MaxDelay          int
	MinDelay          int
}

func (rn *RequestNode) SnapshotStats() *RequestNodeStats {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	var avg float64
	if rn.CompletedCount > 0 {
		avg = float64(rn.TotalDelay) / float64(rn.CompletedCount)
	}
	min := rn.MinDelay
	if rn.CompletedCount == 0 {
		min = 0
	}
	return &RequestNodeStats{
		TotalRequests:     rn.TotalRequests,
		CompletedRequests: rn.CompletedCount,
		AvgDelay:          avg,
		MaxDelay:          rn.MaxDelay,
		MinDelay:          min,
	}
}

// GetQueuePackets returns packet information for stimulus_queue and dispatch_queue
// Returns packets from both queues for visualization
func (rn *RequestNode) GetQueuePackets() []core.PacketInfo {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	packets := make([]core.PacketInfo, 0, rn.stimulusQueue.Len()+rn.dispatchQueue.Len())

	// Add packets from stimulus_queue
	for _, p := range rn.stimulusQueue.Items() {
		if p == nil {
			continue
		}
		packets = append(packets, core.PacketInfo{
			ID:              p.ID,
			RequestID:       p.RequestID,
			Type:            p.Type,
			SrcID:           p.SrcID,
			DstID:           p.DstID,
			GeneratedAt:     p.GeneratedAt,
			SentAt:          p.SentAt,
			ReceivedAt:      p.ReceivedAt,
			CompletedAt:     p.CompletedAt,
			MasterID:        p.MasterID,
			TransactionType: p.TransactionType,
			MessageType:     p.MessageType,
			ResponseType:    p.ResponseType,
			Address:         p.Address,
			DataSize:        p.DataSize,
			TransactionID:   p.TransactionID,
		})
	}

	// Add packets from dispatch_queue
	for _, p := range rn.dispatchQueue.Items() {
		if p == nil {
			continue
		}
		packets = append(packets, core.PacketInfo{
			ID:              p.ID,
			RequestID:       p.RequestID,
			Type:            p.Type,
			SrcID:           p.SrcID,
			DstID:           p.DstID,
			GeneratedAt:     p.GeneratedAt,
			SentAt:          p.SentAt,
			ReceivedAt:      p.ReceivedAt,
			CompletedAt:     p.CompletedAt,
			MasterID:        p.MasterID,
			TransactionType: p.TransactionType,
			MessageType:     p.MessageType,
			ResponseType:    p.ResponseType,
			Address:         p.Address,
			DataSize:        p.DataSize,
			TransactionID:   p.TransactionID,
		})
	}

	return packets
}

// GetStimulusQueuePackets returns packet information for stimulus_queue only
func (rn *RequestNode) GetStimulusQueuePackets() []core.PacketInfo {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	packets := make([]core.PacketInfo, 0, rn.stimulusQueue.Len())
	for _, p := range rn.stimulusQueue.Items() {
		if p == nil {
			continue
		}
		packets = append(packets, core.PacketInfo{
			ID:              p.ID,
			RequestID:       p.RequestID,
			Type:            p.Type,
			SrcID:           p.SrcID,
			DstID:           p.DstID,
			GeneratedAt:     p.GeneratedAt,
			SentAt:          p.SentAt,
			ReceivedAt:      p.ReceivedAt,
			CompletedAt:     p.CompletedAt,
			MasterID:        p.MasterID,
			TransactionType: p.TransactionType,
			MessageType:     p.MessageType,
			ResponseType:    p.ResponseType,
			Address:         p.Address,
			DataSize:        p.DataSize,
			TransactionID:   p.TransactionID,
		})
	}
	return packets
}

// GetDispatchQueuePackets returns packet information for dispatch_queue only
func (rn *RequestNode) GetDispatchQueuePackets() []core.PacketInfo {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	packets := make([]core.PacketInfo, 0, rn.dispatchQueue.Len())
	for _, p := range rn.dispatchQueue.Items() {
		if p == nil {
			continue
		}
		packets = append(packets, core.PacketInfo{
			ID:              p.ID,
			RequestID:       p.RequestID,
			Type:            p.Type,
			SrcID:           p.SrcID,
			DstID:           p.DstID,
			GeneratedAt:     p.GeneratedAt,
			SentAt:          p.SentAt,
			ReceivedAt:      p.ReceivedAt,
			CompletedAt:     p.CompletedAt,
			MasterID:        p.MasterID,
			TransactionType: p.TransactionType,
			MessageType:     p.MessageType,
			ResponseType:    p.ResponseType,
			Address:         p.Address,
			DataSize:        p.DataSize,
			TransactionID:   p.TransactionID,
		})
	}
	return packets
}

// GetPendingRequests is a legacy method kept for backward compatibility
// Deprecated: Use GetQueuePackets() instead
func (rn *RequestNode) GetPendingRequests() []core.PacketInfo {
	return rn.GetQueuePackets()
}

// ConfigureCycleRuntime sets the coordinator bindings for the node.
func (rn *RequestNode) ConfigureCycleRuntime(componentID string, coord *CycleCoordinator) {
	if rn.bindings == nil {
		rn.bindings = NewNodeCycleBindings()
	}
	rn.bindings.SetComponent(componentID)
	rn.bindings.SetCoordinator(coord)
}

// RegisterIncomingSignal registers the receive-finished signal for an incoming edge.
func (rn *RequestNode) RegisterIncomingSignal(edge EdgeKey, signal *CycleSignal) {
	if rn.bindings == nil {
		rn.bindings = NewNodeCycleBindings()
	}
	rn.bindings.RegisterIncoming(edge, signal)
}

// RegisterOutgoingSignal registers the send-finished signal for an outgoing edge.
func (rn *RequestNode) RegisterOutgoingSignal(edge EdgeKey, signal *CycleSignal) {
	if rn.bindings == nil {
		rn.bindings = NewNodeCycleBindings()
	}
	rn.bindings.RegisterOutgoing(edge, signal)
}

// RequestNodeRuntime provides dependencies required by the runtime loop.
type RequestNodeRuntime struct {
	Config             *Config
	Link               *Link
	PacketAllocator    *PacketIDAllocator
	Slaves             []*SlaveNode
	TransactionManager *TransactionManager
	HomeNodeID         int
}

// RunRuntime executes the node logic under the coordinator-driven cycle model.
func (rn *RequestNode) RunRuntime(ctx *RequestNodeRuntime) {
	if rn.bindings == nil {
		return
	}
	coord := rn.bindings.Coordinator()
	if coord == nil {
		return
	}
	componentID := rn.bindings.ComponentID()
	for {
		cycle := coord.WaitForCycle(componentID)
		if cycle < 0 {
			return
		}
		rn.bindings.WaitIncoming(cycle)
		rn.bindings.SignalReceive(cycle)
		rn.Tick(cycle, ctx.Config, ctx.HomeNodeID, ctx.Link, ctx.PacketAllocator, ctx.Slaves, ctx.TransactionManager)
		rn.bindings.SignalSend(cycle)
		coord.MarkDone(componentID, cycle)
	}
}

func (rn *RequestNode) processIncomingMessage(msg *InFlightMessage, cycle int) {
	if msg == nil || msg.Packet == nil {
		return
	}

	packet := msg.Packet

	rn.mu.Lock()
	packet.ReceivedAt = cycle

	// Record PacketReceived event
	if rn.txnMgr != nil && packet.TransactionID > 0 {
		event := &core.PacketEvent{
			TransactionID:  packet.TransactionID,
			PacketID:       packet.ID,
			ParentPacketID: packet.ParentPacketID,
			NodeID:         rn.ID,
			EventType:      core.PacketReceived,
			Cycle:          cycle,
			EdgeKey:        nil, // in-node event
		}
		rn.txnMgr.RecordPacketEvent(event)
	}

	rn.handleResponseLocked(packet, cycle, rn.txnMgr)
	rn.mu.Unlock()
}

// Legacy type aliases for backward compatibility during transition
// These are kept temporarily for compatibility but should be migrated to RequestNode
type Master = RequestNode
type MasterStats = RequestNodeStats

// NewMaster creates a new RequestNode (legacy compatibility function)
// Deprecated: Use NewRequestNode instead
// Note: This function is kept for backward compatibility but requires a generator
// For legacy code, create a ProbabilityGenerator first
func NewMaster(id int, masterIndex int, generator RequestGenerator) *Master {
	return NewRequestNode(id, masterIndex, generator)
}

// weightedChoose returns an index in [0,len(weights)) with probability proportional to weights.
func weightedChoose(rng *rand.Rand, weights []int) int {
	if len(weights) == 0 {
		return 0
	}
	var sum int
	for _, w := range weights {
		if w > 0 {
			sum += w
		}
	}
	if sum <= 0 {
		// default to uniform among indices
		return rng.Intn(len(weights))
	}
	x := rng.Intn(sum)
	acc := 0
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		acc += w
		if x < acc {
			return i
		}
	}
	return len(weights) - 1
}

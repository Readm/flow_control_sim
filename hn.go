package main

import (
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/capabilities"
	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/hooks"
	"github.com/Readm/flow_sim/policy"
	"github.com/Readm/flow_sim/queue"
)

// CacheLine represents a single cache line in the HomeNode cache.
type CacheLine = capabilities.HomeCacheLine

// HomeNode (HN) represents a CHI Home Node that manages cache coherence and routes transactions.
// It receives requests from Request Nodes and forwards them to Slave Nodes,
// then routes responses back to the originating Request Node.
type HomeNode struct {
	Node     // embedded Node base class
	pipeline *PacketPipeline
	txnMgr   *TransactionManager // for recording packet events

	mu       sync.Mutex
	bindings *NodeCycleBindings

	broker    *hooks.PluginBroker
	policyMgr policy.Manager

	// Cache storage: simple map from address to cache line
	cacheCapability capabilities.CacheWithHomeStore
	cacheStore      capabilities.HomeCache
	cacheCapacity   int
	cacheEvictor    capabilities.CacheEvictor

	// Directory: tracks which RequestNodes have cached which addresses
	// map[address] -> set of RequestNode IDs
	directoryCapability capabilities.DirectoryCapability
	directoryStore      capabilities.DirectoryStore

	// Pending requests: tracks ReadOnce requests waiting for Snoop responses
	// map[transactionID] -> pending request info
	pendingRequests   map[int64]*PendingSnoopRequest
	pendingRequestsMu sync.RWMutex

	// PacketIDAllocator for generating response packet IDs
	packetIDs *PacketIDAllocator

	capabilities          []capabilities.NodeCapability
	capabilityRegistry    map[string]struct{}
	defaultCapsRegistered bool
}

func NewHomeNode(id int) *HomeNode {
	hn := &HomeNode{
		Node: Node{
			ID:   id,
			Type: core.NodeTypeHN,
		},
		txnMgr:          nil, // will be set by simulator
		bindings:        NewNodeCycleBindings(),
		pendingRequests: make(map[int64]*PendingSnoopRequest),
		packetIDs:       nil, // will be set by simulator
		cacheCapacity:   DefaultHomeCacheCapacity,
	}

	hn.AddQueue(string(pipelineStageIn), 0, queue.UnlimitedCapacity)
	hn.AddQueue(string(pipelineStageProcess), 0, DefaultForwardQueueCapacity)
	hn.AddQueue(string(pipelineStageOut), 0, queue.UnlimitedCapacity)

	hn.pipeline = newPacketPipeline(hn.makeStageMutator, PipelineCapacities{
		In:      queue.UnlimitedCapacity,
		Process: DefaultForwardQueueCapacity,
		Out:     queue.UnlimitedCapacity,
	}, PipelineHooks{
		Process: queue.StageQueueHooks[*PipelineMessage]{
			OnEnqueue: hn.enqueueHook,
			OnDequeue: hn.dequeueHook,
		},
	})

	return hn
}

func (hn *HomeNode) makeStageMutator(stage pipelineStageName) queue.MutateFunc {
	return func(length int, capacity int) {
		hn.UpdateQueueState(string(stage), length, capacity)
	}
}

func (hn *HomeNode) enqueueHook(entryID queue.EntryID, msg *PipelineMessage, cycle int) {
	if hn.txnMgr == nil || msg == nil || msg.Packet == nil || msg.Packet.TransactionID == 0 {
		return
	}
	event := &core.PacketEvent{
		TransactionID:  msg.Packet.TransactionID,
		PacketID:       msg.Packet.ID,
		ParentPacketID: msg.Packet.ParentPacketID,
		NodeID:         hn.ID,
		EventType:      core.PacketEnqueued,
		Cycle:          cycle,
		EdgeKey:        nil,
	}
	hn.txnMgr.RecordPacketEvent(event)
}

func (hn *HomeNode) dequeueHook(entryID queue.EntryID, msg *PipelineMessage, cycle int) {
	if hn.txnMgr == nil || msg == nil || msg.Packet == nil || msg.Packet.TransactionID == 0 {
		return
	}
	event := &core.PacketEvent{
		TransactionID:  msg.Packet.TransactionID,
		PacketID:       msg.Packet.ID,
		ParentPacketID: msg.Packet.ParentPacketID,
		NodeID:         hn.ID,
		EventType:      core.PacketDequeued,
		Cycle:          cycle,
		EdgeKey:        nil,
	}
	hn.txnMgr.RecordPacketEvent(event)
}

// SetTransactionManager sets the transaction manager for event recording
func (hn *HomeNode) SetTransactionManager(txnMgr *TransactionManager) {
	hn.txnMgr = txnMgr
}

// SetPluginBroker assigns the hook broker for routing and sending stages.
func (hn *HomeNode) SetPluginBroker(b *hooks.PluginBroker) {
	hn.broker = b
	hn.ensureDefaultCapabilities()
}

// SetPolicyManager assigns the policy manager used during routing decisions.
func (hn *HomeNode) SetPolicyManager(m policy.Manager) {
	hn.policyMgr = m
	hn.ensureDefaultCapabilities()
}

// SetCacheCapacity overrides the occupancy limit for the home cache before capability init.
func (hn *HomeNode) SetCacheCapacity(capacity int) {
	if capacity <= 0 {
		capacity = DefaultHomeCacheCapacity
	}
	hn.cacheCapacity = capacity
}

// SetPacketIDAllocator assigns the packet ID allocator for generating response packets.
func (hn *HomeNode) SetPacketIDAllocator(allocator *PacketIDAllocator) {
	hn.packetIDs = allocator
}

func (hn *HomeNode) ensureDefaultCapabilities() {
	if hn.defaultCapsRegistered && hn.policyMgr != nil {
		return
	}
	if hn.broker == nil {
		return
	}
	caps := []capabilities.NodeCapability{}
	if hn.cacheStore == nil {
		caps = append(caps, capabilities.NewHomeCacheCapability(
			fmt.Sprintf("home-cache-%d", hn.ID),
		))
	}
	if hn.directoryStore == nil {
		caps = append(caps, capabilities.NewDirectoryCapability(
			fmt.Sprintf("home-directory-%d", hn.ID),
		))
	}
	if hn.policyMgr != nil {
		caps = append(caps,
			capabilities.NewRoutingCapability(
				fmt.Sprintf("home-routing-%d", hn.ID),
				hn.policyMgr,
			),
			capabilities.NewFlowControlCapability(
				fmt.Sprintf("home-flow-%d", hn.ID),
				hn.policyMgr,
			),
		)
	}
	for _, cap := range caps {
		hn.registerCapability(cap)
	}
	if hn.cacheStore != nil && hn.cacheEvictor == nil {
		lruCap := capabilities.NewLRUEvictionCapability(
			fmt.Sprintf("home-cache-lru-%d", hn.ID),
			capabilities.LRUEvictionConfig{
				Capacity:  hn.cacheCapacity,
				HomeCache: hn.cacheStore,
			},
		)
		hn.registerCapability(lruCap)
	}
	if hn.policyMgr != nil {
		hn.defaultCapsRegistered = true
	}
}

func (hn *HomeNode) registerCapability(cap capabilities.NodeCapability) {
	if cap == nil || hn.broker == nil {
		return
	}
	desc := cap.Descriptor()
	if desc.Name == "" {
		desc.Name = fmt.Sprintf("home-capability-%d-%d", hn.ID, len(hn.capabilities))
	}
	if hn.capabilityRegistry == nil {
		hn.capabilityRegistry = make(map[string]struct{})
	}
	if _, exists := hn.capabilityRegistry[desc.Name]; exists {
		return
	}
	if err := cap.Register(hn.broker); err != nil {
		GetLogger().Warnf("HomeNode %d capability %s registration failed: %v", hn.ID, desc.Name, err)
		return
	}
	hn.capabilityRegistry[desc.Name] = struct{}{}
	hn.capabilities = append(hn.capabilities, cap)

	if cacheCap, ok := cap.(capabilities.CacheWithHomeStore); ok {
		hn.cacheCapability = cacheCap
		hn.cacheStore = cacheCap.HomeCache()
	}
	if dirCap, ok := cap.(capabilities.DirectoryCapability); ok {
		hn.directoryCapability = dirCap
		hn.directoryStore = dirCap.Directory()
	}
	if evictCap, ok := cap.(capabilities.CacheEvictor); ok {
		hn.cacheEvictor = evictCap
	}
}

// ConfigureCycleRuntime sets the coordinator bindings for the node.
func (hn *HomeNode) ConfigureCycleRuntime(componentID string, coord *CycleCoordinator) {
	if hn.bindings == nil {
		hn.bindings = NewNodeCycleBindings()
	}
	hn.bindings.SetComponent(componentID)
	hn.bindings.SetCoordinator(coord)
}

// RegisterIncomingSignal registers the receive-finished signal for an incoming edge.
func (hn *HomeNode) RegisterIncomingSignal(edge core.EdgeKey, signal *CycleSignal) {
	if hn.bindings == nil {
		hn.bindings = NewNodeCycleBindings()
	}
	hn.bindings.RegisterIncoming(edge, signal)
}

// RegisterOutgoingSignal registers the send-finished signal for an outgoing edge.
func (hn *HomeNode) RegisterOutgoingSignal(edge core.EdgeKey, signal *CycleSignal) {
	if hn.bindings == nil {
		hn.bindings = NewNodeCycleBindings()
	}
	hn.bindings.RegisterOutgoing(edge, signal)
}

// CanReceive checks if the HomeNode can receive packets from the given edge.
// HomeNode always can receive (in_queue has unlimited capacity).
func (hn *HomeNode) CanReceive(edgeKey core.EdgeKey, packetCount int) bool {
	return true
}

// OnPackets receives packets from the channel and enqueues them.
func (hn *HomeNode) OnPackets(messages []*InFlightMessage, cycle int) {
	for _, msg := range messages {
		hn.processIncomingMessage(msg, cycle)
	}
}

// OnPacket enqueues a CHI packet received at the Home Node.
// For ReadNoSnp requests, this will be forwarded to the target Slave Node.
// For responses from Slave Nodes, this will be forwarded back to the Request Node.
// This method is kept for backward compatibility but is now called by OnPackets.
func (hn *HomeNode) OnPacket(p *core.Packet, cycle int, ch *Link, cfg *Config) {
	if p == nil {
		return
	}
	p.ReceivedAt = cycle
	hn.mu.Lock()
	defer hn.mu.Unlock()
	if inQ := hn.inQueue(); inQ != nil {
		msg := &PipelineMessage{Packet: p, Kind: "incoming"}
		if _, ok := inQ.Enqueue(msg, cycle); !ok {
			GetLogger().Warnf("HomeNode %d: in_queue full, dropping packet %d", hn.ID, p.ID)
		}
	}
}

// PendingSnoopRequest tracks a ReadOnce request waiting for Snoop responses
type PendingSnoopRequest struct {
	RequestingRNID int   // The RN that sent the original ReadOnce request
	OriginalReqID  int64 // The RequestID of the original ReadOnce request
	TotalSnoops    int   // Total number of Snoop requests sent
	ReceivedSnoops int   // Number of Snoop responses received
	AllNoData      bool  // True if all received responses are SnpNoData
}

// generateSnoopRequest generates a Snoop request packet for the given address and target RN.
func (hn *HomeNode) generateSnoopRequest(addr uint64, targetRNID int, originalTxnID int64, txnType core.CHITransactionType, cycle int) *core.Packet {
	if hn.packetIDs == nil {
		return nil
	}
	snoopID := hn.packetIDs.Allocate()
	return &core.Packet{
		ID:              snoopID,
		Type:            "request", // legacy
		SrcID:           hn.ID,
		DstID:           targetRNID,
		GeneratedAt:     cycle,
		SentAt:          0,
		MasterID:        targetRNID,
		RequestID:       snoopID,
		TransactionType: txnType,
		MessageType:     core.CHIMsgSnp,
		Address:         addr,
		DataSize:        DefaultCacheLineSize,
		TransactionID:   originalTxnID, // Use the original transaction ID
		SnoopTargetID:   targetRNID,
		ParentPacketID:  0, // Snoop request is generated by HN
	}
}

// generateCacheHitResponse generates a CompData response for a cache hit.
func (hn *HomeNode) generateCacheHitResponse(req *core.Packet, cycle int) *core.Packet {
	if hn.packetIDs == nil {
		return nil
	}
	respID := hn.packetIDs.Allocate()
	resp := &core.Packet{
		ID:              respID,
		Type:            "response", // legacy field
		SrcID:           hn.ID,
		DstID:           req.MasterID, // return to Request Node
		GeneratedAt:     cycle,
		SentAt:          0,
		MasterID:        req.MasterID,
		RequestID:       req.RequestID,
		TransactionType: req.TransactionType,
		MessageType:     core.CHIMsgComp,
		ResponseType:    core.CHIRespCompData,
		Address:         req.Address,
		DataSize:        req.DataSize,
		TransactionID:   req.TransactionID,
		ParentPacketID:  req.ID,
	}
	return resp
}

// Tick processes pipeline stages and returns the number of packets sent this cycle.
func (hn *HomeNode) Tick(cycle int, ch *Link, cfg *Config) int {
	if cfg == nil || ch == nil {
		return 0
	}

	hn.mu.Lock()
	defer hn.mu.Unlock()

	hn.releaseToProcess(-1, cycle)
	hn.processStage(cycle, cfg)
	return hn.sendFromOutQueue(cycle, cfg, ch)
}

func (hn *HomeNode) processStage(cycle int, cfg *Config) {
	procQ := hn.processQueue()
	if procQ == nil {
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
		p := msg.Packet
		hn.emitProcessHook(p, cycle, true)
		forward := true

		if p.MessageType == core.CHIMsgReq && p.TransactionType == core.CHITxnReadOnce {
			forward = hn.processReadOnceRequest(p, cycle, cfg)
		}

		if p.MessageType == core.CHIMsgSnpResp {
			forward = hn.processSnoopResponse(p, cycle, cfg) && forward
		}

		if p.MessageType == core.CHIMsgComp && p.ResponseType == core.CHIRespCompData {
			hn.processCompDataResponse(p, cycle)
		}

		if forward {
			hn.enqueueDefaultForward(p, cfg, cycle)
		}

		procQ.Complete(entryID, cycle)
		hn.emitProcessHook(p, cycle, false)
	}
}

func (hn *HomeNode) processReadOnceRequest(p *core.Packet, cycle int, cfg *Config) bool {
	forward := true

	// Directory lookup
	var sharers []int
	if hn.directoryStore != nil {
		sharers = hn.directoryStore.Sharers(p.Address)
	}

	filteredSharers := make([]int, 0, len(sharers))
	for _, rnID := range sharers {
		if rnID != p.MasterID {
			filteredSharers = append(filteredSharers, rnID)
		}
	}

	if len(filteredSharers) > 0 {
		forward = false
		// Track pending request
		hn.pendingRequestsMu.Lock()
		hn.pendingRequests[p.TransactionID] = &PendingSnoopRequest{
			RequestingRNID: p.MasterID,
			OriginalReqID:  p.RequestID,
			TotalSnoops:    len(filteredSharers),
			ReceivedSnoops: 0,
			AllNoData:      true,
		}
		hn.pendingRequestsMu.Unlock()

		for _, sharerID := range filteredSharers {
			snoopReq := hn.generateSnoopRequest(p.Address, sharerID, p.TransactionID, p.TransactionType, cycle)
			if snoopReq == nil {
				continue
			}
			if hn.txnMgr != nil && p.TransactionID > 0 {
				event := &core.PacketEvent{
					TransactionID:  p.TransactionID,
					PacketID:       snoopReq.ID,
					ParentPacketID: p.ID,
					NodeID:         hn.ID,
					EventType:      core.PacketGenerated,
					Cycle:          cycle,
					EdgeKey:        nil,
				}
				hn.txnMgr.RecordPacketEvent(event)
			}
			hn.enqueueOutgoing(snoopReq, "snoop_request", sharerID, cfg.RelayMasterLatency, cycle)
		}
		return false
	}

	cacheHit := false
	if hn.cacheStore != nil {
		if line, ok := hn.cacheStore.GetLine(p.Address); ok && line.Valid {
			cacheHit = true
		}
	}

	if cacheHit {
		forward = false
		resp := hn.generateCacheHitResponse(p, cycle)
		if resp != nil {
			if hn.cacheEvictor != nil {
				hn.cacheEvictor.Touch(p.Address)
			}
			if hn.txnMgr != nil && p.TransactionID > 0 {
				event := &core.PacketEvent{
					TransactionID:  p.TransactionID,
					PacketID:       resp.ID,
					ParentPacketID: p.ID,
					NodeID:         hn.ID,
					EventType:      core.PacketGenerated,
					Cycle:          cycle,
					EdgeKey:        nil,
				}
				hn.txnMgr.RecordPacketEvent(event)
			}
			hn.enqueueOutgoing(resp, "forward_response", resp.DstID, cfg.RelayMasterLatency, cycle)
			if hn.directoryStore != nil {
				hn.directoryStore.Add(p.Address, p.MasterID)
			}
			if hn.txnMgr != nil && p.TransactionID > 0 {
				txn := hn.txnMgr.GetTransaction(p.TransactionID)
				if txn != nil && txn.Context != nil {
					if txn.Context.Metadata == nil {
						txn.Context.Metadata = make(map[string]string)
					}
					txn.Context.Metadata["cache_hit"] = "true"
					txn.Context.Metadata["cache_hit_node"] = fmt.Sprintf("%d", hn.ID)
				}
			}
		}
	} else {
		if hn.txnMgr != nil && p.TransactionID > 0 {
			txn := hn.txnMgr.GetTransaction(p.TransactionID)
			if txn != nil && txn.Context != nil {
				if txn.Context.Metadata == nil {
					txn.Context.Metadata = make(map[string]string)
				}
				txn.Context.Metadata["cache_miss"] = "true"
				txn.Context.Metadata["cache_miss_node"] = fmt.Sprintf("%d", hn.ID)
			}
		}
	}

	return forward
}

func (hn *HomeNode) processSnoopResponse(p *core.Packet, cycle int, cfg *Config) bool {
	hn.pendingRequestsMu.Lock()
	pendingReq, hasPending := hn.pendingRequests[p.TransactionID]
	if !hasPending {
		hn.pendingRequestsMu.Unlock()
		return false
	}
	pendingReq.ReceivedSnoops++

	if p.ResponseType == core.CHIRespSnpData {
		requestingRNID := pendingReq.RequestingRNID
		delete(hn.pendingRequests, p.TransactionID)
		hn.pendingRequestsMu.Unlock()

		if hn.packetIDs == nil {
			return false
		}
		resp := &core.Packet{
			ID:              hn.packetIDs.Allocate(),
			Type:            "response",
			SrcID:           hn.ID,
			DstID:           requestingRNID,
			GeneratedAt:     cycle,
			SentAt:          0,
			MasterID:        requestingRNID,
			RequestID:       pendingReq.OriginalReqID,
			TransactionType: p.TransactionType,
			MessageType:     core.CHIMsgComp,
			ResponseType:    core.CHIRespCompData,
			Address:         p.Address,
			DataSize:        p.DataSize,
			TransactionID:   p.TransactionID,
			ParentPacketID:  p.ID,
		}
		if hn.txnMgr != nil && p.TransactionID > 0 {
			event := &core.PacketEvent{
				TransactionID:  p.TransactionID,
				PacketID:       resp.ID,
				ParentPacketID: p.ID,
				NodeID:         hn.ID,
				EventType:      core.PacketGenerated,
				Cycle:          cycle,
				EdgeKey:        nil,
			}
			hn.txnMgr.RecordPacketEvent(event)
		}
		hn.enqueueOutgoing(resp, "forward_response", resp.DstID, cfg.RelayMasterLatency, cycle)
		if hn.directoryStore != nil {
			hn.directoryStore.Add(p.Address, requestingRNID)
		}
		return false
	}

	pendingReq.AllNoData = pendingReq.AllNoData && true
	allDone := pendingReq.ReceivedSnoops >= pendingReq.TotalSnoops
	if allDone {
		delete(hn.pendingRequests, p.TransactionID)
	}
	hn.pendingRequestsMu.Unlock()

	return false
}

func (hn *HomeNode) processCompDataResponse(p *core.Packet, cycle int) {
	if p.TransactionType != core.CHITxnReadOnce {
		return
	}

	var requestingRNID int
	hn.pendingRequestsMu.Lock()
	if pendingReq, hasPending := hn.pendingRequests[p.TransactionID]; hasPending {
		requestingRNID = pendingReq.RequestingRNID
		delete(hn.pendingRequests, p.TransactionID)
	} else {
		requestingRNID = p.MasterID
	}
	hn.pendingRequestsMu.Unlock()

	if hn.cacheStore != nil {
		hn.cacheStore.UpdateLine(p.Address, CacheLine{Valid: true})
	}
	if hn.cacheEvictor != nil {
		hn.cacheEvictor.Fill(p.Address)
	}
	if hn.directoryStore != nil {
		hn.directoryStore.Add(p.Address, requestingRNID)
	}
	if hn.txnMgr != nil && p.TransactionID > 0 {
		txn := hn.txnMgr.GetTransaction(p.TransactionID)
		if txn != nil && txn.Context != nil {
			if txn.Context.Metadata == nil {
				txn.Context.Metadata = make(map[string]string)
			}
			txn.Context.Metadata["cache_updated"] = "true"
			txn.Context.Metadata["cache_updated_node"] = fmt.Sprintf("%d", hn.ID)
		}
	}
}

func (hn *HomeNode) sendFromOutQueue(cycle int, cfg *Config, ch *Link) int {
	outQ := hn.outQueue()
	if outQ == nil {
		return 0
	}
	sent := 0
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
		defaultTarget := msg.DefaultTarget
		latency := msg.Latency
		if (defaultTarget == 0 || latency == 0) && packet != nil {
			if dt, dl, ok := hn.defaultRoute(packet, cfg); ok {
				if defaultTarget == 0 {
					defaultTarget = dt
				}
				if latency == 0 {
					latency = dl
				}
			}
		}

		needsRouting := msg.Kind == "forward_request" || msg.Kind == "forward_response" || msg.Kind == "forward_other"
		finalTarget := msg.TargetID
		if finalTarget == 0 {
			finalTarget = defaultTarget
		}
		if needsRouting {
			resolved, ok := hn.resolveRoute(packet, defaultTarget)
			if !ok {
				outQ.ResetPending(entryID)
				continue
			}
			finalTarget = resolved
		}
		if finalTarget < 0 {
			outQ.Complete(entryID, cycle)
			continue
		}
		latency = hn.adjustLatency(packet, cfg, finalTarget)
		if !hn.emitBeforeSend(packet, finalTarget, cycle) {
			outQ.ResetPending(entryID)
			continue
		}
		ch.Send(packet, hn.ID, finalTarget, cycle, latency)
		packet.SentAt = cycle
		hn.emitAfterSend(packet, finalTarget, cycle)
		outQ.Complete(entryID, cycle)
		sent++
	}
	return sent
}

func (hn *HomeNode) resolveRoute(packet *core.Packet, defaultTarget int) (int, bool) {
	targetID := defaultTarget
	if hn.broker == nil || packet == nil {
		if targetID == 0 {
			targetID = packet.DstID
		}
		return targetID, true
	}
	if targetID == 0 {
		targetID = packet.DstID
	}
	beforeCtx := &hooks.RouteContext{
		Packet:        packet,
		SourceNodeID:  hn.ID,
		DefaultTarget: targetID,
		TargetID:      targetID,
	}
	if err := hn.broker.EmitBeforeRoute(beforeCtx); err != nil {
		GetLogger().Warnf("HomeNode %d OnBeforeRoute hook failed: %v", hn.ID, err)
		return 0, false
	}
	afterCtx := &hooks.RouteContext{
		Packet:        packet,
		SourceNodeID:  hn.ID,
		DefaultTarget: targetID,
		TargetID:      beforeCtx.TargetID,
	}
	if err := hn.broker.EmitAfterRoute(afterCtx); err != nil {
		GetLogger().Warnf("HomeNode %d OnAfterRoute hook failed: %v", hn.ID, err)
	}
	if afterCtx.TargetID <= 0 {
		return targetID, true
	}
	return afterCtx.TargetID, true
}

func (hn *HomeNode) emitBeforeSend(packet *core.Packet, targetID int, cycle int) bool {
	if hn.broker == nil || packet == nil {
		return true
	}
	ctx := &hooks.MessageContext{
		Packet:   packet,
		NodeID:   hn.ID,
		TargetID: targetID,
		Cycle:    cycle,
	}
	if err := hn.broker.EmitBeforeSend(ctx); err != nil {
		GetLogger().Warnf("HomeNode %d OnBeforeSend hook failed: %v", hn.ID, err)
		return false
	}
	return true
}

func (hn *HomeNode) emitAfterSend(packet *core.Packet, targetID int, cycle int) {
	if hn.broker == nil || packet == nil {
		return
	}
	ctx := &hooks.MessageContext{
		Packet:   packet,
		NodeID:   hn.ID,
		TargetID: targetID,
		Cycle:    cycle,
	}
	if err := hn.broker.EmitAfterSend(ctx); err != nil {
		GetLogger().Warnf("HomeNode %d OnAfterSend hook failed: %v", hn.ID, err)
	}
}

func (hn *HomeNode) defaultRoute(p *core.Packet, cfg *Config) (target int, latency int, ok bool) {
	switch {
	case p.MessageType == core.CHIMsgReq || p.Type == "request":
		return p.DstID, cfg.RelaySlaveLatency, true
	case p.MessageType == core.CHIMsgComp || p.MessageType == core.CHIMsgResp || p.Type == "response":
		if p.MessageType == core.CHIMsgComp || p.MessageType == core.CHIMsgResp {
			return p.MasterID, cfg.RelayMasterLatency, true
		}
		return p.DstID, cfg.RelayMasterLatency, true
	default:
		return 0, 0, false
	}
}

func (hn *HomeNode) adjustLatency(p *core.Packet, cfg *Config, target int) int {
	if p.MessageType == core.CHIMsgReq || p.Type == "request" {
		return cfg.RelaySlaveLatency
	}
	if p.MessageType == core.CHIMsgComp || p.MessageType == core.CHIMsgResp || p.Type == "response" {
		return cfg.RelayMasterLatency
	}
	return cfg.RelaySlaveLatency
}

func (hn *HomeNode) determineForwardKind(packet *core.Packet) string {
	switch {
	case packet == nil:
		return "forward_other"
	case packet.MessageType == core.CHIMsgSnp:
		return "snoop_request"
	case packet.MessageType == core.CHIMsgReq || packet.Type == "request":
		return "forward_request"
	case packet.MessageType == core.CHIMsgComp || packet.MessageType == core.CHIMsgResp || packet.Type == "response":
		return "forward_response"
	default:
		return "forward_other"
	}
}

func (hn *HomeNode) enqueueDefaultForward(packet *core.Packet, cfg *Config, cycle int) {
	if packet == nil {
		return
	}
	defaultTarget, latency, ok := hn.defaultRoute(packet, cfg)
	if !ok {
		return
	}
	kind := hn.determineForwardKind(packet)
	hn.enqueueOutgoing(packet, kind, defaultTarget, latency, cycle)
}

// HomeNodeRuntime contains dependencies required during runtime execution.
type HomeNodeRuntime struct {
	Config *Config
	Link   *Link
}

// RunRuntime executes the home node logic driven by the coordinator.
func (hn *HomeNode) RunRuntime(ctx *HomeNodeRuntime) {
	if hn.bindings == nil {
		return
	}
	coord := hn.bindings.Coordinator()
	if coord == nil {
		return
	}
	componentID := hn.bindings.ComponentID()
	for {
		cycle := coord.WaitForCycle(componentID)
		if cycle < 0 {
			return
		}
		hn.bindings.WaitIncoming(cycle)
		hn.bindings.SignalReceive(cycle)
		hn.Tick(cycle, ctx.Link, ctx.Config)
		hn.bindings.SignalSend(cycle)
		coord.MarkDone(componentID, cycle)
	}
}

// GetQueuePackets returns packet information across in/process/out queues
func (hn *HomeNode) GetQueuePackets() []core.PacketInfo {
	hn.mu.Lock()
	defer hn.mu.Unlock()
	packets := make([]core.PacketInfo, 0)
	packets = hn.appendStagePackets(packets, hn.inQueue())
	packets = hn.appendStagePackets(packets, hn.processQueue())
	packets = hn.appendStagePackets(packets, hn.outQueue())
	return packets
}

func (hn *HomeNode) appendStagePackets(dst []core.PacketInfo, q *queue.StageQueue[*PipelineMessage]) []core.PacketInfo {
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

// Legacy type alias for backward compatibility during transition
// This is kept temporarily for compatibility but should be migrated to HomeNode
type Relay = HomeNode

// NewRelay creates a new HomeNode (legacy compatibility function)
// Deprecated: Use NewHomeNode instead
func NewRelay(id int) *Relay {
	return NewHomeNode(id)
}

func (hn *HomeNode) processIncomingMessage(msg *InFlightMessage, cycle int) {
	if msg == nil || msg.Packet == nil {
		return
	}

	packet := msg.Packet
	packet.ReceivedAt = cycle

	hn.mu.Lock()
	defer hn.mu.Unlock()

	// Record PacketReceived event
	if hn.txnMgr != nil && packet.TransactionID > 0 {
		event := &core.PacketEvent{
			TransactionID:  packet.TransactionID,
			PacketID:       packet.ID,
			ParentPacketID: packet.ParentPacketID,
			NodeID:         hn.ID,
			EventType:      core.PacketReceived,
			Cycle:          cycle,
			EdgeKey:        nil, // in-node event
		}
		hn.txnMgr.RecordPacketEvent(event)
	}

	if inQ := hn.inQueue(); inQ != nil {
		msg := &PipelineMessage{Packet: packet, Kind: "incoming"}
		if _, ok := inQ.Enqueue(msg, cycle); !ok {
			GetLogger().Warnf("HomeNode %d: in_queue full, dropping packet %d", hn.ID, packet.ID)
		}
	}
}

func (hn *HomeNode) releaseToProcess(limit int, cycle int) {
	inQ := hn.inQueue()
	procQ := hn.processQueue()
	if inQ == nil || procQ == nil {
		return
	}
	if limit <= 0 {
		limit = inQ.Len()
	}
	moved := 0
	for moved < limit {
		entryID, msg, ok := inQ.PeekNext()
		if !ok {
			break
		}
		if msg == nil {
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

func (hn *HomeNode) stageQueue(stage pipelineStageName) *queue.StageQueue[*PipelineMessage] {
	if hn == nil || hn.pipeline == nil {
		return nil
	}
	return hn.pipeline.queue(stage)
}

func (hn *HomeNode) inQueue() *queue.StageQueue[*PipelineMessage] {
	return hn.stageQueue(pipelineStageIn)
}

func (hn *HomeNode) processQueue() *queue.StageQueue[*PipelineMessage] {
	return hn.stageQueue(pipelineStageProcess)
}

func (hn *HomeNode) outQueue() *queue.StageQueue[*PipelineMessage] {
	return hn.stageQueue(pipelineStageOut)
}

func (hn *HomeNode) enqueueOutgoing(packet *core.Packet, kind string, defaultTarget int, latency int, cycle int) {
	if packet == nil {
		return
	}
	msg := &PipelineMessage{
		Packet:        packet,
		Kind:          kind,
		DefaultTarget: defaultTarget,
		Latency:       latency,
	}
	if outQ := hn.outQueue(); outQ != nil {
		if _, ok := outQ.Enqueue(msg, cycle); !ok {
			GetLogger().Warnf("HomeNode %d: out_queue full, dropping packet %d", hn.ID, packet.ID)
		}
	}
}

func (hn *HomeNode) emitProcessHook(packet *core.Packet, cycle int, before bool) {
	if hn.broker == nil || packet == nil {
		return
	}
	ctx := &hooks.ProcessContext{
		Packet: packet,
		Node:   hn,
		NodeID: hn.ID,
		Cycle:  cycle,
	}
	var err error
	if before {
		err = hn.broker.EmitBeforeProcess(ctx)
	} else {
		err = hn.broker.EmitAfterProcess(ctx)
	}
	if err != nil {
		GetLogger().Warnf("HomeNode %d process hook failed: %v", hn.ID, err)
	}
}

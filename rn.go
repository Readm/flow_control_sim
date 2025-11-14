package main

import (
	"fmt"
	"math/rand"
	"strconv"
	"sync"

	"github.com/Readm/flow_sim/capabilities"
	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/hooks"
	"github.com/Readm/flow_sim/policy"
	"github.com/Readm/flow_sim/queue"
)

// RequestNode (RN) represents a CHI Request Node that initiates transactions.
// It generates CHI protocol requests and collects responses.
type RequestNode struct {
	Node        // embedded Node base class
	generator   RequestGenerator
	masterIndex int // index of this master in the masters array (for generator)
	broker      *hooks.PluginBroker
	policyMgr   policy.Manager

	pipeline          *PacketPipeline
	dispatchByRequest map[int64]queue.EntryID
	outInFlightBlock  queue.BlockIndex
	cacheCapacity     int
	cacheEvictor      capabilities.CacheEvictor
	addrMapper        AddressMapper
	routerID          int

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

	// PacketIDAllocator for generating snoop response packets
	packetIDs *PacketIDAllocator

	mu       sync.Mutex
	bindings *NodeCycleBindings

	capabilities          []capabilities.NodeCapability
	capabilityRegistry    map[string]struct{}
	defaultCapsRegistered bool

	cacheCapability capabilities.CacheWithRequestStore
	cacheStore      capabilities.RequestCache
	txnCapability   capabilities.TransactionCapability
	txnCreator      capabilities.TransactionCreator
}

func NewRequestNode(id int, masterIndex int, generator RequestGenerator) *RequestNode {
	rn := &RequestNode{
		Node: Node{
			ID:   id,
			Type: core.NodeTypeRN,
		},
		generator:         generator,
		masterIndex:       masterIndex,
		generatedAtByReq:  make(map[int64]int),
		dispatchByRequest: make(map[int64]queue.EntryID),
		MinDelay:          int(^uint(0) >> 1),
		nextAddress:       DefaultAddressBase,
		packetIDs:         nil,
		cacheCapacity:     DefaultRequestCacheCapacity,
	}

	rn.AddQueue(string(pipelineStageIn), 0, queue.UnlimitedCapacity)
	rn.AddQueue(string(pipelineStageProcess), 0, DefaultDispatchQueueCapacity)
	rn.AddQueue(string(pipelineStageOut), 0, queue.UnlimitedCapacity)

	rn.pipeline = newPacketPipeline(rn.makeStageMutator, PipelineCapacities{
		In:      queue.UnlimitedCapacity,
		Process: DefaultDispatchQueueCapacity,
		Out:     queue.UnlimitedCapacity,
	}, PipelineHooks{
		Process: queue.StageQueueHooks[*PipelineMessage]{
			OnEnqueue: rn.dispatchEnqueueHook,
			OnDequeue: rn.dispatchDequeueHook,
		},
	})

	if blockIdx, err := rn.pipeline.registerBlockReason("rn_out_inflight"); err == nil {
		rn.outInFlightBlock = blockIdx
	} else {
		rn.outInFlightBlock = queue.InvalidBlockIndex
		GetLogger().Warnf("RequestNode %d: failed to register out queue block bit: %v", rn.ID, err)
	}

	rn.bindings = NewNodeCycleBindings()
	return rn
}

func (rn *RequestNode) makeStageMutator(stage pipelineStageName) queue.MutateFunc {
	return func(length int, capacity int) {
		rn.UpdateQueueState(string(stage), length, capacity)
	}
}

func (rn *RequestNode) dispatchEnqueueHook(entryID queue.EntryID, msg *PipelineMessage, cycle int) {
	if rn.txnMgr == nil || msg == nil || msg.Packet == nil || msg.Packet.TransactionID == 0 {
		return
	}
	event := &core.PacketEvent{
		TransactionID:  msg.Packet.TransactionID,
		PacketID:       msg.Packet.ID,
		ParentPacketID: msg.Packet.ParentPacketID,
		NodeID:         rn.ID,
		EventType:      core.PacketEnqueued,
		Cycle:          cycle,
		EdgeKey:        nil,
	}
	rn.txnMgr.RecordPacketEvent(event)
}

func (rn *RequestNode) dispatchDequeueHook(entryID queue.EntryID, msg *PipelineMessage, cycle int) {
	if rn.txnMgr == nil || msg == nil || msg.Packet == nil || msg.Packet.TransactionID == 0 {
		return
	}
	event := &core.PacketEvent{
		TransactionID:  msg.Packet.TransactionID,
		PacketID:       msg.Packet.ID,
		ParentPacketID: msg.Packet.ParentPacketID,
		NodeID:         rn.ID,
		EventType:      core.PacketDequeued,
		Cycle:          cycle,
		EdgeKey:        nil,
	}
	rn.txnMgr.RecordPacketEvent(event)
}

// SetCacheCapacity overrides the request cache capacity before capabilities are initialised.
func (rn *RequestNode) SetCacheCapacity(capacity int) {
	if capacity <= 0 {
		capacity = DefaultRequestCacheCapacity
	}
	rn.cacheCapacity = capacity
}

// SetAddressMapper configures the address mapper used for slave selection.
func (rn *RequestNode) SetAddressMapper(mapper AddressMapper) {
	rn.addrMapper = mapper
}

// SetTxFactory installs a transaction capability backed by the provided factory.
func (rn *RequestNode) SetTxFactory(factory *TxFactory) {
	if factory == nil {
		return
	}
	cap := capabilities.NewTransactionCapability(
		fmt.Sprintf("request-txn-factory-%d", rn.ID),
		func(params capabilities.TxRequestParams) (*core.Packet, *core.Transaction, error) {
			packet, txn := factory.CreateRequest(params)
			if packet == nil {
				return nil, txn, fmt.Errorf("tx factory returned nil packet")
			}
			return packet, txn, nil
		},
	)
	rn.registerCapability(cap)
}

// SetPluginBroker assigns the hook broker for lifecycle callbacks.
func (rn *RequestNode) SetPluginBroker(b *hooks.PluginBroker) {
	rn.broker = b
	rn.ensureDefaultCapabilities()
}

// SetPolicyManager assigns the policy manager for routing decisions.
func (rn *RequestNode) SetPolicyManager(m policy.Manager) {
	rn.policyMgr = m
	rn.ensureDefaultCapabilities()
}

// SetRouterID sets the router node used for ring forwarding.
func (rn *RequestNode) SetRouterID(id int) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	rn.routerID = id
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
// Implements unified pipeline phases: generate -> in_queue -> process_queue -> out_queue.
func (rn *RequestNode) Tick(cycle int, cfg *Config, homeNodeID int, ch *Link, packetIDs *PacketIDAllocator, slaves []*SlaveNode, txnMgr *TransactionManager) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	if packetIDs != nil {
		rn.packetIDs = packetIDs
	}
	if txnMgr != nil {
		rn.txnMgr = txnMgr
	}

	if homeNodeID < 0 {
		return
	}

	capacity := cfg.DispatchQueueCapacity
	if capacity <= 0 || capacity == -1 {
		capacity = DefaultDispatchQueueCapacity
	}
	rn.pipeline.setCapacity(pipelineStageProcess, capacity)

	bandwidth := cfg.BandwidthLimit
	if bandwidth <= 0 {
		bandwidth = 1
	}

	rn.generateRequests(cycle, slaves)
	rn.releaseToProcess(bandwidth, cycle)
	rn.processStage(bandwidth, cycle, cfg, homeNodeID)
	rn.sendFromOutQueue(bandwidth, cycle, cfg, ch, homeNodeID)
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

// SetPacketIDAllocator assigns the packet ID allocator for generating snoop response packets.
func (rn *RequestNode) SetPacketIDAllocator(allocator *PacketIDAllocator) {
	rn.packetIDs = allocator
}

func (rn *RequestNode) ensureDefaultCapabilities() {
	if rn.defaultCapsRegistered && rn.policyMgr != nil {
		return
	}
	if rn.broker == nil {
		return
	}
	caps := []capabilities.NodeCapability{}
	if rn.cacheStore == nil {
		caps = append(caps, capabilities.NewMESICacheCapability(
			fmt.Sprintf("request-cache-%d", rn.ID),
		))
	}
	if rn.txnCreator == nil {
		packetAllocator := func() (int64, error) {
			if rn.packetIDs == nil {
				return 0, fmt.Errorf("packet allocator not configured")
			}
			return rn.packetIDs.Allocate(), nil
		}
		transactionCreator := func(txType core.CHITransactionType, addr uint64, cycle int) *core.Transaction {
			if rn.txnMgr == nil {
				return nil
			}
			txn := rn.txnMgr.CreateTransaction(txType, addr, cycle)
			if txn == nil {
				return nil
			}
			return txn
		}
		caps = append(caps, capabilities.NewDefaultTransactionCapability(
			fmt.Sprintf("request-txn-default-%d", rn.ID),
			packetAllocator,
			transactionCreator,
		))
	}
	if rn.policyMgr != nil {
		caps = append(caps,
			capabilities.NewRoutingCapability(
				fmt.Sprintf("request-routing-%d", rn.ID),
				rn.policyMgr,
			),
			capabilities.NewFlowControlCapability(
				fmt.Sprintf("request-flow-%d", rn.ID),
				rn.policyMgr,
			),
		)
	}
	for _, cap := range caps {
		rn.registerCapability(cap)
	}
	if rn.cacheStore != nil && rn.cacheEvictor == nil {
		lruCap := capabilities.NewLRUEvictionCapability(
			fmt.Sprintf("request-cache-lru-%d", rn.ID),
			capabilities.LRUEvictionConfig{
				Capacity:     rn.cacheCapacity,
				RequestCache: rn.cacheStore,
			},
		)
		rn.registerCapability(lruCap)
	}
	if rn.policyMgr != nil {
		rn.defaultCapsRegistered = true
	}
}

func (rn *RequestNode) registerCapability(cap capabilities.NodeCapability) {
	if cap == nil || rn.broker == nil {
		return
	}
	desc := cap.Descriptor()
	if desc.Name == "" {
		desc.Name = fmt.Sprintf("request-capability-%d-%d", rn.ID, len(rn.capabilities))
	}
	if rn.capabilityRegistry == nil {
		rn.capabilityRegistry = make(map[string]struct{})
	}
	if _, exists := rn.capabilityRegistry[desc.Name]; exists {
		return
	}
	if err := cap.Register(rn.broker); err != nil {
		GetLogger().Warnf("RequestNode %d capability %s registration failed: %v", rn.ID, desc.Name, err)
		return
	}
	rn.capabilityRegistry[desc.Name] = struct{}{}
	rn.capabilities = append(rn.capabilities, cap)

	if cacheCap, ok := cap.(capabilities.CacheWithRequestStore); ok {
		rn.cacheCapability = cacheCap
		rn.cacheStore = cacheCap.RequestCache()
	}
	if evictCap, ok := cap.(capabilities.CacheEvictor); ok {
		rn.cacheEvictor = evictCap
	}
	if txnCap, ok := cap.(capabilities.TransactionCapability); ok {
		rn.txnCapability = txnCap
		rn.txnCreator = txnCap.Creator()
	}
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
	isCHIResponse := p.MessageType == core.CHIMsgComp || p.MessageType == core.CHIMsgResp
	isLegacyResponse := p.Type == "response"
	if !isCHIResponse && !isLegacyResponse {
		return
	}

	rn.emitProcessHook(p, cycle, true)
	defer rn.emitProcessHook(p, cycle, false)

	requestID := p.RequestID
	outQ := rn.outQueue()
	var matched bool
	if outQ != nil {
		if entryID, ok := rn.dispatchByRequest[requestID]; ok {
			if _, ok := outQ.Remove(entryID, cycle); ok {
				matched = true
			} else {
				if _, ok := outQ.RemoveMatch(func(msg *PipelineMessage) bool {
					return msg != nil && msg.Packet != nil && msg.Packet.RequestID == requestID
				}, cycle); ok {
					matched = true
				}
			}
		} else {
			if _, ok := outQ.RemoveMatch(func(msg *PipelineMessage) bool {
				return msg != nil && msg.Packet != nil && msg.Packet.RequestID == requestID
			}, cycle); ok {
				matched = true
			}
		}
	}

	if matched {
		delete(rn.dispatchByRequest, requestID)
	}

	if !matched {
		return
	}

	if gen, ok := rn.generatedAtByReq[requestID]; ok {
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

	if txnMgr != nil && p.TransactionID > 0 {
		txnMgr.MarkTransactionCompleted(p.TransactionID, cycle)
	}

	if cacheHandler, ok := rn.cacheCapability.(capabilities.RequestCacheHandler); ok {
		cacheHandler.HandleResponse(p)
		if p.TransactionType == core.CHITxnReadOnce && p.ResponseType == core.CHIRespCompData {
			GetLogger().Infof("[MESI] RequestNode %d: Cache updated to Shared for address 0x%x (TxnID=%d)",
				rn.ID, p.Address, p.TransactionID)
		}
	}

	if rn.cacheEvictor != nil && p.ResponseType == core.CHIRespCompData {
		rn.cacheEvictor.Fill(p.Address)
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

// GetQueuePackets returns packet information across in/process/out queues for visualization
func (rn *RequestNode) GetQueuePackets() []core.PacketInfo {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	packets := make([]core.PacketInfo, 0)
	packets = rn.appendStagePackets(packets, rn.inQueue())
	packets = rn.appendStagePackets(packets, rn.processQueue())
	packets = rn.appendStagePackets(packets, rn.outQueue())
	return packets
}

func (rn *RequestNode) GetStimulusQueuePackets() []core.PacketInfo {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	return rn.appendStagePackets(nil, rn.inQueue())
}

func (rn *RequestNode) GetDispatchQueuePackets() []core.PacketInfo {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	return rn.appendStagePackets(nil, rn.outQueue())
}

func (rn *RequestNode) appendStagePackets(dst []core.PacketInfo, q *queue.StageQueue[*PipelineMessage]) []core.PacketInfo {
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

	// Handle Snoop requests
	if packet.MessageType == core.CHIMsgSnp {
		rn.handleSnoopRequestLocked(packet, cycle)
		rn.mu.Unlock()
		return
	}

	rn.handleResponseLocked(packet, cycle, rn.txnMgr)
	rn.mu.Unlock()
}

// handleSnoopRequestLocked processes a Snoop request (must be called with mu locked).
func (rn *RequestNode) handleSnoopRequestLocked(snoopReq *core.Packet, cycle int) {
	if snoopReq == nil || rn.packetIDs == nil {
		return
	}

	GetLogger().Infof("[MESI] RequestNode %d: Received Snoop request for address 0x%x (TxnID=%d, Cycle=%d)",
		rn.ID, snoopReq.Address, snoopReq.TransactionID, cycle)

	var snoopResp *core.Packet
	if handler, ok := rn.cacheCapability.(capabilities.RequestCacheHandler); ok {
		allocator := func() (int64, error) {
			if rn.packetIDs == nil {
				return 0, fmt.Errorf("packet allocator not configured")
			}
			return rn.packetIDs.Allocate(), nil
		}
		resp, err := handler.BuildSnoopResponse(rn.ID, snoopReq, allocator, cycle)
		if err != nil {
			GetLogger().Warnf("RequestNode %d: failed to build snoop response: %v", rn.ID, err)
			return
		}
		snoopResp = resp
	} else {
		GetLogger().Warnf("RequestNode %d: cache capability missing, cannot respond to snoop", rn.ID)
		return
	}

	if snoopResp == nil {
		return
	}

	if snoopResp.ResponseType == core.CHIRespSnpData {
		GetLogger().Infof("[MESI] RequestNode %d: Cache provides data for address 0x%x (TxnID=%d)",
			rn.ID, snoopReq.Address, snoopReq.TransactionID)
	} else {
		GetLogger().Infof("[MESI] RequestNode %d: Cache invalid, responding with SnpNoData for address 0x%x",
			rn.ID, snoopReq.Address)
	}

	// Record snoop response generation
	if rn.txnMgr != nil && snoopReq.TransactionID > 0 {
		event := &core.PacketEvent{
			TransactionID:  snoopReq.TransactionID,
			PacketID:       snoopResp.ID,
			ParentPacketID: snoopReq.ID,
			NodeID:         rn.ID,
			EventType:      core.PacketGenerated,
			Cycle:          cycle,
			EdgeKey:        nil,
		}
		rn.txnMgr.RecordPacketEvent(event)
	}

	// Queue snoop response to be sent in next Tick
	if inQ := rn.inQueue(); inQ != nil {
		msg := &PipelineMessage{Packet: snoopResp, Kind: "snoop_response"}
		if _, ok := inQ.Enqueue(msg, cycle); !ok {
			GetLogger().Warnf("RequestNode %d: in_queue full, cannot enqueue snoop response %d", rn.ID, snoopResp.ID)
			return
		}
	} else {
		GetLogger().Warnf("RequestNode %d: pipeline not initialized, dropping snoop response %d", rn.ID, snoopResp.ID)
		return
	}
	GetLogger().Infof("[MESI] RequestNode %d: Generated Snoop response %s (PacketID=%d) for TxnID=%d, queued for sending",
		rn.ID, snoopResp.ResponseType, snoopResp.ID, snoopReq.TransactionID)
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

func (rn *RequestNode) stageQueue(stage pipelineStageName) *queue.StageQueue[*PipelineMessage] {
	if rn == nil || rn.pipeline == nil {
		return nil
	}
	return rn.pipeline.queue(stage)
}

func (rn *RequestNode) inQueue() *queue.StageQueue[*PipelineMessage] {
	return rn.stageQueue(pipelineStageIn)
}

func (rn *RequestNode) processQueue() *queue.StageQueue[*PipelineMessage] {
	return rn.stageQueue(pipelineStageProcess)
}

func (rn *RequestNode) outQueue() *queue.StageQueue[*PipelineMessage] {
	return rn.stageQueue(pipelineStageOut)
}

func (rn *RequestNode) generateRequests(cycle int, slaves []*SlaveNode) {
	if rn.generator == nil {
		return
	}
	results := rn.generator.ShouldGenerate(cycle, rn.masterIndex, len(slaves))
	inQ := rn.inQueue()
	for _, result := range results {
		if !result.ShouldGenerate {
			continue
		}

		address := result.Address
		if address == 0 {
			address = rn.nextAddress
			rn.nextAddress += DefaultCacheLineSize
		}

		dataSize := result.DataSize
		if dataSize == 0 {
			dataSize = DefaultCacheLineSize
		}

		txnType := result.TransactionType
		if txnType == "" {
			txnType = core.CHITxnReadNoSnp
		}

		if rn.cacheEvictor != nil {
			rn.cacheEvictor.Touch(address)
		}

		var targetSlave *SlaveNode
		if rn.addrMapper != nil {
			targetSlave = rn.addrMapper.TargetSlave(address)
		}
		if targetSlave == nil {
			if result.SlaveIndex < 0 || result.SlaveIndex >= len(slaves) {
				continue
			}
			targetSlave = slaves[result.SlaveIndex]
		}
		if targetSlave == nil {
			continue
		}

		params := capabilities.TxRequestParams{
			Cycle:           cycle,
			SrcID:           rn.ID,
			MasterID:        rn.ID,
			DstID:           targetSlave.ID,
			TransactionType: txnType,
			Address:         address,
			DataSize:        dataSize,
		}

		creator := rn.txnCreator
		if creator == nil {
			rn.ensureDefaultCapabilities()
			creator = rn.txnCreator
		}
		if creator == nil {
			GetLogger().Warnf("RequestNode %d transaction capability missing", rn.ID)
			continue
		}

		packet, _, err := creator(params)
		if err != nil {
			GetLogger().Warnf("RequestNode %d transaction capability failed: %v", rn.ID, err)
			continue
		}
		if packet == nil {
			continue
		}

		rn.TotalRequests++
		packet.SetMetadata(capabilities.RingFinalTargetMetadataKey, strconv.Itoa(targetSlave.ID))
		rn.generatedAtByReq[packet.RequestID] = cycle

		msg := &PipelineMessage{Packet: packet, Kind: "request"}
		if inQ == nil {
			continue
		}
		if _, ok := inQ.Enqueue(msg, cycle); !ok {
			GetLogger().Warnf("RequestNode %d in_queue full, dropping request %d", rn.ID, packet.RequestID)
		}
	}
}

func (rn *RequestNode) releaseToProcess(limit int, cycle int) {
	inQ := rn.inQueue()
	procQ := rn.processQueue()
	if inQ == nil || procQ == nil {
		return
	}
	if limit <= 0 {
		limit = 1
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

func (rn *RequestNode) processStage(limit int, cycle int, cfg *Config, homeNodeID int) {
	procQ := rn.processQueue()
	if procQ == nil {
		return
	}
	if limit <= 0 {
		limit = 1
	}
	outQ := rn.outQueue()
	processed := 0
	for processed < limit {
		entryID, msg, ok := procQ.PeekNext()
		if !ok {
			break
		}
		if msg == nil || msg.Packet == nil {
			procQ.Complete(entryID, cycle)
			processed++
			continue
		}

		packet := msg.Packet
		rn.emitProcessHook(packet, cycle, true)
		targetID := homeNodeID
		if cfg != nil && cfg.RingEnabled && rn.routerID != 0 {
			targetID = rn.routerID
		}
		latency := cfg.MasterRelayLatency

		switch msg.Kind {
		case "request":
			msg.Kind = "request_prepared"
			msg.DefaultTarget = targetID
		case "snoop_response":
			msg.Kind = "snoop_response_prepared"
			msg.DefaultTarget = targetID
		default:
			GetLogger().Warnf("RequestNode %d: unsupported pipeline message kind %q", rn.ID, msg.Kind)
			procQ.Complete(entryID, cycle)
			rn.emitProcessHook(packet, cycle, false)
			processed++
			continue
		}

		msg.TargetID = targetID
		msg.Latency = latency

		procQ.Complete(entryID, cycle)

		if outQ != nil {
			outEntry, ok := outQ.Enqueue(msg, cycle)
			if !ok {
				GetLogger().Warnf("RequestNode %d: out_queue full, dropping packet %d", rn.ID, packet.ID)
				processed++
				continue
			}
			if rn.outInFlightBlock != queue.InvalidBlockIndex {
				_ = outQ.SetBlocked(outEntry, rn.outInFlightBlock, false, cycle)
			}
			if msg.Kind == "request_prepared" {
				rn.dispatchByRequest[packet.RequestID] = outEntry
			}
		}
		rn.emitProcessHook(packet, cycle, false)
		processed++
	}
}

func (rn *RequestNode) sendFromOutQueue(limit int, cycle int, cfg *Config, ch *Link, homeNodeID int) {
	outQ := rn.outQueue()
	if outQ == nil {
		return
	}
	if limit <= 0 {
		limit = 1
	}
	sent := 0
	for sent < limit {
		entryID, msg, ok := outQ.PeekNext()
		if !ok {
			break
		}
		if msg == nil || msg.Packet == nil {
			outQ.Complete(entryID, cycle)
			continue
		}

		packet := msg.Packet
		defaultTarget := msg.TargetID
		if defaultTarget <= 0 {
			defaultTarget = msg.DefaultTarget
		}
		if defaultTarget <= 0 {
			targetRouter := homeNodeID
			if cfg != nil && cfg.RingEnabled && rn.routerID != 0 {
				targetRouter = rn.routerID
			}
			defaultTarget = targetRouter
		}
		latency := msg.Latency
		if latency <= 0 {
			latency = cfg.MasterRelayLatency
		}

		switch msg.Kind {
		case "request_prepared":
			finalTarget, ok := rn.resolveRoute(packet, defaultTarget)
			if !ok {
				outQ.ResetPending(entryID)
				continue
			}
			msg.TargetID = finalTarget
			if !rn.emitBeforeSend(packet, finalTarget, cycle) {
				outQ.ResetPending(entryID)
				continue
			}
			ch.Send(packet, rn.ID, finalTarget, cycle, latency)
			packet.SentAt = cycle
			if rn.txnMgr != nil && packet.TransactionID > 0 {
				rn.txnMgr.MarkTransactionInFlight(packet.TransactionID, cycle)
			}
			rn.emitAfterSend(packet, finalTarget, cycle)
			rn.dispatchByRequest[packet.RequestID] = entryID
			msg.Kind = "request_inflight"
			if rn.outInFlightBlock != queue.InvalidBlockIndex {
				_ = outQ.SetBlocked(entryID, rn.outInFlightBlock, true, cycle)
			}
		case "snoop_response_prepared":
			msg.TargetID = defaultTarget
			if !rn.emitBeforeSend(packet, defaultTarget, cycle) {
				outQ.ResetPending(entryID)
				continue
			}
			ch.Send(packet, rn.ID, defaultTarget, cycle, latency)
			packet.SentAt = cycle
			rn.emitAfterSend(packet, defaultTarget, cycle)
			outQ.Complete(entryID, cycle)
		case "request_inflight":
			outQ.ResetPending(entryID)
			sent++
			continue
		default:
			GetLogger().Warnf("RequestNode %d: unexpected out_queue message kind %q", rn.ID, msg.Kind)
			outQ.Complete(entryID, cycle)
		}

		sent++
	}
}

func (rn *RequestNode) resolveRoute(packet *core.Packet, defaultTarget int) (int, bool) {
	targetID := defaultTarget
	if rn.broker == nil || packet == nil {
		return targetID, true
	}
	beforeCtx := &hooks.RouteContext{
		Packet:        packet,
		SourceNodeID:  rn.ID,
		DefaultTarget: defaultTarget,
		TargetID:      defaultTarget,
	}
	if err := rn.broker.EmitBeforeRoute(beforeCtx); err != nil {
		GetLogger().Warnf("RequestNode %d OnBeforeRoute hook failed: %v", rn.ID, err)
		return 0, false
	}
	targetID = beforeCtx.TargetID
	afterCtx := &hooks.RouteContext{
		Packet:        packet,
		SourceNodeID:  rn.ID,
		DefaultTarget: defaultTarget,
		TargetID:      targetID,
	}
	if err := rn.broker.EmitAfterRoute(afterCtx); err != nil {
		GetLogger().Warnf("RequestNode %d OnAfterRoute hook failed: %v", rn.ID, err)
	}
	if afterCtx.TargetID > 0 {
		targetID = afterCtx.TargetID
	}
	if targetID <= 0 {
		targetID = defaultTarget
	}
	return targetID, true
}

func (rn *RequestNode) emitBeforeSend(packet *core.Packet, targetID int, cycle int) bool {
	if rn.broker == nil || packet == nil {
		return true
	}
	ctx := &hooks.MessageContext{
		Packet:   packet,
		NodeID:   rn.ID,
		TargetID: targetID,
		Cycle:    cycle,
	}
	if err := rn.broker.EmitBeforeSend(ctx); err != nil {
		GetLogger().Warnf("RequestNode %d OnBeforeSend hook failed: %v", rn.ID, err)
		return false
	}
	return true
}

func (rn *RequestNode) emitAfterSend(packet *core.Packet, targetID int, cycle int) {
	if rn.broker == nil || packet == nil {
		return
	}
	ctx := &hooks.MessageContext{
		Packet:   packet,
		NodeID:   rn.ID,
		TargetID: targetID,
		Cycle:    cycle,
	}
	if err := rn.broker.EmitAfterSend(ctx); err != nil {
		GetLogger().Warnf("RequestNode %d OnAfterSend hook failed: %v", rn.ID, err)
	}
}

func (rn *RequestNode) emitProcessHook(packet *core.Packet, cycle int, before bool) {
	if rn.broker == nil || packet == nil {
		return
	}
	ctx := &hooks.ProcessContext{
		Packet: packet,
		Node:   rn,
		NodeID: rn.ID,
		Cycle:  cycle,
	}
	var err error
	if before {
		err = rn.broker.EmitBeforeProcess(ctx)
	} else {
		err = rn.broker.EmitAfterProcess(ctx)
	}
	if err != nil {
		GetLogger().Warnf("RequestNode %d process hook failed: %v", rn.ID, err)
	}
}

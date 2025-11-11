package main

import (
	"fmt"
	"sync"

	"flow_sim/core"
	"flow_sim/hooks"
	"flow_sim/policy"
	"flow_sim/queue"
)

// CacheLine represents a single cache line in the HomeNode cache.
type CacheLine struct {
	Address uint64
	Valid   bool
}

// HomeNode (HN) represents a CHI Home Node that manages cache coherence and routes transactions.
// It receives requests from Request Nodes and forwards them to Slave Nodes,
// then routes responses back to the originating Request Node.
type HomeNode struct {
	Node   // embedded Node base class
	queue  *queue.TrackedQueue[*core.Packet]
	txnMgr *TransactionManager // for recording packet events

	mu       sync.Mutex
	bindings *NodeCycleBindings

	broker    *hooks.PluginBroker
	policyMgr policy.Manager

	// Cache storage: simple map from address to cache line
	cache   map[uint64]*CacheLine
	cacheMu sync.RWMutex

	// Directory: tracks which RequestNodes have cached which addresses
	// map[address] -> set of RequestNode IDs
	directory   map[uint64]map[int]bool
	directoryMu sync.RWMutex

	// Pending requests: tracks ReadOnce requests waiting for Snoop responses
	// map[transactionID] -> pending request info
	pendingRequests   map[int64]*PendingSnoopRequest
	pendingRequestsMu sync.RWMutex

	// PacketIDAllocator for generating response packet IDs
	packetIDs *PacketIDAllocator
}

func NewHomeNode(id int) *HomeNode {
	hn := &HomeNode{
		Node: Node{
			ID:   id,
			Type: core.NodeTypeHN,
		},
		txnMgr:   nil, // will be set by simulator
		bindings: NewNodeCycleBindings(),
		cache:           make(map[uint64]*CacheLine),
		directory:       make(map[uint64]map[int]bool),
		pendingRequests: make(map[int64]*PendingSnoopRequest),
		packetIDs:       nil, // will be set by simulator
	}
	hn.AddQueue("forward_queue", 0, DefaultForwardQueueCapacity)
	hn.queue = queue.NewTrackedQueue("forward_queue", DefaultForwardQueueCapacity, hn.makeQueueMutator(), queue.QueueHooks[*core.Packet]{
		OnEnqueue: hn.enqueueHook,
		OnDequeue: hn.dequeueHook,
	})
	return hn
}

func (hn *HomeNode) makeQueueMutator() queue.MutateFunc {
	return func(length int, capacity int) {
		hn.UpdateQueueState("forward_queue", length, capacity)
	}
}

func (hn *HomeNode) enqueueHook(p *core.Packet, cycle int) {
	if hn.txnMgr == nil || p == nil || p.TransactionID == 0 {
		return
	}
	event := &core.PacketEvent{
		TransactionID:  p.TransactionID,
		PacketID:       p.ID,
		ParentPacketID: p.ParentPacketID,
		NodeID:         hn.ID,
		EventType:      core.PacketEnqueued,
		Cycle:          cycle,
		EdgeKey:        nil,
	}
	hn.txnMgr.RecordPacketEvent(event)
}

func (hn *HomeNode) dequeueHook(p *core.Packet, cycle int) {
	if hn.txnMgr == nil || p == nil || p.TransactionID == 0 {
		return
	}
	event := &core.PacketEvent{
		TransactionID:  p.TransactionID,
		PacketID:       p.ID,
		ParentPacketID: p.ParentPacketID,
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
}

// SetPolicyManager assigns the policy manager used during routing decisions.
func (hn *HomeNode) SetPolicyManager(m policy.Manager) {
	hn.policyMgr = m
}

// SetPacketIDAllocator assigns the packet ID allocator for generating response packets.
func (hn *HomeNode) SetPacketIDAllocator(allocator *PacketIDAllocator) {
	hn.packetIDs = allocator
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
// HomeNode always can receive (unlimited capacity for forward_queue).
func (hn *HomeNode) CanReceive(edgeKey core.EdgeKey, packetCount int) bool {
	// forward_queue has unlimited capacity (-1)
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
	hn.queue.Enqueue(p, cycle)
	hn.mu.Unlock()
}

// checkCache checks if the cache contains data for the given address.
// Returns true if cache hit, false if cache miss.
func (hn *HomeNode) checkCache(addr uint64) bool {
	hn.cacheMu.RLock()
	defer hn.cacheMu.RUnlock()
	line, exists := hn.cache[addr]
	return exists && line != nil && line.Valid
}

// updateCache updates the cache with data for the given address.
func (hn *HomeNode) updateCache(addr uint64) {
	hn.cacheMu.Lock()
	defer hn.cacheMu.Unlock()
	hn.cache[addr] = &CacheLine{
		Address: addr,
		Valid:   true,
	}
}

// directoryAdd adds a RequestNode to the directory for the given address.
func (hn *HomeNode) directoryAdd(addr uint64, rnID int) {
	hn.directoryMu.Lock()
	defer hn.directoryMu.Unlock()
	if hn.directory[addr] == nil {
		hn.directory[addr] = make(map[int]bool)
	}
	hn.directory[addr][rnID] = true
}

// directoryRemove removes a RequestNode from the directory for the given address.
func (hn *HomeNode) directoryRemove(addr uint64, rnID int) {
	hn.directoryMu.Lock()
	defer hn.directoryMu.Unlock()
	if hn.directory[addr] != nil {
		delete(hn.directory[addr], rnID)
		if len(hn.directory[addr]) == 0 {
			delete(hn.directory, addr)
		}
	}
}

// PendingSnoopRequest tracks a ReadOnce request waiting for Snoop responses
type PendingSnoopRequest struct {
	RequestingRNID  int   // The RN that sent the original ReadOnce request
	OriginalReqID   int64 // The RequestID of the original ReadOnce request
	TotalSnoops     int   // Total number of Snoop requests sent
	ReceivedSnoops  int   // Number of Snoop responses received
	AllNoData       bool  // True if all received responses are SnpNoData
}

// directoryGetSharers returns the list of RequestNode IDs that have cached the given address.
func (hn *HomeNode) directoryGetSharers(addr uint64) []int {
	hn.directoryMu.RLock()
	defer hn.directoryMu.RUnlock()
	sharers, exists := hn.directory[addr]
	if !exists || len(sharers) == 0 {
		return nil
	}
	result := make([]int, 0, len(sharers))
	for rnID := range sharers {
		result = append(result, rnID)
	}
	return result
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
		SentAt:          cycle,
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
		SentAt:          cycle,
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

// Tick processes the queue and forwards CHI packets according to CHI protocol rules.
// For ReadOnce transactions:
//   - If cache hit: generate CompData response directly to RN (Alternative 1)
//   - If cache miss: forward request to SN, then update cache when response arrives
// For ReadNoSnp transactions:
//   - Requests from RN: forward to target SN
//   - CompData responses from SN: forward back to originating RN
func (hn *HomeNode) Tick(cycle int, ch *Link, cfg *Config) int {
	hn.mu.Lock()
	defer hn.mu.Unlock()

	if hn.queue.Len() == 0 {
		return 0
	}

	count := 0
	for hn.queue.Len() > 0 {
		p, ok := hn.queue.PopFront(cycle)
		if !ok {
			break
		}

		// Handle ReadOnce requests with Directory lookup and Snoop
		if p.MessageType == core.CHIMsgReq && p.TransactionType == core.CHITxnReadOnce {
			GetLogger().Infof("[MESI] HomeNode %d: Received ReadOnce request from RN %d (TxnID=%d, Addr=0x%x, Cycle=%d)", 
				hn.ID, p.MasterID, p.TransactionID, p.Address, cycle)
			
			// Check Directory for other sharers
			sharers := hn.directoryGetSharers(p.Address)
			GetLogger().Infof("[MESI] HomeNode %d: Directory lookup for address 0x%x: found %d sharer(s): %v", 
				hn.ID, p.Address, len(sharers), sharers)
			
			// Exclude the requesting RN from sharers list
			filteredSharers := make([]int, 0)
			for _, rnID := range sharers {
				if rnID != p.MasterID {
					filteredSharers = append(filteredSharers, rnID)
				}
			}
			GetLogger().Infof("[MESI] HomeNode %d: After filtering (excluding RN %d): %d sharer(s) to snoop: %v", 
				hn.ID, p.MasterID, len(filteredSharers), filteredSharers)
			
			if len(filteredSharers) > 0 {
				// Other RNs have cached this address: send Snoop requests
				GetLogger().Infof("[MESI] HomeNode %d: Found %d sharer(s) for address 0x%x, sending Snoop requests", 
					hn.ID, len(filteredSharers), p.Address)
				
				// Store the original request to track which RN is waiting for Snoop responses
				hn.pendingRequestsMu.Lock()
				hn.pendingRequests[p.TransactionID] = &PendingSnoopRequest{
					RequestingRNID: p.MasterID,
					OriginalReqID:  p.RequestID,
					TotalSnoops:    len(filteredSharers),
					ReceivedSnoops: 0,
					AllNoData:      true, // Assume all are NoData until we get SnpData
				}
				hn.pendingRequestsMu.Unlock()
				
				// Send Snoop requests to all sharers
				for _, sharerID := range filteredSharers {
					snoopReq := hn.generateSnoopRequest(p.Address, sharerID, p.TransactionID, p.TransactionType, cycle)
					if snoopReq != nil {
						// Record snoop request generation
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
						
						// Send Snoop request to sharer RN
						snoopReq.SentAt = cycle
						ch.Send(snoopReq, hn.ID, sharerID, cycle, cfg.RelayMasterLatency)
						count++
						GetLogger().Infof("[MESI] HomeNode %d: Sent Snoop request (PacketID=%d) to RN %d for address 0x%x", 
							hn.ID, snoopReq.ID, sharerID, p.Address)
					}
				}
				
				// Wait for Snoop responses - do NOT forward to SN yet
				// The request will be handled when Snoop response arrives
				GetLogger().Infof("[MESI] HomeNode %d: Waiting for Snoop responses, NOT forwarding to SN", 
					hn.ID)
				continue // Skip forwarding to SN
			} else {
				// No other sharers: check HN cache
				if hn.checkCache(p.Address) {
					// Cache hit: generate CompData response directly
					GetLogger().Infof("[Cache] HomeNode %d: CACHE HIT for address 0x%x (TxnID=%d, Cycle=%d)", 
						hn.ID, p.Address, p.TransactionID, cycle)
					resp := hn.generateCacheHitResponse(p, cycle)
					if resp != nil {
						// Record cache hit event
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

						// Send response directly to RN
						latency := cfg.RelayMasterLatency
						resp.SentAt = cycle
						GetLogger().Infof("[Cache] HomeNode %d: Sending CompData response directly to RN %d (TxnID=%d, PacketID=%d, Cycle=%d)", 
							hn.ID, resp.DstID, p.TransactionID, resp.ID, cycle)
						ch.Send(resp, hn.ID, resp.DstID, cycle, latency)
						count++

						// Update Directory: add requesting RN as sharer
						hn.directoryAdd(p.Address, p.MasterID)

						// Record cache hit in transaction metadata
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
						continue
					}
				} else {
					// Cache miss: record and continue with normal forwarding to SN
					GetLogger().Infof("[Cache] HomeNode %d: CACHE MISS for address 0x%x (TxnID=%d, Cycle=%d) - forwarding to SN", 
						hn.ID, p.Address, p.TransactionID, cycle)
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
			}
		}

		// Handle Snoop responses from RNs
		if p.MessageType == core.CHIMsgSnpResp {
			GetLogger().Infof("[MESI] HomeNode %d: Received Snoop response %s from RN %d for address 0x%x (TxnID=%d, Cycle=%d)", 
				hn.ID, p.ResponseType, p.SrcID, p.Address, p.TransactionID, cycle)
			
			// Find the original ReadOnce request that triggered this Snoop
			hn.pendingRequestsMu.Lock()
			pendingReq, hasPending := hn.pendingRequests[p.TransactionID]
			if !hasPending {
				hn.pendingRequestsMu.Unlock()
				GetLogger().Warnf("[MESI] HomeNode %d: Received Snoop response for unknown TxnID=%d", hn.ID, p.TransactionID)
			} else {
				pendingReq.ReceivedSnoops++
				
				if p.ResponseType == core.CHIRespSnpData {
					// Snoop response with data: forward as CompData to the original requesting RN
					requestingRNID := pendingReq.RequestingRNID
					hn.pendingRequestsMu.Unlock()
					
					resp := &core.Packet{
						ID:              hn.packetIDs.Allocate(),
						Type:            "response", // legacy
						SrcID:           hn.ID,
						DstID:           requestingRNID,
						GeneratedAt:     cycle,
						SentAt:          cycle,
						MasterID:        requestingRNID,
						RequestID:       pendingReq.OriginalReqID, // Use original ReadOnce request ID
						TransactionType: p.TransactionType,
						MessageType:     core.CHIMsgComp,
						ResponseType:    core.CHIRespCompData,
						Address:         p.Address,
						DataSize:        p.DataSize,
						TransactionID:   p.TransactionID,
						ParentPacketID:  p.ID,
					}
					
					// Record response generation
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
					
					// Send CompData to requesting RN
					ch.Send(resp, hn.ID, requestingRNID, cycle, cfg.RelayMasterLatency)
					count++
					
					// Update Directory: add requesting RN as sharer
					hn.directoryAdd(p.Address, requestingRNID)
					
					// Remove from pending requests
					hn.pendingRequestsMu.Lock()
					delete(hn.pendingRequests, p.TransactionID)
					hn.pendingRequestsMu.Unlock()
					
					GetLogger().Infof("[MESI] HomeNode %d: Forwarded Snoop data as CompData to RN %d (TxnID=%d, PacketID=%d)", 
						hn.ID, requestingRNID, p.TransactionID, resp.ID)
					continue
				} else if p.ResponseType == core.CHIRespSnpNoData {
					// Snoop response without data: mark as NoData
					pendingReq.AllNoData = pendingReq.AllNoData && true // stays true
					GetLogger().Infof("[MESI] HomeNode %d: Snoop response has no data (%d/%d received)", 
						hn.ID, pendingReq.ReceivedSnoops, pendingReq.TotalSnoops)
					
					// Check if all Snoop responses have been received
					if pendingReq.ReceivedSnoops >= pendingReq.TotalSnoops {
						// All Snoop responses received, all are NoData: need to forward to SN
						// For now, we'll log this case - in a full implementation, we'd store the original
						// request packet and forward it to SN here
						GetLogger().Infof("[MESI] HomeNode %d: All Snoop responses are NoData (%d/%d), should forward to SN (not implemented yet)", 
							hn.ID, pendingReq.ReceivedSnoops, pendingReq.TotalSnoops)
						// Remove from pending - transaction will timeout or be handled elsewhere
						delete(hn.pendingRequests, p.TransactionID)
						hn.pendingRequestsMu.Unlock()
					} else {
						// Still waiting for more Snoop responses
						hn.pendingRequestsMu.Unlock()
					}
				}
			}
		}

		// Handle CompData responses from SN: update cache and Directory for ReadOnce transactions
		if p.MessageType == core.CHIMsgComp && p.ResponseType == core.CHIRespCompData {
			if p.TransactionType == core.CHITxnReadOnce {
				// Get the requesting RN ID from pending requests or use MasterID
				var requestingRNID int
				hn.pendingRequestsMu.Lock()
				if pendingReq, hasPending := hn.pendingRequests[p.TransactionID]; hasPending {
					requestingRNID = pendingReq.RequestingRNID
					// Remove from pending - we got data from SN (Snoop didn't provide data or we got SN data first)
					delete(hn.pendingRequests, p.TransactionID)
					GetLogger().Infof("[MESI] HomeNode %d: Removed TxnID=%d from pending requests (got data from SN), requesting RN=%d", 
						hn.ID, p.TransactionID, requestingRNID)
				} else {
					// Not in pending requests, use MasterID (should be the original RN)
					requestingRNID = p.MasterID
					GetLogger().Infof("[MESI] HomeNode %d: TxnID=%d not in pending requests, using MasterID=%d", 
						hn.ID, p.TransactionID, requestingRNID)
				}
				hn.pendingRequestsMu.Unlock()
				
				// Update cache with the data
				GetLogger().Infof("[Cache] HomeNode %d: Received CompData from SN, updating cache for address 0x%x (TxnID=%d, Cycle=%d, RequestingRN=%d)", 
					hn.ID, p.Address, p.TransactionID, cycle, requestingRNID)
				hn.updateCache(p.Address)
				
				// Update Directory: add the requesting RN as sharer
				hn.directoryAdd(p.Address, requestingRNID)
				GetLogger().Infof("[MESI] HomeNode %d: Added RN %d to Directory for address 0x%x", 
					hn.ID, requestingRNID, p.Address)
				
				GetLogger().Infof("[Cache] HomeNode %d: Cache and Directory updated for address 0x%x (TxnID=%d)", 
					hn.ID, p.Address, p.TransactionID)
				// Record cache update event
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
		}

		defaultTarget, latency, ok := hn.defaultRoute(p, cfg)
		if !ok {
			continue
		}

		finalTarget := defaultTarget

		if hn.broker != nil {
			routeCtx := &hooks.RouteContext{
				Packet:        p,
				SourceNodeID:  hn.ID,
				DefaultTarget: defaultTarget,
				TargetID:      defaultTarget,
			}
			if err := hn.broker.EmitBeforeRoute(routeCtx); err != nil {
				GetLogger().Warnf("HomeNode %d OnBeforeRoute hook failed: %v", hn.ID, err)
				continue
			}
			finalTarget = routeCtx.TargetID
		}

		if hn.policyMgr != nil {
			resolvedTarget, err := hn.policyMgr.ResolveRoute(p, hn.ID, finalTarget)
			if err != nil {
				GetLogger().Warnf("HomeNode %d policy route failed: %v", hn.ID, err)
				continue
			}
			finalTarget = resolvedTarget
		}

		if hn.broker != nil {
			routeCtx := &hooks.RouteContext{
				Packet:        p,
				SourceNodeID:  hn.ID,
				DefaultTarget: defaultTarget,
				TargetID:      finalTarget,
			}
			if err := hn.broker.EmitAfterRoute(routeCtx); err != nil {
				GetLogger().Warnf("HomeNode %d OnAfterRoute hook failed: %v", hn.ID, err)
			}
			finalTarget = routeCtx.TargetID
		}

		if hn.broker != nil {
			sendCtx := &hooks.MessageContext{
				Packet: p,
				NodeID: hn.ID,
				Cycle:  cycle,
			}
			if err := hn.broker.EmitBeforeSend(sendCtx); err != nil {
				GetLogger().Warnf("HomeNode %d OnBeforeSend hook failed: %v", hn.ID, err)
				continue
			}
		}

		if hn.policyMgr != nil {
			if err := hn.policyMgr.CheckFlowControl(p, hn.ID, finalTarget); err != nil {
				GetLogger().Warnf("HomeNode %d flow control blocked packet: %v", hn.ID, err)
				continue
			}
		}

		latency = hn.adjustLatency(p, cfg, finalTarget)

		p.SentAt = cycle
		ch.Send(p, hn.ID, finalTarget, cycle, latency)
		count++

		if hn.broker != nil {
			sendCtx := &hooks.MessageContext{
				Packet: p,
				NodeID: hn.ID,
				Cycle:  cycle,
			}
			if err := hn.broker.EmitAfterSend(sendCtx); err != nil {
				GetLogger().Warnf("HomeNode %d OnAfterSend hook failed: %v", hn.ID, err)
			}
		}
	}

	return count
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

// GetQueuePackets returns packet information for the forward_queue
func (hn *HomeNode) GetQueuePackets() []core.PacketInfo {
	hn.mu.Lock()
	defer hn.mu.Unlock()
	packets := make([]core.PacketInfo, 0, hn.queue.Len())
	for _, p := range hn.queue.Items() {
		if p == nil {
			continue
		}
		packets = append(packets, core.PacketInfo{
			ID:              p.ID,
			Type:            p.Type,
			SrcID:           p.SrcID,
			DstID:           p.DstID,
			GeneratedAt:     p.GeneratedAt,
			SentAt:          p.SentAt,
			ReceivedAt:      p.ReceivedAt,
			CompletedAt:     p.CompletedAt,
			MasterID:        p.MasterID,
			RequestID:       p.RequestID,
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
		event := &PacketEvent{
			TransactionID:  packet.TransactionID,
			PacketID:       packet.ID,
			ParentPacketID: packet.ParentPacketID,
			NodeID:         hn.ID,
			EventType:      PacketReceived,
			Cycle:          cycle,
			EdgeKey:        nil, // in-node event
		}
		hn.txnMgr.RecordPacketEvent(event)
	}

	hn.queue.Enqueue(packet, cycle)
}

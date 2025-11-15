package chi

import (
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/capabilities"
	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/hooks"
)

// HomeCapability encapsulates CHI home-node behaviour (directory, snoop fan-out, cache hit handling).
type HomeCapability interface {
	capabilities.NodeCapability
	HandlePacket(packet *core.Packet, cycle int) (forward bool, outgoing []OutgoingPacket)
}

// HomeConfig wires dependencies required by HomeCapability.
type HomeConfig struct {
	Name             string
	NodeID           int
	Cache            capabilities.HomeCache
	Directory        capabilities.DirectoryStore
	CacheEvictor     capabilities.CacheEvictor
	PacketAllocator  capabilities.PacketAllocator
	Recorder         PacketRecorder
	MetadataRecorder MetadataRecorder
	SetFinalTarget   func(packet *core.Packet, target int)
}

type homeCapability struct {
	name             string
	nodeID           int
	cache            capabilities.HomeCache
	directory        capabilities.DirectoryStore
	cacheEvictor     capabilities.CacheEvictor
	packetAllocator  capabilities.PacketAllocator
	recorder         PacketRecorder
	metadataRecorder MetadataRecorder
	setFinalTarget   func(packet *core.Packet, target int)

	mu      sync.Mutex
	pending map[int64]*pendingSnoop
}

type pendingSnoop struct {
	requestingRNID int
	originalReqID  int64
	totalSnoops    int
	receivedSnoops int
	allNoData      bool
}

// NewHomeCapability constructs a CHI home-node capability.
func NewHomeCapability(cfg HomeConfig) (HomeCapability, error) {
	if cfg.Cache == nil {
		return nil, fmt.Errorf("home cache is required")
	}
	if cfg.Directory == nil {
		return nil, fmt.Errorf("directory store is required")
	}
	if cfg.PacketAllocator == nil {
		return nil, fmt.Errorf("packet allocator is required")
	}
	if cfg.SetFinalTarget == nil {
		return nil, fmt.Errorf("final target setter is required")
	}
	name := cfg.Name
	if name == "" {
		name = "chi-home"
	}
	return &homeCapability{
		name:             name,
		nodeID:           cfg.NodeID,
		cache:            cfg.Cache,
		directory:        cfg.Directory,
		cacheEvictor:     cfg.CacheEvictor,
		packetAllocator:  cfg.PacketAllocator,
		recorder:         cfg.Recorder,
		metadataRecorder: cfg.MetadataRecorder,
		setFinalTarget:   cfg.SetFinalTarget,
		pending:          make(map[int64]*pendingSnoop),
	}, nil
}

func (c *homeCapability) Descriptor() hooks.PluginDescriptor {
	return hooks.PluginDescriptor{
		Name:        c.name,
		Category:    hooks.PluginCategoryCapability,
		Description: "CHI MESI home capability",
	}
}

func (c *homeCapability) Register(broker *hooks.PluginBroker) error {
	if broker == nil {
		return nil
	}
	broker.RegisterPluginMetadata(c.Descriptor())
	return nil
}

func (c *homeCapability) HandlePacket(p *core.Packet, cycle int) (bool, []OutgoingPacket) {
	if p == nil {
		return true, nil
	}
	switch {
	case p.MessageType == core.CHIMsgReq && p.TransactionType == core.CHITxnReadOnce:
		return c.handleReadOnce(p, cycle)
	case p.MessageType == core.CHIMsgSnpResp:
		return c.handleSnoopResponse(p, cycle)
	case p.MessageType == core.CHIMsgComp && p.ResponseType == core.CHIRespCompData:
		c.handleCompDataResponse(p, cycle)
		return true, nil
	default:
		return true, nil
	}
}

func (c *homeCapability) handleReadOnce(p *core.Packet, cycle int) (bool, []OutgoingPacket) {
	outgoing := []OutgoingPacket{}

	sharers := c.directory.Sharers(p.Address)
	filtered := make([]int, 0, len(sharers))
	for _, rnID := range sharers {
		if rnID != p.MasterID {
			filtered = append(filtered, rnID)
		}
	}

	if len(filtered) > 0 {
		entry := &pendingSnoop{
			requestingRNID: p.MasterID,
			originalReqID:  p.RequestID,
			totalSnoops:    len(filtered),
			allNoData:      true,
		}
		c.mu.Lock()
		c.pending[p.TransactionID] = entry
		c.mu.Unlock()
		for _, target := range filtered {
			if snoop := c.buildSnoopRequest(p, target, cycle); snoop != nil {
				outgoing = append(outgoing, OutgoingPacket{
					Packet:      snoop,
					TargetID:    target,
					Kind:        "snoop_request",
					LatencyHint: LatencyToMaster,
				})
			}
		}
		return false, outgoing
	}

	if c.cacheHit(p.Address) {
		if resp := c.buildCacheHitResponse(p, cycle); resp != nil {
			outgoing = append(outgoing, OutgoingPacket{
				Packet:      resp,
				TargetID:    resp.DstID,
				Kind:        "forward_response",
				LatencyHint: LatencyToMaster,
			})
		}
		c.directory.Add(p.Address, p.MasterID)
		c.recordMetadata(p.TransactionID, "cache_hit", "true")
		c.recordMetadata(p.TransactionID, "cache_hit_node", fmt.Sprintf("%d", c.nodeID))
		return false, outgoing
	}

	c.recordMetadata(p.TransactionID, "cache_miss", "true")
	c.recordMetadata(p.TransactionID, "cache_miss_node", fmt.Sprintf("%d", c.nodeID))
	return true, nil
}

func (c *homeCapability) handleSnoopResponse(p *core.Packet, cycle int) (bool, []OutgoingPacket) {
	c.mu.Lock()
	entry, ok := c.pending[p.TransactionID]
	if !ok {
		c.mu.Unlock()
		return false, nil
	}
	entry.receivedSnoops++
	defer c.mu.Unlock()

	if p.ResponseType == core.CHIRespSnpData {
		delete(c.pending, p.TransactionID)
		resp := c.buildCompDataFromSnoop(p, entry, cycle)
		if resp == nil {
			return false, nil
		}
		return false, []OutgoingPacket{
			{
				Packet:      resp,
				TargetID:    resp.DstID,
				Kind:        "forward_response",
				LatencyHint: LatencyToMaster,
			},
		}
	}

	if entry.receivedSnoops >= entry.totalSnoops {
		delete(c.pending, p.TransactionID)
	}
	return false, nil
}

func (c *homeCapability) handleCompDataResponse(p *core.Packet, cycle int) {
	requestingRNID := p.MasterID
	c.mu.Lock()
	if entry, ok := c.pending[p.TransactionID]; ok {
		requestingRNID = entry.requestingRNID
		delete(c.pending, p.TransactionID)
	}
	c.mu.Unlock()

	if c.cache != nil {
		c.cache.UpdateLine(p.Address, capabilities.HomeCacheLine{Address: p.Address, Valid: true})
	}
	if c.cacheEvictor != nil {
		c.cacheEvictor.Fill(p.Address)
	}
	if c.directory != nil {
		c.directory.Add(p.Address, requestingRNID)
	}
	c.recordMetadata(p.TransactionID, "cache_updated", "true")
	c.recordMetadata(p.TransactionID, "cache_updated_node", fmt.Sprintf("%d", c.nodeID))
}

func (c *homeCapability) cacheHit(addr uint64) bool {
	if c.cache == nil {
		return false
	}
	line, ok := c.cache.GetLine(addr)
	return ok && line.Valid
}

func (c *homeCapability) buildSnoopRequest(req *core.Packet, target int, cycle int) *core.Packet {
	id, err := c.packetAllocator()
	if err != nil {
		return nil
	}
	packet := &core.Packet{
		ID:              id,
		Type:            "request",
		SrcID:           c.nodeID,
		DstID:           target,
		GeneratedAt:     cycle,
		MasterID:        target,
		RequestID:       id,
		TransactionType: req.TransactionType,
		MessageType:     core.CHIMsgSnp,
		Address:         req.Address,
		DataSize:        req.DataSize,
		TransactionID:   req.TransactionID,
		SnoopTargetID:   target,
	}
	c.setFinalTarget(packet, target)
	c.recordPacketEvent(&core.PacketEvent{
		TransactionID:  req.TransactionID,
		PacketID:       packet.ID,
		ParentPacketID: req.ID,
		NodeID:         c.nodeID,
		EventType:      core.PacketGenerated,
		Cycle:          cycle,
	})
	return packet
}

func (c *homeCapability) buildCacheHitResponse(req *core.Packet, cycle int) *core.Packet {
	id, err := c.packetAllocator()
	if err != nil {
		return nil
	}
	resp := &core.Packet{
		ID:              id,
		Type:            "response",
		SrcID:           c.nodeID,
		DstID:           req.MasterID,
		GeneratedAt:     cycle,
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
	c.setFinalTarget(resp, req.MasterID)
	c.recordPacketEvent(&core.PacketEvent{
		TransactionID:  req.TransactionID,
		PacketID:       resp.ID,
		ParentPacketID: req.ID,
		NodeID:         c.nodeID,
		EventType:      core.PacketGenerated,
		Cycle:          cycle,
	})
	if c.cacheEvictor != nil {
		c.cacheEvictor.Touch(req.Address)
	}
	return resp
}

func (c *homeCapability) buildCompDataFromSnoop(snoopResp *core.Packet, entry *pendingSnoop, cycle int) *core.Packet {
	id, err := c.packetAllocator()
	if err != nil {
		return nil
	}
	resp := &core.Packet{
		ID:              id,
		Type:            "response",
		SrcID:           c.nodeID,
		DstID:           entry.requestingRNID,
		GeneratedAt:     cycle,
		MasterID:        entry.requestingRNID,
		RequestID:       entry.originalReqID,
		TransactionType: snoopResp.TransactionType,
		MessageType:     core.CHIMsgComp,
		ResponseType:    core.CHIRespCompData,
		Address:         snoopResp.Address,
		DataSize:        snoopResp.DataSize,
		TransactionID:   snoopResp.TransactionID,
		ParentPacketID:  snoopResp.ID,
	}
	c.setFinalTarget(resp, resp.DstID)
	c.recordPacketEvent(&core.PacketEvent{
		TransactionID:  snoopResp.TransactionID,
		PacketID:       resp.ID,
		ParentPacketID: snoopResp.ID,
		NodeID:         c.nodeID,
		EventType:      core.PacketGenerated,
		Cycle:          cycle,
	})
	if c.directory != nil {
		c.directory.Add(snoopResp.Address, entry.requestingRNID)
	}
	return resp
}

func (c *homeCapability) recordPacketEvent(event *core.PacketEvent) {
	if c.recorder == nil || event == nil {
		return
	}
	if event.NodeID == 0 {
		event.NodeID = c.nodeID
	}
	c.recorder.RecordPacketEvent(event)
}

func (c *homeCapability) recordMetadata(txnID int64, key, value string) {
	if c.metadataRecorder == nil || txnID == 0 {
		return
	}
	c.metadataRecorder(txnID, key, value)
}

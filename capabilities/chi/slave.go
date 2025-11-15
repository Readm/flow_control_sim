package chi

import (
	"fmt"

	"github.com/Readm/flow_sim/capabilities"
	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/hooks"
)

// SlaveCapability generates CHI responses for slave nodes.
type SlaveCapability interface {
	capabilities.NodeCapability
	HandleRequest(req *core.Packet, cycle int, allocator capabilities.PacketAllocator) ([]*core.Packet, error)
}

// SlaveConfig configures dependencies for slave capability.
type SlaveConfig struct {
	Name           string
	NodeID         int
	Recorder       PacketRecorder
	SetFinalTarget func(packet *core.Packet, target int)
}

type slaveCapability struct {
	name           string
	nodeID         int
	recorder       PacketRecorder
	setFinalTarget func(packet *core.Packet, target int)
}

// NewSlaveCapability constructs the slave capability.
func NewSlaveCapability(cfg SlaveConfig) (SlaveCapability, error) {
	if cfg.SetFinalTarget == nil {
		return nil, fmt.Errorf("final target setter is required")
	}
	name := cfg.Name
	if name == "" {
		name = "chi-slave"
	}
	return &slaveCapability{
		name:           name,
		nodeID:         cfg.NodeID,
		recorder:       cfg.Recorder,
		setFinalTarget: cfg.SetFinalTarget,
	}, nil
}

func (c *slaveCapability) Descriptor() hooks.PluginDescriptor {
	return hooks.PluginDescriptor{
		Name:        c.name,
		Category:    hooks.PluginCategoryCapability,
		Description: "CHI MESI slave capability",
	}
}

func (c *slaveCapability) Register(broker *hooks.PluginBroker) error {
	if broker == nil {
		return nil
	}
	broker.RegisterPluginMetadata(c.Descriptor())
	return nil
}

func (c *slaveCapability) HandleRequest(req *core.Packet, cycle int, allocator capabilities.PacketAllocator) ([]*core.Packet, error) {
	if req == nil {
		return nil, nil
	}
	if allocator == nil {
		return nil, fmt.Errorf("packet allocator not configured")
	}
	id, err := allocator()
	if err != nil {
		return nil, err
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
	c.setFinalTarget(resp, resp.DstID)
	c.recordPacketEvent(&core.PacketEvent{
		TransactionID:  resp.TransactionID,
		PacketID:       resp.ID,
		ParentPacketID: req.ID,
		NodeID:         c.nodeID,
		EventType:      core.PacketGenerated,
		Cycle:          cycle,
	})
	return []*core.Packet{resp}, nil
}

func (c *slaveCapability) recordPacketEvent(event *core.PacketEvent) {
	if c.recorder == nil || event == nil {
		return
	}
	if event.NodeID == 0 {
		event.NodeID = c.nodeID
	}
	c.recorder.RecordPacketEvent(event)
}

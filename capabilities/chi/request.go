package chi

import (
	"fmt"

	"github.com/Readm/flow_sim/capabilities"
	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/hooks"
)

// RequestCapability coordinates request-side protocol actions (state machine + cache updates).
type RequestCapability interface {
	capabilities.NodeCapability
	BuildRequest(params capabilities.TxRequestParams) (*core.Packet, *core.Transaction, error)
	HandleResponse(packet *core.Packet, cycle int)
	HandleSnoopRequest(req *core.Packet, cycle int, allocator func() (int64, error)) (*core.Packet, error)
}

// RequestConfig wires dependencies for the request protocol capability.
type RequestConfig struct {
	Name            string
	Creator         capabilities.TransactionCreator
	Cache           capabilities.RequestCache
	StateMachine    capabilities.ProtocolStateMachine
	CacheHandler    capabilities.RequestCacheHandler
	PacketAllocator capabilities.PacketAllocator
}

type requestCapability struct {
	name            string
	creator         capabilities.TransactionCreator
	cache           capabilities.RequestCache
	sm              capabilities.ProtocolStateMachine
	handler         capabilities.RequestCacheHandler
	packetAllocator capabilities.PacketAllocator
}

// NewRequestCapability creates a CHI/MESI request-side capability.
func NewRequestCapability(cfg RequestConfig) (RequestCapability, error) {
	if cfg.Creator == nil {
		return nil, fmt.Errorf("transaction creator is required")
	}
	if cfg.Cache == nil {
		return nil, fmt.Errorf("request cache is required")
	}
	if cfg.StateMachine == nil {
		return nil, fmt.Errorf("state machine is required")
	}
	if cfg.CacheHandler == nil {
		return nil, fmt.Errorf("cache handler is required")
	}
	name := cfg.Name
	if name == "" {
		name = "chi-request"
	}
	return &requestCapability{
		name:            name,
		creator:         cfg.Creator,
		cache:           cfg.Cache,
		sm:              cfg.StateMachine,
		handler:         cfg.CacheHandler,
		packetAllocator: cfg.PacketAllocator,
	}, nil
}

func (c *requestCapability) Descriptor() hooks.PluginDescriptor {
	return hooks.PluginDescriptor{
		Name:        c.name,
		Category:    hooks.PluginCategoryCapability,
		Description: "CHI MESI request capability",
	}
}

func (c *requestCapability) Register(broker *hooks.PluginBroker) error {
	if broker == nil {
		return nil
	}
	broker.RegisterPluginMetadata(c.Descriptor())
	return nil
}

func (c *requestCapability) BuildRequest(params capabilities.TxRequestParams) (*core.Packet, *core.Transaction, error) {
	packet, txn, err := c.creator(params)
	if err != nil || packet == nil {
		return packet, txn, err
	}
	event := requestEventFromTxn(params.TransactionType)
	if event != "" {
		c.sm.ApplyEvent(params.Address, event)
	}
	return packet, txn, nil
}

func (c *requestCapability) HandleResponse(packet *core.Packet, cycle int) {
	if packet == nil {
		return
	}
	switch {
	case packet.MessageType == core.CHIMsgComp && packet.ResponseType == core.CHIRespCompData:
		c.sm.ApplyEvent(packet.Address, "DataResp")
	case packet.MessageType == core.CHIMsgSnpResp && packet.ResponseType == core.CHIRespSnpData:
		c.sm.ApplyEvent(packet.Address, "SnoopData")
	}
}

func requestEventFromTxn(txType core.CHITransactionType) string {
	switch txType {
	case core.CHITxnReadNoSnp, core.CHITxnReadOnce:
		return "LocalGetS"
	case core.CHITxnWriteNoSnp, core.CHITxnWriteUnique:
		return "LocalGetX"
	default:
		return ""
	}
}

func (c *requestCapability) HandleSnoopRequest(req *core.Packet, cycle int, allocator func() (int64, error)) (*core.Packet, error) {
	if c.handler == nil || req == nil {
		return nil, fmt.Errorf("cache handler not configured")
	}
	if allocator == nil {
		allocator = c.packetAllocator
	}
	if allocator == nil {
		return nil, fmt.Errorf("packet allocator not configured")
	}
	resp, err := c.handler.BuildSnoopResponse(req.SrcID, req, allocator, cycle)
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.ResponseType == core.CHIRespSnpData {
		c.sm.ApplyEvent(req.Address, "SnoopGetS")
	} else if resp != nil && resp.ResponseType == core.CHIRespSnpInvalid {
		c.sm.ApplyEvent(req.Address, "SnoopGetX")
	}
	return resp, nil
}

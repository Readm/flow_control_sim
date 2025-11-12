package capabilities

import (
	"sync"

	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/hooks"
)

// CacheRole represents the intended owner of the cache capability.
type CacheRole string

const (
	// CacheRoleRequest is used by request nodes to maintain MESI states.
	CacheRoleRequest CacheRole = "request"
	// CacheRoleHome is used by home nodes to maintain simple cache lines.
	CacheRoleHome CacheRole = "home"
)

// RequestCache exposes MESI cache helpers for RequestNode.
type RequestCache interface {
	GetState(addr uint64) core.MESIState
	SetState(addr uint64, state core.MESIState)
	Invalidate(addr uint64)
	Clear()
}

// HomeCacheLine represents cache metadata maintained by HomeNode.
type HomeCacheLine struct {
	Address  uint64
	Valid    bool
	Metadata map[string]string
}

// HomeCache exposes cache helpers for HomeNode.
type HomeCache interface {
	GetLine(addr uint64) (HomeCacheLine, bool)
	UpdateLine(addr uint64, line HomeCacheLine)
	Invalidate(addr uint64)
	Clear()
}

// CacheCapability is a NodeCapability that also exposes cache helpers.
type CacheCapability interface {
	NodeCapability
	Role() CacheRole
}

// PacketAllocator allocates unique packet IDs.
type PacketAllocator func() (int64, error)

// RequestCacheHandler adds high-level helpers for RequestNode cache behaviour.
type RequestCacheHandler interface {
	CacheWithRequestStore
	HandleResponse(packet *core.Packet)
	BuildSnoopResponse(nodeID int, req *core.Packet, alloc PacketAllocator, cycle int) (*core.Packet, error)
}

// CacheWithRequestStore adds request cache accessors.
type CacheWithRequestStore interface {
	CacheCapability
	RequestCache() RequestCache
}

// CacheWithHomeStore adds home cache accessors.
type CacheWithHomeStore interface {
	CacheCapability
	HomeCache() HomeCache
}

type cacheCapability struct {
	name        string
	description string
	role        CacheRole

	requestStore *mesiCacheStore
	homeStore    *homeCacheStore
}

func NewMESICacheCapability(name string) CacheWithRequestStore {
	return &cacheCapability{
		name:         name,
		description:  "MESI cache capability",
		role:         CacheRoleRequest,
		requestStore: newMESICacheStore(),
	}
}

func NewHomeCacheCapability(name string) CacheWithHomeStore {
	return &cacheCapability{
		name:        name,
		description: "home cache capability",
		role:        CacheRoleHome,
		homeStore:   newHomeCacheStore(),
	}
}

func (c *cacheCapability) Descriptor() hooks.PluginDescriptor {
	return hooks.PluginDescriptor{
		Name:        c.name,
		Category:    hooks.PluginCategoryCapability,
		Description: c.description,
	}
}

func (c *cacheCapability) Register(broker *hooks.PluginBroker) error {
	if broker == nil {
		return nil
	}
	broker.RegisterPluginMetadata(c.Descriptor())
	return nil
}

func (c *cacheCapability) Role() CacheRole {
	return c.role
}

func (c *cacheCapability) RequestCache() RequestCache {
	return c.requestStore
}

func (c *cacheCapability) HomeCache() HomeCache {
	return c.homeStore
}

func (c *cacheCapability) HandleResponse(packet *core.Packet) {
	if c.role != CacheRoleRequest || c.requestStore == nil || packet == nil {
		return
	}
	if packet.TransactionType == core.CHITxnReadOnce && packet.ResponseType == core.CHIRespCompData {
		c.requestStore.SetState(packet.Address, core.MESIShared)
	}
}

func (c *cacheCapability) BuildSnoopResponse(nodeID int, req *core.Packet, alloc PacketAllocator, cycle int) (*core.Packet, error) {
	if c.role != CacheRoleRequest || c.requestStore == nil || req == nil || alloc == nil {
		return nil, nil
	}

	state := c.requestStore.GetState(req.Address)
	packetID, err := alloc()
	if err != nil {
		return nil, err
	}

	resp := &core.Packet{
		ID:              packetID,
		Type:            "response",
		SrcID:           nodeID,
		DstID:           req.SrcID,
		GeneratedAt:     cycle,
		SentAt:          0,
		MasterID:        req.SrcID,
		RequestID:       req.ID,
		TransactionType: req.TransactionType,
		MessageType:     core.CHIMsgSnpResp,
		Address:         req.Address,
		DataSize:        req.DataSize,
		TransactionID:   req.TransactionID,
		OriginalTxnID:   req.TransactionID,
		ParentPacketID:  req.ID,
	}

	if state.CanProvideData() {
		resp.ResponseType = core.CHIRespSnpData
		if req.TransactionType == core.CHITxnReadOnce {
			c.requestStore.SetState(req.Address, core.MESIShared)
		}
	} else {
		resp.ResponseType = core.CHIRespSnpNoData
	}

	return resp, nil
}

type mesiCacheStore struct {
	mu    sync.RWMutex
	state map[uint64]core.MESIState
}

func newMESICacheStore() *mesiCacheStore {
	return &mesiCacheStore{
		state: make(map[uint64]core.MESIState),
	}
}

func (s *mesiCacheStore) GetState(addr uint64) core.MESIState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if state, ok := s.state[addr]; ok {
		return state
	}
	return core.MESIInvalid
}

func (s *mesiCacheStore) SetState(addr uint64, state core.MESIState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state == core.MESIInvalid {
		delete(s.state, addr)
		return
	}
	s.state[addr] = state
}

func (s *mesiCacheStore) Invalidate(addr uint64) {
	s.SetState(addr, core.MESIInvalid)
}

func (s *mesiCacheStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.state)
}

type homeCacheStore struct {
	mu    sync.RWMutex
	lines map[uint64]HomeCacheLine
}

func newHomeCacheStore() *homeCacheStore {
	return &homeCacheStore{
		lines: make(map[uint64]HomeCacheLine),
	}
}

func (s *homeCacheStore) GetLine(addr uint64) (HomeCacheLine, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	line, ok := s.lines[addr]
	return line, ok
}

func (s *homeCacheStore) UpdateLine(addr uint64, line HomeCacheLine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !line.Valid {
		delete(s.lines, addr)
		return
	}
	line.Address = addr
	s.lines[addr] = line
}

func (s *homeCacheStore) Invalidate(addr uint64) {
	s.UpdateLine(addr, HomeCacheLine{Valid: false})
}

func (s *homeCacheStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.lines)
}

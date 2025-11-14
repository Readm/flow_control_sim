package capabilities

import (
	"strconv"
	"sync"

	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/hooks"
)

// RequestCache exposes MESI cache helpers.
type RequestCache interface {
	GetState(addr uint64) core.MESIState
	SetState(addr uint64, state core.MESIState)
	Invalidate(addr uint64)
	Clear()
}

// HomeCacheLine represents cache metadata maintained by cache capability users.
type HomeCacheLine struct {
	Address  uint64
	State    core.MESIState
	Valid    bool
	Metadata map[string]string
}

// HomeCache exposes cache helpers that operate on cache lines.
type HomeCache interface {
	GetLine(addr uint64) (HomeCacheLine, bool)
	UpdateLine(addr uint64, line HomeCacheLine)
	Invalidate(addr uint64)
	Clear()
}

// CacheCapability is a NodeCapability that also exposes cache helpers.
type CacheCapability interface {
	NodeCapability
}

// CacheConfig configures cache capability features.
type CacheConfig struct {
	Description   string
	EnableRequest bool
	EnableLine    bool
	DefaultState  core.MESIState
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
	name         string
	description  string
	cfg          CacheConfig
	store        *cacheStore
	requestStore RequestCache
	homeStore    HomeCache
}

func NewCacheCapability(name string, cfg CacheConfig) CacheCapability {
	if cfg.DefaultState == "" {
		cfg.DefaultState = core.MESIModified
	}
	desc := cfg.Description
	if desc == "" {
		switch {
		case cfg.EnableRequest && cfg.EnableLine:
			desc = "unified cache capability"
		case cfg.EnableRequest:
			desc = "MESI cache capability"
		case cfg.EnableLine:
			desc = "cache line capability"
		default:
			desc = "cache capability"
		}
	}

	store := newCacheStore()
	cap := &cacheCapability{
		name:        name,
		description: desc,
		cfg:         cfg,
		store:       store,
	}
	if cfg.EnableRequest {
		cap.requestStore = &requestCacheAdapter{store: store}
	}
	if cfg.EnableLine {
		cap.homeStore = &homeCacheAdapter{store: store, defaultState: cfg.DefaultState}
	}
	return cap
}

func NewMESICacheCapability(name string) CacheWithRequestStore {
	return NewCacheCapability(name, CacheConfig{
		Description:   "MESI cache capability",
		EnableRequest: true,
	}).(CacheWithRequestStore)
}

func NewHomeCacheCapability(name string) CacheWithHomeStore {
	return NewCacheCapability(name, CacheConfig{
		Description:  "cache line capability",
		EnableLine:   true,
		DefaultState: core.MESIModified,
	}).(CacheWithHomeStore)
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

func (c *cacheCapability) RequestCache() RequestCache {
	return c.requestStore
}

func (c *cacheCapability) HomeCache() HomeCache {
	return c.homeStore
}

func (c *cacheCapability) HandleResponse(packet *core.Packet) {
	if c.requestStore == nil || packet == nil {
		return
	}
	if packet.TransactionType == core.CHITxnReadOnce && packet.ResponseType == core.CHIRespCompData {
		c.requestStore.SetState(packet.Address, core.MESIShared)
	}
}

func (c *cacheCapability) BuildSnoopResponse(nodeID int, req *core.Packet, alloc PacketAllocator, cycle int) (*core.Packet, error) {
	if c.requestStore == nil || req == nil || alloc == nil {
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

	resp.SetMetadata(RingFinalTargetMetadataKey, strconv.Itoa(resp.DstID))

	return resp, nil
}

type cacheEntry struct {
	State    core.MESIState
	Valid    bool
	Metadata map[string]string
}

type cacheStore struct {
	mu    sync.RWMutex
	lines map[uint64]cacheEntry
}

func newCacheStore() *cacheStore {
	return &cacheStore{lines: make(map[uint64]cacheEntry)}
}

func (s *cacheStore) getEntry(addr uint64) (cacheEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.lines[addr]
	if !ok {
		return cacheEntry{}, false
	}
	return cacheEntry{
		State:    entry.State,
		Valid:    entry.Valid,
		Metadata: core.CloneMetadata(entry.Metadata),
	}, true
}

func (s *cacheStore) setEntry(addr uint64, entry cacheEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !entry.Valid {
		delete(s.lines, addr)
		return
	}
	entry.Metadata = core.CloneMetadata(entry.Metadata)
	s.lines[addr] = entry
}

func (s *cacheStore) setState(addr uint64, state core.MESIState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state == core.MESIInvalid {
		delete(s.lines, addr)
		return
	}
	entry := s.lines[addr]
	entry.State = state
	entry.Valid = true
	if entry.Metadata == nil {
		entry.Metadata = map[string]string{}
	}
	s.lines[addr] = entry
}

func (s *cacheStore) getState(addr uint64) core.MESIState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.lines[addr]
	if !ok || !entry.Valid {
		return core.MESIInvalid
	}
	return entry.State
}

func (s *cacheStore) invalidate(addr uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.lines, addr)
}

func (s *cacheStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.lines)
}

type requestCacheAdapter struct {
	store *cacheStore
}

func (a *requestCacheAdapter) GetState(addr uint64) core.MESIState {
	return a.store.getState(addr)
}

func (a *requestCacheAdapter) SetState(addr uint64, state core.MESIState) {
	a.store.setState(addr, state)
}

func (a *requestCacheAdapter) Invalidate(addr uint64) {
	a.store.invalidate(addr)
}

func (a *requestCacheAdapter) Clear() {
	a.store.clear()
}

type homeCacheAdapter struct {
	store        *cacheStore
	defaultState core.MESIState
}

func (a *homeCacheAdapter) GetLine(addr uint64) (HomeCacheLine, bool) {
	entry, ok := a.store.getEntry(addr)
	if !ok || !entry.Valid {
		return HomeCacheLine{}, false
	}
	state := entry.State
	if state == "" {
		state = a.defaultState
	}
	return HomeCacheLine{
		Address:  addr,
		State:    state,
		Valid:    true,
		Metadata: entry.Metadata,
	}, true
}

func (a *homeCacheAdapter) UpdateLine(addr uint64, line HomeCacheLine) {
	if !line.Valid {
		a.store.invalidate(addr)
		return
	}
	state := line.State
	if state == "" {
		state = a.defaultState
	}
	a.store.setEntry(addr, cacheEntry{
		State:    state,
		Valid:    true,
		Metadata: line.Metadata,
	})
}

func (a *homeCacheAdapter) Invalidate(addr uint64) {
	a.store.invalidate(addr)
}

func (a *homeCacheAdapter) Clear() {
	a.store.clear()
}

var (
	_ CacheCapability       = (*cacheCapability)(nil)
	_ CacheWithRequestStore = (*cacheCapability)(nil)
	_ CacheWithHomeStore    = (*cacheCapability)(nil)
	_ RequestCacheHandler   = (*cacheCapability)(nil)
)

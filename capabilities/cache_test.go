package capabilities

import (
	"testing"

	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/hooks"
)

func TestMESICacheCapabilityStoresState(t *testing.T) {
	cap := NewMESICacheCapability("mesi-cache-test")
	broker := hooks.NewPluginBroker()
	if err := cap.Register(broker); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	store := cap.RequestCache()
	if store == nil {
		t.Fatalf("expected request cache store")
	}

	addr := uint64(0x1000)
	if state := store.GetState(addr); state != core.MESIInvalid {
		t.Fatalf("expected invalid state, got %v", state)
	}

	store.SetState(addr, core.MESIShared)
	if state := store.GetState(addr); state != core.MESIShared {
		t.Fatalf("expected shared state, got %v", state)
	}

	store.Invalidate(addr)
	if state := store.GetState(addr); state != core.MESIInvalid {
		t.Fatalf("expected invalid state after invalidate, got %v", state)
	}

	handler, ok := cap.(RequestCacheHandler)
	if !ok {
		t.Fatalf("expected capability to implement RequestCacheHandler")
	}
	packet := &core.Packet{
		TransactionType: core.CHITxnReadOnce,
		ResponseType:    core.CHIRespCompData,
		Address:         addr,
	}
	handler.HandleResponse(packet)
	if state := store.GetState(addr); state != core.MESIShared {
		t.Fatalf("expected shared state after HandleResponse, got %v", state)
	}

	makeAllocator := func() PacketAllocator {
		next := int64(1)
		return func() (int64, error) {
			id := next
			next++
			return id, nil
		}
	}

	resp, err := handler.BuildSnoopResponse(10, &core.Packet{
		ID:              2,
		SrcID:           1,
		TransactionType: core.CHITxnReadOnce,
		Address:         addr,
		DataSize:        64,
	}, makeAllocator(), 100)
	if err != nil {
		t.Fatalf("BuildSnoopResponse returned error: %v", err)
	}
	if resp == nil || resp.ResponseType != core.CHIRespSnpData {
		t.Fatalf("expected SnpData response, got %+v", resp)
	}

	// invalidate and expect NoData
	store.Invalidate(addr)
	resp, err = handler.BuildSnoopResponse(10, &core.Packet{
		ID:              3,
		SrcID:           1,
		TransactionType: core.CHITxnReadOnce,
		Address:         addr,
		DataSize:        64,
	}, makeAllocator(), 101)
	if err != nil {
		t.Fatalf("BuildSnoopResponse returned error: %v", err)
	}
	if resp == nil || resp.ResponseType != core.CHIRespSnpNoData {
		t.Fatalf("expected SnpNoData response, got %+v", resp)
	}
}

func TestHomeCacheCapabilityUpdatesLines(t *testing.T) {
	cap := NewHomeCacheCapability("home-cache-test")
	broker := hooks.NewPluginBroker()
	if err := cap.Register(broker); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	store := cap.HomeCache()
	if store == nil {
		t.Fatalf("expected home cache store")
	}

	addr := uint64(0x2000)
	if line, ok := store.GetLine(addr); ok || line.Valid {
		t.Fatalf("expected cache miss for new address")
	}

	store.UpdateLine(addr, HomeCacheLine{Valid: true})
	line, ok := store.GetLine(addr)
	if !ok || !line.Valid {
		t.Fatalf("expected valid cache line after update")
	}
	if line.Address != addr {
		t.Fatalf("expected stored address %x, got %x", addr, line.Address)
	}

	store.Invalidate(addr)
	if line, ok := store.GetLine(addr); ok && line.Valid {
		t.Fatalf("expected cache line to be invalidated")
	}
}

package decoder

import (
	"testing"
)

// mockDecoder is a simple test implementation of the Decoder interface
type mockDecoder struct {
	targetID   int
	attributes map[string]interface{}
}

func newMockDecoder(targetID int) *mockDecoder {
	return &mockDecoder{
		targetID: targetID,
		attributes: map[string]interface{}{
			AttrIsMemory:    true,
			AttrIsCacheable: true,
			AttrHomeNodeID:  targetID,
		},
	}
}

func (m *mockDecoder) DecodeAddress(addr uint64) (*DecodeResult, error) {
	return &DecodeResult{
		Addr:       addr,
		TargetID:   m.targetID,
		Attributes: m.attributes,
	}, nil
}

func TestDecoder_Interface(t *testing.T) {
	t.Parallel()

	// Test that mockDecoder implements Decoder interface
	var _ Decoder = (*mockDecoder)(nil)
}

func TestDecodeResult_BasicFields(t *testing.T) {
	t.Parallel()

	addr := uint64(0x1000)
	targetID := 42

	decoder := newMockDecoder(targetID)
	result, err := decoder.DecodeAddress(addr)

	if err != nil {
		t.Fatalf("DecodeAddress failed: %v", err)
	}

	// Test basic fields
	if result.Addr != addr {
		t.Errorf("expected Addr=%#x, got %#x", addr, result.Addr)
	}
	if result.TargetID != targetID {
		t.Errorf("expected TargetID=%d, got %d", targetID, result.TargetID)
	}
}

func TestDecodeResult_Attributes(t *testing.T) {
	t.Parallel()

	decoder := newMockDecoder(10)
	result, err := decoder.DecodeAddress(0x2000)

	if err != nil {
		t.Fatalf("DecodeAddress failed: %v", err)
	}

	// Test standard attributes
	if result.Attributes == nil {
		t.Fatal("Attributes should not be nil")
	}

	// Test AttrIsMemory
	isMemory, ok := result.Attributes[AttrIsMemory].(bool)
	if !ok {
		t.Error("AttrIsMemory should be bool")
	}
	if !isMemory {
		t.Error("expected AttrIsMemory to be true")
	}

	// Test AttrIsCacheable
	isCacheable, ok := result.Attributes[AttrIsCacheable].(bool)
	if !ok {
		t.Error("AttrIsCacheable should be bool")
	}
	if !isCacheable {
		t.Error("expected AttrIsCacheable to be true")
	}

	// Test AttrHomeNodeID
	homeNodeID, ok := result.Attributes[AttrHomeNodeID].(int)
	if !ok {
		t.Error("AttrHomeNodeID should be int")
	}
	if homeNodeID != 10 {
		t.Errorf("expected AttrHomeNodeID=10, got %d", homeNodeID)
	}
}

func TestDecodeResult_MultipleAddresses(t *testing.T) {
	t.Parallel()

	decoder := newMockDecoder(5)

	addresses := []uint64{0x1000, 0x2000, 0x3000, 0xFFFF}

	for _, addr := range addresses {
		result, err := decoder.DecodeAddress(addr)
		if err != nil {
			t.Errorf("DecodeAddress(%#x) failed: %v", addr, err)
			continue
		}

		if result.Addr != addr {
			t.Errorf("address %#x: expected Addr=%#x, got %#x", addr, addr, result.Addr)
		}

		if result.TargetID != 5 {
			t.Errorf("address %#x: expected TargetID=5, got %d", addr, result.TargetID)
		}
	}
}

func TestDecodeResult_AttributeConstants(t *testing.T) {
	t.Parallel()

	// Test that attribute constants are properly defined
	constants := []string{
		AttrIsMemory,
		AttrIsCacheable,
		AttrHomeNodeID,
		AttrSliceID,
	}

	expectedValues := []string{
		"IsMemory",
		"IsCacheable",
		"HomeNodeID",
		"SliceID",
	}

	for i, constant := range constants {
		if constant != expectedValues[i] {
			t.Errorf("constant %d: expected %q, got %q", i, expectedValues[i], constant)
		}
	}
}

func TestDecoder_CustomAttributes(t *testing.T) {
	t.Parallel()

	// Test decoder with custom attributes
	decoder := &mockDecoder{
		targetID: 7,
		attributes: map[string]interface{}{
			AttrIsMemory:    true,
			AttrIsCacheable: false,
			AttrHomeNodeID:  7,
			AttrSliceID:     3,
			"CustomAttr":    "custom_value",
		},
	}

	result, err := decoder.DecodeAddress(0x5000)
	if err != nil {
		t.Fatalf("DecodeAddress failed: %v", err)
	}

	// Test custom attribute
	customValue, ok := result.Attributes["CustomAttr"].(string)
	if !ok {
		t.Error("CustomAttr should be string")
	}
	if customValue != "custom_value" {
		t.Errorf("expected CustomAttr='custom_value', got %q", customValue)
	}

	// Test SliceID
	sliceID, ok := result.Attributes[AttrSliceID].(int)
	if !ok {
		t.Error("AttrSliceID should be int")
	}
	if sliceID != 3 {
		t.Errorf("expected AttrSliceID=3, got %d", sliceID)
	}

	// Test IsCacheable is false
	isCacheable, ok := result.Attributes[AttrIsCacheable].(bool)
	if !ok {
		t.Error("AttrIsCacheable should be bool")
	}
	if isCacheable {
		t.Error("expected AttrIsCacheable to be false")
	}
}

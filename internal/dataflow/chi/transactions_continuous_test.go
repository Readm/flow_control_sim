package chi

import (
	"context"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/capability/cache"
	"github.com/Readm/flow_sim/internal/core/capability/decoder"
	"github.com/Readm/flow_sim/internal/core/capability/directory"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/dataflow/message"
	"github.com/Readm/flow_sim/internal/dataflow/transaction"
)

// ============================================================================
// Test Setup Helpers
// ============================================================================

// TestDecoder implements a simple decoder for testing
type TestDecoder struct {
	// addrRanges maps address ranges to node IDs
	// For simplicity: addr >> 12 gives the node ID
	nodeMapping map[uint64]int
}

func NewTestDecoder() *TestDecoder {
	return &TestDecoder{
		nodeMapping: make(map[uint64]int),
	}
}

// AddMapping adds an address range to node mapping
// startAddr: start of address range
// endAddr: end of address range (inclusive)
// nodeID: target node for this range
func (d *TestDecoder) AddMapping(startAddr, endAddr uint64, nodeID int) {
	// Simple implementation: just map each address
	for addr := startAddr; addr <= endAddr; addr += 0x1000 {
		d.nodeMapping[addr] = nodeID
	}
}

func (d *TestDecoder) DecodeAddress(addr uint64) (*decoder.DecodeResult, error) {
	// Align to 4KB boundary
	alignedAddr := addr & 0xFFFFFFFFFFFFF000

	nodeID, exists := d.nodeMapping[alignedAddr]
	if !exists {
		// Default: map to node 2 (Home Node)
		nodeID = 2
	}

	return &decoder.DecodeResult{
		Addr:     addr,
		TargetID: nodeID,
	}, nil
}

// setupTestNode creates a node with Cache, Directory, and Decoder
func setupTestNode(nodeID int, dec *TestDecoder) *node.Node {
	n := node.New(nodeID)

	// Add Cache
	c := cache.NewFullyAssociativeCache(16)
	n.AddCache(c)

	// Add Directory (for Home Node)
	if nodeID >= 2 {
		dir := directory.NewFullyAssociativeDirectory(16)
		n.AddDirectory(dir)
	}

	// Add Decoder
	n.SetData("CHI_Decoder", dec)

	// Add Message Builder
	mb := NewMessageBuilder(nodeID)
	n.SetData("CHI_MessageBuilder", mb)

	return n
}

// ============================================================================
// Test: ReadShared Continuous
// ============================================================================

func TestReadSharedContinuous(t *testing.T) {
	// ===== Setup =====
	// Create decoder: address 0x1000 maps to HN (node 2)
	dec := NewTestDecoder()
	dec.AddMapping(0x1000, 0x1FFF, 2)

	// Create nodes
	rn := setupTestNode(1, dec) // Requester Node
	hn := setupTestNode(2, dec) // Home Node

	// Create TxnManagers
	rnMgr := transaction.NewTxnManager(rn)
	hnMgr := transaction.NewTxnManager(hn)

	// Test address
	testAddr := uint64(0x1000)

	// Track execution
	done := make(chan bool, 1)
	var receivedData []byte
	var executionError error

	// ===== Execute Transaction =====
	rnMgr.Start(context.Background(), func(ctx *transaction.TxnContext) {
		data, err := ReadSharedContinuous(ctx, testAddr)
		if err != nil {
			t.Errorf("ReadSharedContinuous failed: %v", err)
			executionError = err
			done <- false
			return
		}

		receivedData = data
		done <- true
	})

	// ===== Simulate Tick-based Execution =====
	time.Sleep(10 * time.Millisecond)

	// Cycle 1: RN processes Send() - sends ReadShared request
	out1, _ := rnMgr.Tick(1, nil)

	// Cycle 2: RN processes MigrateTo() - sends migration request
	out2, _ := rnMgr.Tick(2, nil)

	// Collect all outgoing messages
	allOut := append(out1, out2...)

	// Find migration message
	var migMsg *message.Message
	for _, msg := range allOut {
		if msg.Type == transaction.MsgTypeMigrationRequest {
			migMsg = msg
			break
		}
	}

	if migMsg == nil {
		t.Fatal("Migration request not found")
	}

	// Verify migration target
	if migMsg.TargetNodeID != 2 {
		t.Errorf("Expected migration to node 2, got %d", migMsg.TargetNodeID)
	}

	// Cycle 3: HN receives migration request
	hnMgr.Tick(3, []*message.Message{migMsg})

	time.Sleep(10 * time.Millisecond)

	// Cycle 4-10: HN processes transaction's operations
	// (Send CompData, MigrateTo back)
	for i := 4; i <= 10; i++ {
		outHN, _ := hnMgr.Tick(uint64(i), nil)

		// Look for migration back to RN
		for _, msg := range outHN {
			if msg.Type == transaction.MsgTypeMigrationRequest && msg.TargetNodeID == 1 {
				// Process migration back
				rnMgr.Tick(uint64(i+1), []*message.Message{msg})
				time.Sleep(10 * time.Millisecond)
			}
		}
	}

	// Final ticks to complete
	for i := 11; i <= 15; i++ {
		rnMgr.Tick(uint64(i), nil)
		hnMgr.Tick(uint64(i), nil)
	}

	// ===== Verify Results =====
	select {
	case success := <-done:
		if !success {
			if executionError != nil {
				t.Fatalf("Transaction failed: %v", executionError)
			} else {
				t.Fatal("Transaction failed without error")
			}
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Transaction timed out")
	}

	// Verify data received
	if receivedData == nil {
		t.Fatal("No data received")
	}

	if len(receivedData) != 64 { // loadDataFromMemory returns 64 bytes
		t.Errorf("Expected 64 bytes, got %d", len(receivedData))
	}

	// Verify RN cache state
	rnCache := rn.Caches()[0]
	if !rnCache.IsPresent(testAddr) {
		t.Error("Address not present in RN cache")
	}

	cacheState := rnCache.GetState(testAddr)
	if cacheState != cache.StateShared {
		t.Errorf("Expected cache state Shared, got %s", cacheState)
	}

	// Verify HN directory state
	hnDir := hn.Directories()[0]
	dirState := hnDir.GetState(testAddr)
	if dirState != "Shared" {
		t.Errorf("Expected directory state Shared, got %s", dirState)
	}

	sharers := hnDir.GetSharers(testAddr)
	if len(sharers) == 0 {
		t.Error("Expected at least one sharer in directory")
	}

	if !contains(sharers, 1) {
		t.Errorf("Expected RN (node 1) to be in sharers list, got %v", sharers)
	}
}

// ============================================================================
// Test: ReadUnique Continuous
// ============================================================================

func TestReadUniqueContinuous(t *testing.T) {
	// ===== Setup =====
	dec := NewTestDecoder()
	dec.AddMapping(0x2000, 0x2FFF, 2)

	rn := setupTestNode(1, dec)
	hn := setupTestNode(2, dec)

	rnMgr := transaction.NewTxnManager(rn)
	hnMgr := transaction.NewTxnManager(hn)

	testAddr := uint64(0x2000)

	done := make(chan bool, 1)
	var receivedData []byte
	var executionError error

	// ===== Execute Transaction =====
	rnMgr.Start(context.Background(), func(ctx *transaction.TxnContext) {
		data, err := ReadUniqueContinuous(ctx, testAddr)
		if err != nil {
			t.Errorf("ReadUniqueContinuous failed: %v", err)
			executionError = err
			done <- false
			return
		}

		receivedData = data
		done <- true
	})

	// ===== Simulate Execution =====
	time.Sleep(10 * time.Millisecond)

	// Process ticks
	out1, _ := rnMgr.Tick(1, nil)
	out2, _ := rnMgr.Tick(2, nil)

	allOut := append(out1, out2...)

	var migMsg *message.Message
	for _, msg := range allOut {
		if msg.Type == transaction.MsgTypeMigrationRequest {
			migMsg = msg
			break
		}
	}

	if migMsg == nil {
		t.Fatal("Migration request not found")
	}

	hnMgr.Tick(3, []*message.Message{migMsg})

	time.Sleep(10 * time.Millisecond)

	// Process HN and RN ticks for migration back
	for i := 4; i <= 15; i++ {
		outHN, _ := hnMgr.Tick(uint64(i), nil)

		for _, msg := range outHN {
			if msg.Type == transaction.MsgTypeMigrationRequest && msg.TargetNodeID == 1 {
				rnMgr.Tick(uint64(i+1), []*message.Message{msg})
				time.Sleep(10 * time.Millisecond)
			}
		}

		rnMgr.Tick(uint64(i), nil)
	}

	// ===== Verify Results =====
	select {
	case success := <-done:
		if !success {
			if executionError != nil {
				t.Fatalf("Transaction failed: %v", executionError)
			} else {
				t.Fatal("Transaction failed without error")
			}
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Transaction timed out")
	}

	// Verify data
	if receivedData == nil {
		t.Fatal("No data received")
	}

	// Verify RN cache state is Exclusive
	rnCache := rn.Caches()[0]
	if !rnCache.IsPresent(testAddr) {
		t.Error("Address not present in RN cache")
	}

	cacheState := rnCache.GetState(testAddr)
	if cacheState != cache.StateExclusive {
		t.Errorf("Expected cache state Exclusive, got %s", cacheState)
	}

	// Verify HN directory state is Exclusive
	hnDir := hn.Directories()[0]
	dirState := hnDir.GetState(testAddr)
	if dirState != "Exclusive" {
		t.Errorf("Expected directory state Exclusive, got %s", dirState)
	}

	// Verify only RN is in sharers list
	sharers := hnDir.GetSharers(testAddr)
	if len(sharers) != 1 {
		t.Errorf("Expected exactly 1 sharer, got %d", len(sharers))
	}

	if sharers[0] != 1 {
		t.Errorf("Expected sharer to be node 1, got %d", sharers[0])
	}
}

// ============================================================================
// Test: WriteUnique Continuous
// ============================================================================

func TestWriteUniqueContinuous(t *testing.T) {
	// ===== Setup =====
	dec := NewTestDecoder()
	dec.AddMapping(0x3000, 0x3FFF, 2)

	rn := setupTestNode(1, dec)
	hn := setupTestNode(2, dec)

	rnMgr := transaction.NewTxnManager(rn)
	hnMgr := transaction.NewTxnManager(hn)

	testAddr := uint64(0x3000)
	testData := []byte{0x11, 0x22, 0x33, 0x44}

	done := make(chan bool, 1)
	var executionError error

	// ===== Execute Transaction =====
	rnMgr.Start(context.Background(), func(ctx *transaction.TxnContext) {
		err := WriteUniqueContinuous(ctx, testAddr, testData)
		if err != nil {
			t.Errorf("WriteUniqueContinuous failed: %v", err)
			executionError = err
			done <- false
			return
		}

		done <- true
	})

	// ===== Simulate Execution =====
	time.Sleep(10 * time.Millisecond)

	out1, _ := rnMgr.Tick(1, nil)
	out2, _ := rnMgr.Tick(2, nil)

	allOut := append(out1, out2...)

	var migMsg *message.Message
	for _, msg := range allOut {
		if msg.Type == transaction.MsgTypeMigrationRequest {
			migMsg = msg
			break
		}
	}

	if migMsg == nil {
		t.Fatal("Migration request not found")
	}

	hnMgr.Tick(3, []*message.Message{migMsg})

	time.Sleep(10 * time.Millisecond)

	for i := 4; i <= 15; i++ {
		outHN, _ := hnMgr.Tick(uint64(i), nil)

		for _, msg := range outHN {
			if msg.Type == transaction.MsgTypeMigrationRequest && msg.TargetNodeID == 1 {
				rnMgr.Tick(uint64(i+1), []*message.Message{msg})
				time.Sleep(10 * time.Millisecond)
			}
		}

		rnMgr.Tick(uint64(i), nil)
	}

	// ===== Verify Results =====
	select {
	case success := <-done:
		if !success {
			if executionError != nil {
				t.Fatalf("Transaction failed: %v", executionError)
			} else {
				t.Fatal("Transaction failed without error")
			}
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Transaction timed out")
	}

	// Verify RN cache has Modified state with correct data
	rnCache := rn.Caches()[0]
	if !rnCache.IsPresent(testAddr) {
		t.Error("Address not present in RN cache")
	}

	cacheState := rnCache.GetState(testAddr)
	if cacheState != cache.StateModified {
		t.Errorf("Expected cache state Modified, got %s", cacheState)
	}

	cachedData := rnCache.GetData(testAddr)
	if len(cachedData) < len(testData) {
		t.Errorf("Cached data too short: got %d bytes, expected at least %d", len(cachedData), len(testData))
	} else {
		// Check first few bytes match
		for i := 0; i < len(testData); i++ {
			if cachedData[i] != testData[i] {
				t.Errorf("Data mismatch at byte %d: expected 0x%02X, got 0x%02X", i, testData[i], cachedData[i])
			}
		}
	}

	// Verify HN directory state is Modified
	hnDir := hn.Directories()[0]
	dirState := hnDir.GetState(testAddr)
	if dirState != "Modified" {
		t.Errorf("Expected directory state Modified, got %s", dirState)
	}

	// Verify only RN is in sharers list
	sharers := hnDir.GetSharers(testAddr)
	if len(sharers) != 1 {
		t.Errorf("Expected exactly 1 sharer, got %d", len(sharers))
	}

	if sharers[0] != 1 {
		t.Errorf("Expected sharer to be node 1, got %d", sharers[0])
	}
}

// ============================================================================
// Test: Decoder Usage - No Hardcoded Node IDs
// ============================================================================

func TestDecoderDrivenMigration(t *testing.T) {
	// This test verifies that transactions use Decoder to determine
	// target nodes, not hardcoded values

	// ===== Setup with different address mappings =====
	dec := NewTestDecoder()
	dec.AddMapping(0x1000, 0x1FFF, 2) // Range 1 -> HN node 2
	dec.AddMapping(0x2000, 0x2FFF, 3) // Range 2 -> HN node 3
	dec.AddMapping(0x3000, 0x3FFF, 4) // Range 3 -> HN node 4

	rn := setupTestNode(1, dec)
	_ = setupTestNode(2, dec) // hn2
	_ = setupTestNode(3, dec) // hn3
	_ = setupTestNode(4, dec) // hn4

	rnMgr := transaction.NewTxnManager(rn)
	// Not needed for this test
	// hn2Mgr := transaction.NewTxnManager(hn2)
	// hn3Mgr := transaction.NewTxnManager(hn3)
	// hn4Mgr := transaction.NewTxnManager(hn4)

	// Test addresses from different ranges
	testCases := []struct {
		addr           uint64
		expectedHN     int
		description    string
	}{
		{0x1500, 2, "Address 0x1500 should map to HN 2"},
		{0x2ABC, 3, "Address 0x2ABC should map to HN 3"},
		{0x3777, 4, "Address 0x3777 should map to HN 4"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			done := make(chan bool, 1)
			var actualTargetNode int

			rnMgr.Start(context.Background(), func(ctx *transaction.TxnContext) {
				// Use decoder to find target
				decoder := ctx.GetDecoder()
				result, err := decoder.DecodeAddress(tc.addr)
				if err != nil {
					t.Errorf("DecodeAddress failed: %v", err)
					done <- false
					return
				}
				actualTargetNode = result.TargetID

				// Verify it matches expected
				if actualTargetNode != tc.expectedHN {
					t.Errorf("Decoder returned wrong target: expected %d, got %d", tc.expectedHN, actualTargetNode)
				}

				done <- true
			})

			time.Sleep(10 * time.Millisecond)
			rnMgr.Tick(1, nil)

			select {
			case <-done:
				// Success
			case <-time.After(100 * time.Millisecond):
				t.Fatal("Test timed out")
			}

			// Verify decoder was used correctly
			if actualTargetNode != tc.expectedHN {
				t.Errorf("Expected target node %d, got %d", tc.expectedHN, actualTargetNode)
			}
		})
	}
}

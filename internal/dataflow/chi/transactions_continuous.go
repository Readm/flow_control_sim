package chi

import (
	"fmt"
	"time"

	"github.com/Readm/flow_sim/internal/dataflow/message"
	"github.com/Readm/flow_sim/internal/dataflow/transaction"
)

// ============================================================================
// Continuous-Style CHI Transactions
// These transactions use MigrateTo() to move execution across nodes
// ============================================================================

// ReadSharedContinuous implements CHI ReadShared as a continuous transaction.
// The transaction goroutine migrates from RN to HN to handle the full flow.
//
// Flow:
// 1. [RN] Check local cache
// 2. [RN] Decode address to find Home Node
// 3. [RN] Send ReadShared request
// 4. [RN -> HN] Migrate to Home Node
// 5. [HN] Check directory state
// 6. [HN] Send snoops if needed, or get data from memory
// 7. [HN] Wait for snoop responses if needed
// 8. [HN] Send CompData response to RN
// 9. [HN -> RN] Migrate back to RN
// 10. [RN] Update local cache to Shared state
// 11. [RN] Return data
func ReadSharedContinuous(
	ctx *transaction.TxnContext,
	addr uint64,
) ([]byte, error) {
	// ===== Phase 1: On Requester Node (RN) =====
	rnNodeID := ctx.NodeID()

	// Get RN's capabilities
	cache := ctx.GetCache()
	decoder := ctx.GetDecoder()
	if decoder == nil {
		return nil, fmt.Errorf("decoder not available on RN %d", rnNodeID)
	}

	// Check local cache for fast path
	if cache != nil && cache.IsPresent(addr) {
		state := cache.GetState(addr)
		if state != "Invalid" {
			// Cache hit - return immediately
			return cache.GetData(addr), nil
		}
	}

	// Decode address to find Home Node
	decodeResult, err := decoder.DecodeAddress(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode address 0x%x: %w", addr, err)
	}
	homeNodeID := decodeResult.TargetID

	// Build and send ReadShared request
	reqPayload := NewCHIPayload(OpcodeReadShared, addr)
	txnID := ctx.TxnID()
	reqPayload.SetReturnInfo(rnNodeID, txnID.TxnID)

	reqMsg := &message.Message{
		TransactionID: ctx.TxnID(),
		Channel:       CHIChannelREQ,
		Type:          OpcodeReadShared,
		SourceNodeID:  rnNodeID,
		TargetNodeID:  homeNodeID,
		Payload:       reqPayload,
	}

	if err := ctx.Send(reqMsg); err != nil {
		return nil, fmt.Errorf("failed to send ReadShared request: %w", err)
	}

	// ===== Migrate to Home Node =====
	hnCtx, err := ctx.MigrateTo(homeNodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to migrate to HN %d: %w", homeNodeID, err)
	}

	// ===== Phase 2: On Home Node (HN) =====
	// Verify we're on the correct node
	if hnCtx.NodeID() != homeNodeID {
		return nil, fmt.Errorf("migration error: expected node %d, got %d", homeNodeID, hnCtx.NodeID())
	}

	// Get HN's capabilities
	hnDir := hnCtx.GetDirectory()
	if hnDir == nil {
		return nil, fmt.Errorf("directory not available on HN %d", homeNodeID)
	}

	// Check directory state
	dirState := hnDir.GetState(addr)
	var data []byte

	if dirState == "Modified" || dirState == "Owned" {
		// Need to snoop the owner to get latest data
		sharers := hnDir.GetSharers(addr)
		if len(sharers) == 0 {
			return nil, fmt.Errorf("directory shows Modified/Owned but no sharers for addr 0x%x", addr)
		}

		ownerID := sharers[0] // First sharer is the owner

		// Send snoop request
		snpPayload := NewCHIPayload(OpcodeSnpSharedFwd, addr)
		txnIDForSnp := ctx.TxnID()
		snpPayload.SetReturnInfo(rnNodeID, txnIDForSnp.TxnID)

		snpMsg := &message.Message{
			TransactionID: ctx.TxnID(),
			Channel:       CHIChannelSNP,
			Type:          OpcodeSnpSharedFwd,
			SourceNodeID:  homeNodeID,
			TargetNodeID:  ownerID,
			Payload:       snpPayload,
		}

		if err := hnCtx.Send(snpMsg); err != nil {
			return nil, fmt.Errorf("failed to send snoop: %w", err)
		}

		// Wait for snoop response with data
		snpResult, err := hnCtx.Yield(&transaction.YieldCommand{
			Type: transaction.YieldTypeWaitForMessage,
			WaitFor: &transaction.WaitForMessage{
				Type: OpcodeSnpRespData,
			},
			Timeout: 100 * time.Millisecond,
		})
		if err != nil {
			return nil, fmt.Errorf("snoop timeout for addr 0x%x: %w", addr, err)
		}

		snpRespMsg := snpResult.(*message.Message)
		snpRespPayload := snpRespMsg.Payload.(*CHIPayload)
		data = snpRespPayload.Data

		// Update directory: add new sharer, keep owner as sharer
		hnDir.SetState(addr, "Shared")
		if !contains(sharers, rnNodeID) {
			hnDir.AddSharer(addr, rnNodeID)
		}

	} else {
		// Clean or Shared state - get data from memory
		data = loadDataFromMemory(addr)

		// Update directory: add new sharer
		hnDir.SetState(addr, "Shared")
		hnDir.AddSharer(addr, rnNodeID)
	}

	// Send CompData response to RN
	// Note: In real system, this would be sent automatically
	// Here we send it explicitly before migrating back
	compPayload := NewCHIPayload(OpcodeCompData, addr)
	compPayload.SetData(data)

	compMsg := &message.Message{
		TransactionID: ctx.TxnID(),
		Channel:       CHIChannelDAT,
		Type:          OpcodeCompData,
		SourceNodeID:  homeNodeID,
		TargetNodeID:  rnNodeID,
		Payload:       compPayload,
	}

	if err := hnCtx.Send(compMsg); err != nil {
		return nil, fmt.Errorf("failed to send CompData: %w", err)
	}

	// ===== Migrate back to Requester Node =====
	rnCtx, err := hnCtx.MigrateTo(rnNodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to migrate back to RN %d: %w", rnNodeID, err)
	}

	// ===== Phase 3: Back on Requester Node (RN) =====
	// Update local cache
	rnCache := rnCtx.GetCache()
	if rnCache != nil {
		rnCache.SetData(addr, data)
		rnCache.SetState(addr, "Shared")
	}

	// Complete transaction
	rnCtx.Complete(nil)

	return data, nil
}

// ReadUniqueContinuous implements CHI ReadUnique as a continuous transaction.
// Similar to ReadShared but acquires exclusive access.
//
// Flow:
// 1. [RN] Check local cache for exclusive access
// 2. [RN] Decode address to find Home Node
// 3. [RN] Send ReadUnique request
// 4. [RN -> HN] Migrate to Home Node
// 5. [HN] Send snoops to invalidate all sharers
// 6. [HN] Wait for all snoop responses
// 7. [HN] Send CompData response to RN
// 8. [HN -> RN] Migrate back to RN
// 9. [RN] Update local cache to Exclusive state
// 10. [RN] Return data
func ReadUniqueContinuous(
	ctx *transaction.TxnContext,
	addr uint64,
) ([]byte, error) {
	// ===== Phase 1: On Requester Node (RN) =====
	rnNodeID := ctx.NodeID()

	// Get RN's capabilities
	cache := ctx.GetCache()
	decoder := ctx.GetDecoder()
	if decoder == nil {
		return nil, fmt.Errorf("decoder not available on RN %d", rnNodeID)
	}

	// Check local cache for fast path
	if cache != nil && cache.IsPresent(addr) {
		state := cache.GetState(addr)
		if state == "Exclusive" || state == "Modified" {
			// Already have exclusive access
			return cache.GetData(addr), nil
		}
	}

	// Decode address to find Home Node
	decodeResult, err := decoder.DecodeAddress(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode address 0x%x: %w", addr, err)
	}
	homeNodeID := decodeResult.TargetID

	// Build and send ReadUnique request
	reqPayload := NewCHIPayload(OpcodeReadUnique, addr)
	txnID := ctx.TxnID()
	reqPayload.SetReturnInfo(rnNodeID, txnID.TxnID)

	reqMsg := &message.Message{
		TransactionID: ctx.TxnID(),
		Channel:       CHIChannelREQ,
		Type:          OpcodeReadUnique,
		SourceNodeID:  rnNodeID,
		TargetNodeID:  homeNodeID,
		Payload:       reqPayload,
	}

	if err := ctx.Send(reqMsg); err != nil {
		return nil, fmt.Errorf("failed to send ReadUnique request: %w", err)
	}

	// ===== Migrate to Home Node =====
	hnCtx, err := ctx.MigrateTo(homeNodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to migrate to HN %d: %w", homeNodeID, err)
	}

	// ===== Phase 2: On Home Node (HN) =====
	if hnCtx.NodeID() != homeNodeID {
		return nil, fmt.Errorf("migration error: expected node %d, got %d", homeNodeID, hnCtx.NodeID())
	}

	// Get HN's capabilities
	hnDir := hnCtx.GetDirectory()
	if hnDir == nil {
		return nil, fmt.Errorf("directory not available on HN %d", homeNodeID)
	}

	// Get current sharers
	sharers := hnDir.GetSharers(addr)
	var data []byte

	// Send invalidating snoops to all current sharers
	for _, sharerID := range sharers {
		if sharerID == rnNodeID {
			continue // Don't snoop ourselves
		}

		snpPayload := NewCHIPayload(OpcodeSnpUniqueFwd, addr)
		txnIDForSnp := ctx.TxnID()
		snpPayload.SetReturnInfo(rnNodeID, txnIDForSnp.TxnID)

		snpMsg := &message.Message{
			TransactionID: ctx.TxnID(),
			Channel:       CHIChannelSNP,
			Type:          OpcodeSnpUniqueFwd,
			SourceNodeID:  homeNodeID,
			TargetNodeID:  sharerID,
			Payload:       snpPayload,
		}

		if err := hnCtx.Send(snpMsg); err != nil {
			return nil, fmt.Errorf("failed to send snoop to node %d: %w", sharerID, err)
		}
	}

	// Wait for all snoop responses
	snoopResponseCount := 0
	for _, sharerID := range sharers {
		if sharerID == rnNodeID {
			continue
		}

		result, err := hnCtx.Yield(&transaction.YieldCommand{
			Type: transaction.YieldTypeWaitForMessage,
			WaitFor: &transaction.WaitForMessage{
				Type: OpcodeSnpResp, // or OpcodeSnpRespData
			},
			Timeout: 100 * time.Millisecond,
		})
		if err != nil {
			return nil, fmt.Errorf("snoop timeout waiting for node %d: %w", sharerID, err)
		}

		snpRespMsg := result.(*message.Message)
		snpRespPayload := snpRespMsg.Payload.(*CHIPayload)

		// If this is the first response with data, use it
		if data == nil && snpRespPayload.Data != nil {
			data = snpRespPayload.Data
		}

		snoopResponseCount++
	}

	// If no snoop provided data, load from memory
	if data == nil {
		data = loadDataFromMemory(addr)
	}

	// Update directory: clear all sharers, add only the requester
	hnDir.ClearSharers(addr)
	hnDir.AddSharer(addr, rnNodeID)
	hnDir.SetState(addr, "Exclusive")

	// Send CompData response to RN
	compPayload := NewCHIPayload(OpcodeCompData, addr)
	compPayload.SetData(data)

	compMsg := &message.Message{
		TransactionID: ctx.TxnID(),
		Channel:       CHIChannelDAT,
		Type:          OpcodeCompData,
		SourceNodeID:  homeNodeID,
		TargetNodeID:  rnNodeID,
		Payload:       compPayload,
	}

	if err := hnCtx.Send(compMsg); err != nil {
		return nil, fmt.Errorf("failed to send CompData: %w", err)
	}

	// ===== Migrate back to Requester Node =====
	rnCtx, err := hnCtx.MigrateTo(rnNodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to migrate back to RN %d: %w", rnNodeID, err)
	}

	// ===== Phase 3: Back on Requester Node (RN) =====
	// Update local cache to Exclusive state
	rnCache := rnCtx.GetCache()
	if rnCache != nil {
		rnCache.SetData(addr, data)
		rnCache.SetState(addr, "Exclusive")
	}

	// Complete transaction
	rnCtx.Complete(nil)

	return data, nil
}

// WriteUniqueContinuous implements CHI WriteUnique as a continuous transaction.
// This transaction writes data and acquires exclusive ownership.
//
// Flow:
// 1. [RN] Check local cache - if Modified, write locally and return
// 2. [RN] Decode address to find Home Node
// 3. [RN] Send WriteUnique request with data
// 4. [RN -> HN] Migrate to Home Node
// 5. [HN] Send invalidating snoops to all sharers
// 6. [HN] Wait for all snoop responses
// 7. [HN] Send Comp response to RN
// 8. [HN -> RN] Migrate back to RN
// 9. [RN] Update local cache to Modified state with new data
// 10. [RN] Return success
func WriteUniqueContinuous(
	ctx *transaction.TxnContext,
	addr uint64,
	data []byte,
) error {
	// ===== Phase 1: On Requester Node (RN) =====
	rnNodeID := ctx.NodeID()

	// Get RN's capabilities
	cache := ctx.GetCache()
	decoder := ctx.GetDecoder()
	if decoder == nil {
		return fmt.Errorf("decoder not available on RN %d", rnNodeID)
	}

	// Fast path: if already have Modified state, just update locally
	if cache != nil && cache.IsPresent(addr) {
		state := cache.GetState(addr)
		if state == "Modified" {
			cache.SetData(addr, data)
			ctx.Complete(nil)
			return nil
		}
	}

	// Decode address to find Home Node
	decodeResult, err := decoder.DecodeAddress(addr)
	if err != nil {
		return fmt.Errorf("failed to decode address 0x%x: %w", addr, err)
	}
	homeNodeID := decodeResult.TargetID

	// Build and send WriteUnique request with data
	reqPayload := NewCHIPayload(OpcodeReadUnique, addr) // WriteUnique uses ReadUnique opcode
	reqPayload.SetData(data)
	txnID := ctx.TxnID()
	reqPayload.SetReturnInfo(rnNodeID, txnID.TxnID)

	reqMsg := &message.Message{
		TransactionID: ctx.TxnID(),
		Channel:       CHIChannelREQ,
		Type:          OpcodeReadUnique,
		SourceNodeID:  rnNodeID,
		TargetNodeID:  homeNodeID,
		Payload:       reqPayload,
	}

	if err := ctx.Send(reqMsg); err != nil {
		return fmt.Errorf("failed to send WriteUnique request: %w", err)
	}

	// ===== Migrate to Home Node =====
	hnCtx, err := ctx.MigrateTo(homeNodeID)
	if err != nil {
		return fmt.Errorf("failed to migrate to HN %d: %w", homeNodeID, err)
	}

	// ===== Phase 2: On Home Node (HN) =====
	if hnCtx.NodeID() != homeNodeID {
		return fmt.Errorf("migration error: expected node %d, got %d", homeNodeID, hnCtx.NodeID())
	}

	// Get HN's capabilities
	hnDir := hnCtx.GetDirectory()
	if hnDir == nil {
		return fmt.Errorf("directory not available on HN %d", homeNodeID)
	}

	// Get current sharers and invalidate them
	sharers := hnDir.GetSharers(addr)

	// Send invalidating snoops to all current sharers
	for _, sharerID := range sharers {
		if sharerID == rnNodeID {
			continue
		}

		snpPayload := NewCHIPayload(OpcodeSnpUniqueFwd, addr)
		txnIDForSnp := hnCtx.TxnID()
		snpPayload.SetReturnInfo(rnNodeID, txnIDForSnp.TxnID)

		snpMsg := &message.Message{
			TransactionID: ctx.TxnID(),
			Channel:       CHIChannelSNP,
			Type:          OpcodeSnpUniqueFwd,
			SourceNodeID:  homeNodeID,
			TargetNodeID:  sharerID,
			Payload:       snpPayload,
		}

		if err := hnCtx.Send(snpMsg); err != nil {
			return fmt.Errorf("failed to send snoop to node %d: %w", sharerID, err)
		}
	}

	// Wait for all snoop responses
	for _, sharerID := range sharers {
		if sharerID == rnNodeID {
			continue
		}

		_, err := hnCtx.Yield(&transaction.YieldCommand{
			Type: transaction.YieldTypeWaitForMessage,
			WaitFor: &transaction.WaitForMessage{
				Type: OpcodeSnpResp,
			},
			Timeout: 100 * time.Millisecond,
		})
		if err != nil {
			return fmt.Errorf("snoop timeout waiting for node %d: %w", sharerID, err)
		}
	}

	// Update directory: clear all sharers, add only the requester
	hnDir.ClearSharers(addr)
	hnDir.AddSharer(addr, rnNodeID)
	hnDir.SetState(addr, "Modified")

	// Send Comp response (no data needed for write)
	compPayload := NewCHIPayload(OpcodeComp, addr)

	compMsg := &message.Message{
		TransactionID: ctx.TxnID(),
		Channel:       CHIChannelRSP,
		Type:          OpcodeComp,
		SourceNodeID:  homeNodeID,
		TargetNodeID:  rnNodeID,
		Payload:       compPayload,
	}

	if err := hnCtx.Send(compMsg); err != nil {
		return fmt.Errorf("failed to send Comp: %w", err)
	}

	// ===== Migrate back to Requester Node =====
	rnCtx, err := hnCtx.MigrateTo(rnNodeID)
	if err != nil {
		return fmt.Errorf("failed to migrate back to RN %d: %w", rnNodeID, err)
	}

	// ===== Phase 3: Back on Requester Node (RN) =====
	// Update local cache to Modified state with new data
	rnCache := rnCtx.GetCache()
	if rnCache != nil {
		rnCache.SetData(addr, data)
		rnCache.SetState(addr, "Modified")
	}

	// Complete transaction
	rnCtx.Complete(nil)

	return nil
}

// ============================================================================
// Helper Functions
// ============================================================================

// contains checks if a slice contains a value
func contains(slice []int, val int) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

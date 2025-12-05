package chi

import (
	"context"
	"fmt"

	"github.com/Readm/flow_sim/internal/dataflow/message"
	"github.com/Readm/flow_sim/internal/dataflow/transaction"
)

// ============================================================================
// Continuous-Style Snoop Handlers
// These handlers use MigrateTo() to implement Direct Memory Transfer (DMT)
// ============================================================================

// SnpSharedFwdContinuous handles SnpSharedFwd snoop request.
// This snoop asks a node to provide data and downgrade to Shared state.
//
// Flow:
// 1. [Snooped RN] Check local cache state
// 2. [Snooped RN] Downgrade to Shared if needed
// 3a. [DMT Path] Migrate to requester, deliver data directly
// 3b. [Default Path] Send data back to HN, let HN forward
//
// DMT (Direct Memory Transfer):
//   - When enabled, data goes directly from snooped node to requester
//   - Reduces latency (one hop instead of two)
//   - Controlled by ReturnNID field in snoop message
func SnpSharedFwdContinuous(
	ctx *transaction.TxnContext,
	snpMsg *message.Message,
	useDMT bool, // Whether to use Direct Memory Transfer
) error {
	// ===== Phase 1: On Snooped Node (被 snoop 的节点) =====
	snpedNodeID := ctx.NodeID()

	// Extract snoop information
	payload, ok := snpMsg.Payload.(*CHIPayload)
	if !ok {
		return fmt.Errorf("invalid payload type in SnpSharedFwd")
	}

	addr := payload.Addr
	returnNID := payload.ReturnNID      // Original requester node ID
	returnTxnID := payload.ReturnTxnID  // Original transaction ID
	homeNodeID := snpMsg.SourceNodeID   // HN sent this snoop

	// Get local cache
	cache := ctx.GetCache()
	if cache == nil {
		// No cache capability, send SnpResp (no data)
		return sendSnpRespNoData(ctx, snpMsg, homeNodeID)
	}

	// Check if address is present
	if !cache.IsPresent(addr) {
		// Address not in cache, send SnpResp (no data)
		return sendSnpRespNoData(ctx, snpMsg, homeNodeID)
	}

	// Get current state and data
	state := cache.GetState(addr)
	data := cache.GetData(addr)
	var shouldProvideData bool

	// Determine if we should provide data based on state
	switch state {
	case "Modified", "Owned":
		// Dirty data - must provide
		shouldProvideData = true
		cache.SetState(addr, "Shared") // Downgrade to Shared

	case "Exclusive":
		// Clean exclusive - provide data
		shouldProvideData = true
		cache.SetState(addr, "Shared") // Downgrade to Shared

	case "Shared":
		// Already shared - usually don't provide data
		// (HN will get data from memory or other source)
		shouldProvideData = false

	default: // "Invalid"
		shouldProvideData = false
	}

	if !shouldProvideData {
		// No data to provide
		return sendSnpRespNoData(ctx, snpMsg, homeNodeID)
	}

	// ===== Choose Path: DMT vs Default =====
	if useDMT && returnNID != snpedNodeID {
		// ===== DMT Path: Migrate to Requester =====
		// Send data directly to requester, bypassing HN

		// Build SnpRespData to send to requester
		respPayload := NewCHIPayload(OpcodeSnpRespData, addr)
		respPayload.SetData(data)
		respPayload.SetReturnInfo(returnNID, returnTxnID)

		respMsg := &message.Message{
			TransactionID: snpMsg.TransactionID,
			Channel:       CHIChannelDAT,
			Type:          OpcodeSnpRespDataFwded, // Forwarded variant
			SourceNodeID:  snpedNodeID,
			TargetNodeID:  returnNID, // Direct to requester!
			Payload:       respPayload,
		}

		if err := ctx.SendAndWait(respMsg); err != nil {
			return fmt.Errorf("failed to send DMT SnpRespData: %w", err)
		}

		// Optionally: Migrate to requester to ensure delivery
		// (In real hardware, this is just routing; in simulation, we model it)
		reqCtx, err := ctx.MigrateTo(returnNID)
		if err != nil {
			return fmt.Errorf("DMT migration failed: %w", err)
		}

		// Now on requester node - could verify data received
		// But for snoop handler, we just complete here
		reqCtx.Complete(nil)

		return nil

	} else {
		// ===== Default Path: Send back to HN =====
		// HN will forward to requester

		respPayload := NewCHIPayload(OpcodeSnpRespData, addr)
		respPayload.SetData(data)

		respMsg := &message.Message{
			TransactionID: snpMsg.TransactionID,
			Channel:       CHIChannelDAT,
			Type:          OpcodeSnpRespData,
			SourceNodeID:  snpedNodeID,
			TargetNodeID:  homeNodeID, // Back to HN
			Payload:       respPayload,
		}

		if err := ctx.SendAndWait(respMsg); err != nil {
			return fmt.Errorf("failed to send SnpRespData: %w", err)
		}

		ctx.Complete(nil)
		return nil
	}
}

// SnpUniqueFwdContinuous handles SnpUniqueFwd snoop request.
// This snoop asks a node to invalidate its cache line.
//
// Flow:
// 1. [Snooped RN] Check local cache state
// 2. [Snooped RN] Invalidate cache line
// 3a. [DMT Path] If had dirty data, migrate to requester and deliver
// 3b. [Default Path] Send data/response back to HN
func SnpUniqueFwdContinuous(
	ctx *transaction.TxnContext,
	snpMsg *message.Message,
	useDMT bool,
) error {
	// ===== Phase 1: On Snooped Node =====
	snpedNodeID := ctx.NodeID()

	payload, ok := snpMsg.Payload.(*CHIPayload)
	if !ok {
		return fmt.Errorf("invalid payload type in SnpUniqueFwd")
	}

	addr := payload.Addr
	returnNID := payload.ReturnNID
	returnTxnID := payload.ReturnTxnID
	homeNodeID := snpMsg.SourceNodeID

	cache := ctx.GetCache()
	if cache == nil || !cache.IsPresent(addr) {
		// No data to invalidate
		return sendSnpRespNoData(ctx, snpMsg, homeNodeID)
	}

	// Get data before invalidating
	state := cache.GetState(addr)
	data := cache.GetData(addr)
	var shouldProvideData bool

	switch state {
	case "Modified", "Owned":
		// Dirty data - must provide
		shouldProvideData = true

	case "Exclusive":
		// Clean exclusive - may need to provide
		shouldProvideData = true

	case "Shared":
		// Shared - usually don't provide
		shouldProvideData = false

	default:
		shouldProvideData = false
	}

	// ===== Invalidate cache line =====
	cache.SetState(addr, "Invalid")

	if !shouldProvideData {
		// Send simple SnpResp
		return sendSnpRespNoData(ctx, snpMsg, homeNodeID)
	}

	// ===== Choose Path: DMT vs Default =====
	if useDMT && returnNID != snpedNodeID {
		// ===== DMT Path =====
		respPayload := NewCHIPayload(OpcodeSnpRespData, addr)
		respPayload.SetData(data)
		respPayload.SetReturnInfo(returnNID, returnTxnID)

		respMsg := &message.Message{
			TransactionID: snpMsg.TransactionID,
			Channel:       CHIChannelDAT,
			Type:          OpcodeSnpRespDataFwded,
			SourceNodeID:  snpedNodeID,
			TargetNodeID:  returnNID, // Direct to requester
			Payload:       respPayload,
		}

		if err := ctx.SendAndWait(respMsg); err != nil {
			return fmt.Errorf("failed to send DMT SnpRespData: %w", err)
		}

		// Migrate to requester for direct delivery
		reqCtx, err := ctx.MigrateTo(returnNID)
		if err != nil {
			return fmt.Errorf("DMT migration failed: %w", err)
		}

		reqCtx.Complete(nil)
		return nil

	} else {
		// ===== Default Path =====
		respPayload := NewCHIPayload(OpcodeSnpRespData, addr)
		respPayload.SetData(data)

		respMsg := &message.Message{
			TransactionID: snpMsg.TransactionID,
			Channel:       CHIChannelDAT,
			Type:          OpcodeSnpRespData,
			SourceNodeID:  snpedNodeID,
			TargetNodeID:  homeNodeID,
			Payload:       respPayload,
		}

		if err := ctx.SendAndWait(respMsg); err != nil {
			return fmt.Errorf("failed to send SnpRespData: %w", err)
		}

		ctx.Complete(nil)
		return nil
	}
}

// SnpInvalidateContinuous handles simple invalidation snoop.
// No data transfer needed, just invalidate.
func SnpInvalidateContinuous(
	ctx *transaction.TxnContext,
	snpMsg *message.Message,
) error {
	payload, ok := snpMsg.Payload.(*CHIPayload)
	if !ok {
		return fmt.Errorf("invalid payload type in SnpInvalidate")
	}

	addr := payload.Addr
	homeNodeID := snpMsg.SourceNodeID

	cache := ctx.GetCache()
	if cache == nil || !cache.IsPresent(addr) {
		// Nothing to invalidate
		return sendSnpRespNoData(ctx, snpMsg, homeNodeID)
	}

	// Invalidate cache line
	cache.SetState(addr, "Invalid")

	// Send simple SnpResp
	return sendSnpRespNoData(ctx, snpMsg, homeNodeID)
}

// ============================================================================
// Helper Functions
// ============================================================================

// sendSnpRespNoData sends a snoop response without data
func sendSnpRespNoData(
	ctx *transaction.TxnContext,
	snpMsg *message.Message,
	targetNodeID int,
) error {
	payload := snpMsg.Payload.(*CHIPayload)

	respPayload := NewCHIPayload(OpcodeSnpResp, payload.Addr)

	respMsg := &message.Message{
		TransactionID: snpMsg.TransactionID,
		Channel:       CHIChannelRSP,
		Type:          OpcodeSnpResp,
		SourceNodeID:  ctx.NodeID(),
		TargetNodeID:  targetNodeID,
		Payload:       respPayload,
	}

	if err := ctx.SendAndWait(respMsg); err != nil {
		return err
	}

	ctx.Complete(nil)
	return nil
}

// ============================================================================
// Snoop Dispatcher for Continuous Handlers
// ============================================================================

// ContinuousSnoopDispatcher dispatches snoop messages to continuous handlers
type ContinuousSnoopDispatcher struct {
	useDMT bool // Global DMT enable flag
}

// NewContinuousSnoopDispatcher creates a new dispatcher
func NewContinuousSnoopDispatcher(useDMT bool) *ContinuousSnoopDispatcher {
	return &ContinuousSnoopDispatcher{
		useDMT: useDMT,
	}
}

// Dispatch launches appropriate snoop handler based on message type
func (d *ContinuousSnoopDispatcher) Dispatch(
	txnMgr *transaction.TxnManager,
	snpMsg *message.Message,
) error {
	switch snpMsg.Type {
	case OpcodeSnpSharedFwd:
		// Launch SnpSharedFwdContinuous handler
		txnMgr.Start(context.Background(), func(ctx *transaction.TxnContext) {
			if err := SnpSharedFwdContinuous(ctx, snpMsg, d.useDMT); err != nil {
				// Log error
				fmt.Printf("SnpSharedFwd handler error: %v\n", err)
			}
		})
		return nil

	case OpcodeSnpUniqueFwd:
		// Launch SnpUniqueFwdContinuous handler
		txnMgr.Start(context.Background(), func(ctx *transaction.TxnContext) {
			if err := SnpUniqueFwdContinuous(ctx, snpMsg, d.useDMT); err != nil {
				fmt.Printf("SnpUniqueFwd handler error: %v\n", err)
			}
		})
		return nil

	default:
		return fmt.Errorf("unsupported snoop opcode: %d", snpMsg.Type)
	}
}

// ============================================================================
// Integration Example
// ============================================================================

// Example: How to integrate with TxnManager
//
// In your node setup:
//   dispatcher := NewContinuousSnoopDispatcher(useDMT=true)
//   node.SetData("CHI_SnoopDispatcher", dispatcher)
//
// In TxnManager.Tick():
//   for _, msg := range incoming {
//       if msg.Channel == CHIChannelSNP {
//           dispatcher := node.GetData("CHI_SnoopDispatcher").(*ContinuousSnoopDispatcher)
//           dispatcher.Dispatch(txnManager, msg)
//       }
//   }

package chi

import (
	"fmt"
	"time"

	"github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/dataflow/message"
	"github.com/Readm/flow_sim/internal/dataflow/transaction"
)

// ============================================================================
// CHI Transaction Implementations - Using Framework Interfaces
// ============================================================================

// ReadCleanTxn implements CHI ReadClean transaction from RN perspective.
//
// Flow:
// 1. Check local cache
// 2. If hit, return data
// 3. If miss, send ReadClean request to Home Node
// 4. Wait for CompData response
// 5. Update cache to Shared state
// 6. Return data
func ReadCleanTxn(
	ctx *transaction.TxnContext,
	n node.Node,
	addr uint64,
) ([]byte, error) {
	// Step 1: Get CHI capabilities from node
	c := GetCHICache(n)
	decoder, err := GetCHIDecoder(n)
	if err != nil {
		return nil, err
	}
	msgBuilder, err := GetCHIMessageBuilder(n)
	if err != nil {
		return nil, err
	}

	// Step 2: Check local cache
	if c != nil && c.IsPresent(addr) {
		state := c.GetState(addr)
		if state != cache.StateInvalid {
			// Cache hit
			return c.GetData(addr), nil
		}
	}

	// Step 3: Cache miss - decode address to find Home Node
	decodeResult, err := decoder.DecodeAddress(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode address 0x%x: %w", addr, err)
	}
	homeNodeID := decodeResult.TargetID

	// Step 4: Build ReadClean request message
	reqPayload := NewCHIPayload(OpcodeReadClean, addr)
	reqMsg := msgBuilder.NewMessage(
		ctx.TxnID(),
		OpcodeReadClean,
		n.ID(),
		homeNodeID,
		reqPayload,
	)

	// Step 5: Send request and wait for CompData response
	if err := ctx.Send(reqMsg); err != nil {
		return nil, err
	}

	result, err := ctx.Yield(&transaction.YieldCommand{
		Type: transaction.YieldTypeWaitForMessage,
		WaitFor: &transaction.WaitForMessage{
			Type: OpcodeCompData,
		},
		Timeout: 100 * time.Millisecond,
	})
	if err != nil {
		return nil, fmt.Errorf("ReadClean timeout for address 0x%x: %w", addr, err)
	}

	// Step 6: Extract data from response
	respMsg, ok := result.(*message.Message)
	if !ok {
		return nil, fmt.Errorf("invalid response type for ReadClean")
	}

	payload, ok := respMsg.Payload.(*CHIPayload)
	if !ok {
		return nil, fmt.Errorf("invalid payload type in CompData response")
	}

	// Step 7: Update local cache to Shared state
	if c != nil {
		c.SetData(addr, payload.Data)
		c.SetState(addr, cache.StateShared)
	}

	return payload.Data, nil
}

// ReadSharedTxn implements CHI ReadShared transaction from RN perspective.
// Similar to ReadClean, but may involve snooping other caches.
func ReadSharedTxn(
	ctx *transaction.TxnContext,
	n node.Node,
	addr uint64,
) ([]byte, error) {
	// Implementation similar to ReadCleanTxn
	// For simplicity, use same logic as ReadClean
	c := GetCHICache(n)
	decoder, err := GetCHIDecoder(n)
	if err != nil {
		return nil, err
	}
	msgBuilder, err := GetCHIMessageBuilder(n)
	if err != nil {
		return nil, err
	}

	// Check local cache
	if c != nil && c.IsPresent(addr) {
		state := c.GetState(addr)
		if state != cache.StateInvalid {
			return c.GetData(addr), nil
		}
	}

	// Decode address
	decodeResult, err := decoder.DecodeAddress(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode address 0x%x: %w", addr, err)
	}

	// Build ReadShared request
	reqPayload := NewCHIPayload(OpcodeReadShared, addr)
	reqMsg := msgBuilder.NewMessage(
		ctx.TxnID(),
		OpcodeReadShared,
		n.ID(),
		decodeResult.TargetID,
		reqPayload,
	)

	// Send and wait
	if err := ctx.Send(reqMsg); err != nil {
		return nil, err
	}

	result, err := ctx.Yield(&transaction.YieldCommand{
		Type: transaction.YieldTypeWaitForMessage,
		WaitFor: &transaction.WaitForMessage{
			Type: OpcodeCompData,
		},
		Timeout: 100 * time.Millisecond,
	})
	if err != nil {
		return nil, fmt.Errorf("ReadShared timeout for address 0x%x: %w", addr, err)
	}

	// Extract data
	respMsg, ok := result.(*message.Message)
	if !ok {
		return nil, fmt.Errorf("invalid response type for ReadShared")
	}

	payload, ok := respMsg.Payload.(*CHIPayload)
	if !ok {
		return nil, fmt.Errorf("invalid payload type in CompData response")
	}

	// Update cache
	if c != nil {
		c.SetData(addr, payload.Data)
		c.SetState(addr, cache.StateShared)
	}

	return payload.Data, nil
}

// ReadUniqueTxn implements CHI ReadUnique transaction (for exclusive access).
func ReadUniqueTxn(
	ctx *transaction.TxnContext,
	n node.Node,
	addr uint64,
) ([]byte, error) {
	c := GetCHICache(n)
	decoder, err := GetCHIDecoder(n)
	if err != nil {
		return nil, err
	}
	msgBuilder, err := GetCHIMessageBuilder(n)
	if err != nil {
		return nil, err
	}

	// Check if already have exclusive access
	if c != nil && c.IsPresent(addr) {
		state := c.GetState(addr)
		if state == cache.StateExclusive || state == cache.StateModified {
			return c.GetData(addr), nil
		}
	}

	// Decode address
	decodeResult, err := decoder.DecodeAddress(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode address 0x%x: %w", addr, err)
	}

	// Build ReadUnique request
	reqPayload := NewCHIPayload(OpcodeReadUnique, addr)
	reqMsg := msgBuilder.NewMessage(
		ctx.TxnID(),
		OpcodeReadUnique,
		n.ID(),
		decodeResult.TargetID,
		reqPayload,
	)

	// Send and wait
	if err := ctx.Send(reqMsg); err != nil {
		return nil, err
	}

	result, err := ctx.Yield(&transaction.YieldCommand{
		Type: transaction.YieldTypeWaitForMessage,
		WaitFor: &transaction.WaitForMessage{
			Type: OpcodeCompData,
		},
		Timeout: 100 * time.Millisecond,
	})
	if err != nil {
		return nil, fmt.Errorf("ReadUnique timeout for address 0x%x: %w", addr, err)
	}

	// Extract data
	respMsg, ok := result.(*message.Message)
	if !ok {
		return nil, fmt.Errorf("invalid response type for ReadUnique")
	}

	payload, ok := respMsg.Payload.(*CHIPayload)
	if !ok {
		return nil, fmt.Errorf("invalid payload type in CompData response")
	}

	// Update cache to Exclusive state
	if c != nil {
		c.SetData(addr, payload.Data)
		c.SetState(addr, cache.StateExclusive)
	}

	return payload.Data, nil
}

// ============================================================================
// HN (Home Node) Transaction Handlers
// ============================================================================

// HomeNodeReadCleanHandler handles ReadClean requests at the Home Node.
func HomeNodeReadCleanHandler(
	ctx *transaction.TxnContext,
	n node.Node,
	reqMsg *message.Message,
) error {
	payload, ok := reqMsg.Payload.(*CHIPayload)
	if !ok {
		return fmt.Errorf("invalid payload type in ReadClean request")
	}
	addr := payload.Addr

	// Get capabilities
	dir := GetCHIDirectory(n)
	msgBuilder, err := GetCHIMessageBuilder(n)
	if err != nil {
		return err
	}

	if dir == nil {
		return fmt.Errorf("directory not available at Home Node %d", n.ID())
	}

	// Check directory state
	dirState := dir.GetState(addr)

	// Simple implementation: always return data from memory
	// TODO: Handle dirty case with snoop
	data := loadDataFromMemory(addr)

	respPayload := NewCHIPayload(OpcodeCompData, addr)
	respPayload.SetData(data)

	respMsg := msgBuilder.NewMessage(
		reqMsg.TransactionID,
		OpcodeCompData,
		n.ID(),
		reqMsg.SourceNodeID,
		respPayload,
	)

	if err := ctx.Send(respMsg); err != nil {
		return err
	}

	// Update directory
	if dirState != "Shared" {
		dir.SetState(addr, "Shared")
	}
	dir.AddSharer(addr, reqMsg.SourceNodeID)

	return nil
}

// HomeNodeReadSharedHandler handles ReadShared requests at the Home Node.
func HomeNodeReadSharedHandler(
	ctx *transaction.TxnContext,
	n node.Node,
	reqMsg *message.Message,
) error {
	// Similar to ReadClean handler
	return HomeNodeReadCleanHandler(ctx, n, reqMsg)
}

// ============================================================================
// RN Snoop Handlers
// ============================================================================

// SnpSharedFwdHandler handles SnpSharedFwd at a Request Node.
func SnpSharedFwdHandler(
	ctx *transaction.TxnContext,
	n node.Node,
	snpMsg *message.Message,
) error {
	payload, ok := snpMsg.Payload.(*CHIPayload)
	if !ok {
		return fmt.Errorf("invalid payload type in SnpSharedFwd")
	}
	addr := payload.Addr

	// Get capabilities
	c := GetCHICache(n)
	msgBuilder, err := GetCHIMessageBuilder(n)
	if err != nil {
		return err
	}

	if c == nil || !c.IsPresent(addr) {
		return fmt.Errorf("SnpSharedFwd for address 0x%x but line not present", addr)
	}

	// Get data and downgrade to Shared
	data := c.GetData(addr)
	c.SetState(addr, cache.StateShared)

	// Forward data to requester
	respPayload := NewCHIPayload(OpcodeSnpRespData, addr)
	respPayload.SetData(data)
	respPayload.SetReturnInfo(payload.ReturnNID, payload.ReturnTxnID)

	respMsg := msgBuilder.NewMessage(
		snpMsg.TransactionID,
		OpcodeSnpRespData,
		n.ID(),
		payload.ReturnNID,
		respPayload,
	)

	return ctx.Send(respMsg)
}

// ============================================================================
// Helper Functions
// ============================================================================

// loadDataFromMemory simulates loading data from memory.
func loadDataFromMemory(addr uint64) []byte {
	// Placeholder: return dummy data
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(addr + uint64(i))
	}
	return data
}

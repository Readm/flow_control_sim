package chi

import (
	"fmt"
	"time"
)

// ============================================================================
// CHI Transaction Implementations
// ============================================================================
//
// Design Rules:
// - ONLY use interfaces defined in interfaces.go
// - NO imports from transaction/cache/directory/message packages
// - Complete decoupling from framework internals
//
// ============================================================================

// ============================================================================
// RN (Request Node) Transactions
// ============================================================================

// ReadCleanTxn implements a CHI ReadClean transaction from RN perspective.
//
// Flow:
// 1. Check local cache
// 2. If hit, return data
// 3. If miss, send ReadClean request to Home Node
// 4. Wait for CompData response
// 5. Update cache to Shared state
// 6. Return data
//
// Parameters:
//   - ctx: Transaction context for Yield/Resume
//   - env: Node environment (Cache, Decoder, etc.)
//   - addr: Target address
//
// Returns:
//   - []byte: Data read from address
//   - error: Any error encountered
func ReadCleanTxn(ctx TxnContext, env *NodeEnv, addr uint64) ([]byte, error) {
	// Step 1: Check local cache
	if env.Cache != nil && env.Cache.IsPresent(addr) {
		state := env.Cache.GetState(addr)
		if state != CacheStateInvalid {
			// Cache hit - return data directly
			return env.Cache.GetData(addr), nil
		}
	}

	// Step 2: Cache miss - decode address to find Home Node
	decodeResult, err := env.Decoder.DecodeAddress(addr)
	if err != nil {
		return nil, &CHIError{
			Code:    ErrCodeNonData,
			Message: fmt.Sprintf("Failed to decode address 0x%x: %v", addr, err),
		}
	}

	// Step 3: Build ReadClean request message
	reqPayload := NewCHIPayload(OpcodeReadClean, addr)
	reqMsg := env.MsgBuilder.NewMessage(
		ctx.GetTxnID(),
		OpcodeReadClean,
		ctx.GetNodeID(),
		decodeResult.HomeNodeID,
		reqPayload,
	)

	// Step 4: Send request and wait for CompData response
	result, err := ctx.Yield(NewYieldSendAndWait(
		OpcodeCompData,
		addr,
		100*time.Millisecond, // Timeout
		reqMsg,
	))
	if err != nil {
		return nil, &CHIError{
			Code:    ErrCodeNonData,
			Message: fmt.Sprintf("ReadClean timeout for address 0x%x: %v", addr, err),
		}
	}

	// Step 5: Extract data from response
	respMsg, ok := result.(Message)
	if !ok {
		return nil, &CHIError{
			Code:    ErrCodeNonData,
			Message: fmt.Sprintf("Invalid response type for ReadClean"),
		}
	}

	payload, ok := respMsg.GetPayload().(*CHIPayload)
	if !ok {
		return nil, &CHIError{
			Code:    ErrCodeNonData,
			Message: fmt.Sprintf("Invalid payload type in CompData response"),
		}
	}

	// Step 6: Update local cache to Shared state
	if env.Cache != nil {
		env.Cache.SetData(addr, payload.Data)
		env.Cache.SetState(addr, CacheStateShared)
	}

	return payload.Data, nil
}

// ReadSharedTxn implements a CHI ReadShared transaction from RN perspective.
//
// Similar to ReadClean, but may involve snooping other caches.
//
// Flow:
// 1. Check local cache
// 2. If hit, return data
// 3. If miss, send ReadShared request to Home Node
// 4. Wait for CompData response (HN may snoop other caches)
// 5. Update cache to Shared state
// 6. Return data
func ReadSharedTxn(ctx TxnContext, env *NodeEnv, addr uint64) ([]byte, error) {
	// Step 1: Check local cache
	if env.Cache != nil && env.Cache.IsPresent(addr) {
		state := env.Cache.GetState(addr)
		if state != CacheStateInvalid {
			// Cache hit - return data directly
			return env.Cache.GetData(addr), nil
		}
	}

	// Step 2: Decode address to find Home Node
	decodeResult, err := env.Decoder.DecodeAddress(addr)
	if err != nil {
		return nil, &CHIError{
			Code:    ErrCodeNonData,
			Message: fmt.Sprintf("Failed to decode address 0x%x: %v", addr, err),
		}
	}

	// Step 3: Build ReadShared request message
	reqPayload := NewCHIPayload(OpcodeReadShared, addr)
	reqMsg := env.MsgBuilder.NewMessage(
		ctx.GetTxnID(),
		OpcodeReadShared,
		ctx.GetNodeID(),
		decodeResult.HomeNodeID,
		reqPayload,
	)

	// Step 4: Send request and wait for CompData response
	result, err := ctx.Yield(NewYieldSendAndWait(
		OpcodeCompData,
		addr,
		100*time.Millisecond,
		reqMsg,
	))
	if err != nil {
		return nil, &CHIError{
			Code:    ErrCodeNonData,
			Message: fmt.Sprintf("ReadShared timeout for address 0x%x: %v", addr, err),
		}
	}

	// Step 5: Extract data from response
	respMsg, ok := result.(Message)
	if !ok {
		return nil, &CHIError{
			Code:    ErrCodeNonData,
			Message: fmt.Sprintf("Invalid response type for ReadShared"),
		}
	}

	payload, ok := respMsg.GetPayload().(*CHIPayload)
	if !ok {
		return nil, &CHIError{
			Code:    ErrCodeNonData,
			Message: fmt.Sprintf("Invalid payload type in CompData response"),
		}
	}

	// Step 6: Update local cache to Shared state
	if env.Cache != nil {
		env.Cache.SetData(addr, payload.Data)
		env.Cache.SetState(addr, CacheStateShared)
	}

	return payload.Data, nil
}

// ReadUniqueTxn implements a CHI ReadUnique transaction (for exclusive access).
//
// Flow:
// 1. Check local cache
// 2. If in Exclusive/Modified state, return data
// 3. If miss or Shared, send ReadUnique request to Home Node
// 4. Wait for CompData response
// 5. Update cache to Exclusive state
// 6. Return data
func ReadUniqueTxn(ctx TxnContext, env *NodeEnv, addr uint64) ([]byte, error) {
	// Step 1: Check local cache
	if env.Cache != nil && env.Cache.IsPresent(addr) {
		state := env.Cache.GetState(addr)
		if state == CacheStateExclusive || state == CacheStateModified {
			// Already have exclusive access
			return env.Cache.GetData(addr), nil
		}
	}

	// Step 2: Decode address
	decodeResult, err := env.Decoder.DecodeAddress(addr)
	if err != nil {
		return nil, &CHIError{
			Code:    ErrCodeNonData,
			Message: fmt.Sprintf("Failed to decode address 0x%x: %v", addr, err),
		}
	}

	// Step 3: Build ReadUnique request
	reqPayload := NewCHIPayload(OpcodeReadUnique, addr)
	reqMsg := env.MsgBuilder.NewMessage(
		ctx.GetTxnID(),
		OpcodeReadUnique,
		ctx.GetNodeID(),
		decodeResult.HomeNodeID,
		reqPayload,
	)

	// Step 4: Send request and wait for CompData
	result, err := ctx.Yield(NewYieldSendAndWait(
		OpcodeCompData,
		addr,
		100*time.Millisecond,
		reqMsg,
	))
	if err != nil {
		return nil, &CHIError{
			Code:    ErrCodeNonData,
			Message: fmt.Sprintf("ReadUnique timeout for address 0x%x: %v", addr, err),
		}
	}

	// Step 5: Extract data
	respMsg, ok := result.(Message)
	if !ok {
		return nil, &CHIError{
			Code:    ErrCodeNonData,
			Message: fmt.Sprintf("Invalid response type for ReadUnique"),
		}
	}

	payload, ok := respMsg.GetPayload().(*CHIPayload)
	if !ok {
		return nil, &CHIError{
			Code:    ErrCodeNonData,
			Message: fmt.Sprintf("Invalid payload type in CompData response"),
		}
	}

	// Step 6: Update cache to Exclusive state
	if env.Cache != nil {
		env.Cache.SetData(addr, payload.Data)
		env.Cache.SetState(addr, CacheStateExclusive)
	}

	return payload.Data, nil
}

// ============================================================================
// HN (Home Node) Transaction Handlers
// ============================================================================

// HomeNodeReadCleanHandler handles ReadClean requests at the Home Node.
//
// Flow:
// 1. Check directory state
// 2. If clean (no sharers), read from memory and send CompData
// 3. If dirty, send snoop to owner and forward data
// 4. Update directory state
func HomeNodeReadCleanHandler(ctx TxnContext, env *NodeEnv, reqMsg Message) error {
	payload, ok := reqMsg.GetPayload().(*CHIPayload)
	if !ok {
		return &CHIError{
			Code:    ErrCodeNonData,
			Message: "Invalid payload type in ReadClean request",
		}
	}
	addr := payload.Addr

	// Step 1: Check directory state
	if env.Dir == nil {
		return &CHIError{
			Code:    ErrCodeNonData,
			Message: "Directory not available at Home Node",
		}
	}

	dirState := env.Dir.GetState(addr)

	// Step 2: Handle based on directory state
	switch dirState {
	case DirStateNotPresent, DirStateShared:
		// Clean case - read from memory and send CompData
		data := loadDataFromMemory(addr) // Placeholder function

		respPayload := NewCHIPayload(OpcodeCompData, addr)
		respPayload.SetData(data)

		respMsg := env.MsgBuilder.NewMessage(
			reqMsg.GetTransactionID(),
			OpcodeCompData,
			ctx.GetNodeID(),
			reqMsg.GetSourceNodeID(),
			respPayload,
		)

		if err := ctx.Send(respMsg); err != nil {
			return err
		}

		// Update directory: add requester as sharer
		env.Dir.AddSharer(addr, reqMsg.GetSourceNodeID())
		env.Dir.SetState(addr, DirStateShared)

	case DirStateModified, DirStateExclusive:
		// Dirty case - need to snoop the owner
		owner := env.Dir.GetOwner(addr)

		// Send SnpSharedFwd to owner
		snpPayload := NewCHIPayload(OpcodeSnpSharedFwd, addr)
		snpPayload.SetReturnInfo(reqMsg.GetSourceNodeID(), int(reqMsg.GetTransactionID().TxnID))

		snpMsg := env.MsgBuilder.NewMessage(
			ctx.GetTxnID(), // New transaction for snoop
			OpcodeSnpSharedFwd,
			ctx.GetNodeID(),
			owner,
			snpPayload,
		)

		// Send snoop and wait for SnpRespData
		result, err := ctx.Yield(NewYieldSendAndWait(
			OpcodeSnpRespData,
			addr,
			100*time.Millisecond,
			snpMsg,
		))
		if err != nil {
			return &CHIError{
				Code:    ErrCodeNonData,
				Message: fmt.Sprintf("Snoop timeout for address 0x%x", addr),
			}
		}

		// Data will be forwarded directly from owner to requester
		// Update directory state
		env.Dir.AddSharer(addr, reqMsg.GetSourceNodeID())
		env.Dir.SetState(addr, DirStateShared)
		env.Dir.SetOwner(addr, -1) // Clear owner

		_ = result // SnpRespData received
	}

	return nil
}

// HomeNodeReadSharedHandler handles ReadShared requests at the Home Node.
// Similar to ReadClean handler.
func HomeNodeReadSharedHandler(ctx TxnContext, env *NodeEnv, reqMsg Message) error {
	// Implementation similar to ReadClean
	return HomeNodeReadCleanHandler(ctx, env, reqMsg)
}

// ============================================================================
// RN Snoop Handlers
// ============================================================================

// SnpSharedFwdHandler handles SnpSharedFwd at a Request Node.
//
// Flow:
// 1. Check if we have the data
// 2. If yes, downgrade to Shared and forward data to requester
// 3. Send SnpRespData with data
func SnpSharedFwdHandler(ctx TxnContext, env *NodeEnv, snpMsg Message) error {
	payload, ok := snpMsg.GetPayload().(*CHIPayload)
	if !ok {
		return &CHIError{
			Code:    ErrCodeNonData,
			Message: "Invalid payload type in SnpSharedFwd",
		}
	}
	addr := payload.Addr

	// Step 1: Check cache
	if env.Cache == nil || !env.Cache.IsPresent(addr) {
		// We don't have the data - should not happen
		return &CHIError{
			Code:    ErrCodeNonData,
			Message: fmt.Sprintf("SnpSharedFwd for address 0x%x but line not present", addr),
		}
	}

	// Step 2: Get data and downgrade to Shared
	data := env.Cache.GetData(addr)
	env.Cache.SetState(addr, CacheStateShared)

	// Step 3: Forward data to requester (via ReturnNID)
	respPayload := NewCHIPayload(OpcodeSnpRespData, addr)
	respPayload.SetData(data)
	respPayload.SetReturnInfo(payload.ReturnNID, payload.ReturnTxnID)

	respMsg := env.MsgBuilder.NewMessage(
		snpMsg.GetTransactionID(),
		OpcodeSnpRespData,
		ctx.GetNodeID(),
		payload.ReturnNID, // Forward to original requester
		respPayload,
	)

	return ctx.Send(respMsg)
}

// ============================================================================
// Helper Functions (Placeholders)
// ============================================================================

// loadDataFromMemory simulates loading data from memory.
// In production, this would be replaced with actual memory access.
func loadDataFromMemory(addr uint64) []byte {
	// Placeholder: return dummy data
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(addr + uint64(i))
	}
	return data
}

// ============================================================================
// Transaction Registry
// ============================================================================

// TransactionRegistry maps transaction types to their implementations.
var TransactionRegistry = map[int]TransactionFunc{
	OpcodeReadClean:  ReadCleanTxn,
	OpcodeReadShared: ReadSharedTxn,
	OpcodeReadUnique: ReadUniqueTxn,
}

// HandlerRegistry maps message types to their handlers.
var HandlerRegistry = map[int]TransactionHandler{
	OpcodeReadClean:    HomeNodeReadCleanHandler,
	OpcodeReadShared:   HomeNodeReadSharedHandler,
	OpcodeSnpSharedFwd: SnpSharedFwdHandler,
}

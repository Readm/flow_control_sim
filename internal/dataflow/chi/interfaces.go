package chi

// ============================================================================
// CHI Protocol - Framework Integration Documentation
// ============================================================================
//
// The CHI (Coherence Hub Interface) protocol implementation is built on top
// of the flow simulation framework using a decoupled design.
//
// ## Architecture Overview
//
// CHI transactions directly use framework interfaces without any adapter layer:
//
//   - cache.Cache           - Cache operations with snoop handling
//   - directory.Directory   - Directory management with writeback detection
//   - decoder.Decoder       - Address decoding to find target nodes
//   - node.Node             - Node representation with protocol-agnostic data storage
//   - message.Message       - Protocol messages
//   - transaction.TxnContext - Transaction execution context
//
// ## Node Configuration Pattern
//
// CHI uses the Node.data map to store protocol-specific information without
// coupling the framework to CHI:
//
//   Example:
//     node.SetData("CHI_Role", "RN")                    // Node role
//     node.SetData("CHI_Decoder", decoderInstance)      // Address decoder
//     node.SetData("CHI_MessageBuilder", builderInstance) // Message builder
//
//   Access via helper functions:
//     role, err := GetCHIRole(node)
//     decoder, err := GetCHIDecoder(node)
//     cache := GetCHICache(node)
//
// ## CHI-Specific Types
//
// CHI protocol-specific types are defined in separate files:
//
//   - constants.go  - CHI opcodes (OpcodeReadClean, OpcodeCompData, etc.)
//   - types.go      - CHIPayload structure for message payloads
//   - decoder.go    - CHI decoder implementations (StaticDecoder, HashDecoder)
//   - node_helper.go - Helper functions for Node.data access
//   - message_builder.go - Message creation helper
//   - transactions.go - CHI transaction implementations
//
// ## Transaction Implementation Pattern
//
// CHI transactions follow this pattern:
//
//   func ReadCleanTxn(ctx *transaction.TxnContext, n *node.Node, addr uint64) ([]byte, error) {
//       // 1. Get CHI capabilities from node
//       c := GetCHICache(n)
//       decoder, _ := GetCHIDecoder(n)
//       msgBuilder, _ := GetCHIMessageBuilder(n)
//
//       // 2. Check local cache using framework interface
//       if c != nil && c.IsPresent(addr) {
//           state := c.GetState(addr)
//           if state != cache.StateInvalid {
//               return c.GetData(addr), nil
//           }
//       }
//
//       // 3. Decode address to find Home Node
//       decodeResult, _ := decoder.DecodeAddress(addr)
//
//       // 4. Build and send request message
//       reqPayload := NewCHIPayload(OpcodeReadClean, addr)
//       reqMsg := msgBuilder.NewMessage(ctx.TxnID(), OpcodeReadClean, n.ID(), decodeResult.TargetID, reqPayload)
//       ctx.Send(reqMsg)
//
//       // 5. Wait for response
//       result, _ := ctx.Yield(&transaction.YieldCommand{...})
//
//       // 6. Update cache and return data
//       respMsg := result.(*message.Message)
//       payload := respMsg.Payload.(*CHIPayload)
//       c.SetData(addr, payload.Data)
//       c.SetState(addr, cache.StateShared)
//       return payload.Data, nil
//   }
//
// ## Design Benefits
//
// This design provides:
//
//   - Zero coupling: Framework remains protocol-agnostic
//   - Multiple protocols: Can support CHI, AXI, CXL simultaneously
//   - Type safety: Helper functions provide compile-time type checking
//   - Flexibility: Easy to extend with new protocol features
//
// ============================================================================

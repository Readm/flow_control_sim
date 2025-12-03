package transaction

import (
	"context"
	"errors"
	"time"

	"github.com/Readm/flow_sim/internal/core/capability/cache"
	"github.com/Readm/flow_sim/internal/core/capability/decoder"
	"github.com/Readm/flow_sim/internal/core/capability/directory"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/dataflow"
	"github.com/Readm/flow_sim/internal/dataflow/message"
)

// TxnContext provides the context for a Transaction to interact with TxnManager.
type TxnContext struct {
	yieldCh  chan *YieldCommand
	resumeCh chan interface{}
	ctx      context.Context
	nodeID   int
	txnID    dataflow.TransactionID

	// nodeAccessor provides access to node resources.
	// This enables both segmented and continuous transaction styles.
	nodeAccessor NodeAccessor
}

// NewTxnContext creates a new TxnContext with a NodeAccessor.
// This constructor supports the unified transaction framework.
func NewTxnContext(nodeID int, txnID dataflow.TransactionID, yieldCh chan *YieldCommand, resumeCh chan interface{}, ctx context.Context, nodeAccessor NodeAccessor) *TxnContext {
	return &TxnContext{
		yieldCh:      yieldCh,
		resumeCh:     resumeCh,
		ctx:          ctx,
		nodeID:       nodeID,
		txnID:        txnID,
		nodeAccessor: nodeAccessor,
	}
}

// Yield sends a yield command to TxnManager and waits for a resume value.
func (tc *TxnContext) Yield(cmd *YieldCommand) (interface{}, error) {
	if cmd == nil {
		return nil, errors.New("yield command cannot be nil")
	}

	// Send yield command (non-blocking, should have buffer)
	select {
	case tc.yieldCh <- cmd:
	case <-tc.ctx.Done():
		return nil, tc.ctx.Err()
	default:
		return nil, errors.New("yield channel is full")
	}

	// Wait for resume value with timeout support
	if cmd.Timeout > 0 {
		select {
		case val := <-tc.resumeCh:
			return val, nil
		case <-time.After(cmd.Timeout):
			return nil, errors.New("yield timeout")
		case <-tc.ctx.Done():
			return nil, tc.ctx.Err()
		}
	} else {
		select {
		case val := <-tc.resumeCh:
			return val, nil
		case <-tc.ctx.Done():
			return nil, tc.ctx.Err()
		}
	}
}

// Send queues a message to be sent by TxnManager in the next Tick.
func (tc *TxnContext) Send(msg *message.Message) error {
	if msg == nil {
		return errors.New("message cannot be nil")
	}

	// Set TransactionID if not set
	if msg.TransactionID.NodeID == 0 && msg.TransactionID.TxnID == 0 {
		msg.TransactionID = tc.txnID
	}

	// Send via yield command with SendQueue
	// Use YieldTypeSendOnly since we're only sending, not waiting for a response
	cmd := &YieldCommand{
		Type:      YieldTypeSendOnly,
		SendQueue: []*message.Message{msg},
	}

	select {
	case tc.yieldCh <- cmd:
		return nil
	case <-tc.ctx.Done():
		return tc.ctx.Err()
	default:
		return errors.New("yield channel is full")
	}
}

// NodeID returns the node ID.
func (tc *TxnContext) NodeID() int {
	return tc.nodeID
}

// TxnID returns the transaction ID.
func (tc *TxnContext) TxnID() dataflow.TransactionID {
	return tc.txnID
}

// Complete signals that the transaction is complete.
func (tc *TxnContext) Complete(result interface{}) error {
	cmd := &YieldCommand{
		Type: YieldTypeComplete,
	}
	// Send complete command
	select {
	case tc.yieldCh <- cmd:
		// Optionally send result via resumeCh if needed
		if result != nil {
			select {
			case tc.resumeCh <- result:
			default:
			}
		}
		return nil
	case <-tc.ctx.Done():
		return tc.ctx.Err()
	default:
		return errors.New("yield channel is full")
	}
}

// ===========================================================================
// Unified Framework Support: Migration and Resource Access
// ===========================================================================

// MigrateTo migrates the transaction to another node.
// This enables continuous-style transactions where the same goroutine
// continues executing on a different node.
//
// The method:
// 1. Yields with YieldTypeMigrateTo
// 2. Waits for the target node's TxnManager to resume the transaction
// 3. Returns a new TxnContext with the target node's NodeAccessor
func (tc *TxnContext) MigrateTo(targetNodeID int) (*TxnContext, error) {
	// Construct migration request
	migrateCmd := &YieldCommand{
		Type:            YieldTypeMigrateTo,
		MigrateToNodeID: targetNodeID,
	}

	// Yield and wait for migration to complete
	resumeVal, err := tc.Yield(migrateCmd)
	if err != nil {
		return nil, err
	}

	// Extract migration result
	migResult, ok := resumeVal.(*MigrationResult)
	if !ok {
		return nil, errors.New("invalid migration result type")
	}

	// Create new context for the target node
	newCtx := &TxnContext{
		yieldCh:      tc.yieldCh,   // Reuse channels
		resumeCh:     tc.resumeCh,
		ctx:          tc.ctx,
		txnID:        tc.txnID,
		nodeID:       targetNodeID,
		nodeAccessor: migResult.NodeAccessor,  // New node's accessor
	}

	return newCtx, nil
}

// GetCache returns the Cache capability of the current node.
// This method works with the unified framework's NodeAccessor.
func (tc *TxnContext) GetCache() cache.Cache {
	if tc.nodeAccessor != nil {
		return tc.nodeAccessor.GetCache()
	}
	return nil
}

// GetDirectory returns the Directory capability of the current node.
// This method works with the unified framework's NodeAccessor.
func (tc *TxnContext) GetDirectory() directory.Directory {
	if tc.nodeAccessor != nil {
		return tc.nodeAccessor.GetDirectory()
	}
	return nil
}

// GetDecoder returns the Decoder capability of the current node.
// This method works with the unified framework's NodeAccessor.
func (tc *TxnContext) GetDecoder() decoder.Decoder {
	if tc.nodeAccessor != nil {
		return tc.nodeAccessor.GetDecoder()
	}
	return nil
}

// GetNode returns the underlying Node object for the current node.
// This method is provided for compatibility with existing code.
func (tc *TxnContext) GetNode() *node.Node {
	if tc.nodeAccessor != nil {
		return tc.nodeAccessor.GetNode()
	}
	return nil
}


package transaction

import (
	"context"
	"errors"
	"time"

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
	nodeCtx  NodeCtx
}

// NewTxnContext creates a new TxnContext.
func NewTxnContext(nodeID int, txnID dataflow.TransactionID, yieldCh chan *YieldCommand, resumeCh chan interface{}, ctx context.Context, nodeCtx NodeCtx) *TxnContext {
	return &TxnContext{
		yieldCh:  yieldCh,
		resumeCh: resumeCh,
		ctx:      ctx,
		nodeID:   nodeID,
		txnID:    txnID,
		nodeCtx:  nodeCtx,
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

// GetCacheState gets the cache state for an address (via NodeCtx).
func (tc *TxnContext) GetCacheState(addr Addr) string {
	if tc.nodeCtx != nil {
		return tc.nodeCtx.GetCacheState(addr)
	}
	return "Invalid" // Default state
}

// ReadCache reads data from cache (via NodeCtx).
func (tc *TxnContext) ReadCache(addr Addr) []byte {
	if tc.nodeCtx != nil {
		return tc.nodeCtx.ReadCache(addr)
	}
	return nil
}

// UpdateCache updates cache state (via NodeCtx).
func (tc *TxnContext) UpdateCache(addr Addr, state string, data []byte) {
	if tc.nodeCtx != nil {
		tc.nodeCtx.UpdateCache(addr, state, data)
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


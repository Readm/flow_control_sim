package transaction

import (
	"context"
	"errors"
	"time"

	capcache "github.com/Readm/flow_sim/internal/core/capability/cache"
	capdir "github.com/Readm/flow_sim/internal/core/capability/directory"
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
	caps     CapabilityProvider

	pendingOps []Operation
}

// NewTxnContext creates a new TxnContext.
func NewTxnContext(nodeID int, txnID dataflow.TransactionID, yieldCh chan *YieldCommand, resumeCh chan interface{}, ctx context.Context, caps CapabilityProvider) *TxnContext {
	return &TxnContext{
		yieldCh:  yieldCh,
		resumeCh: resumeCh,
		ctx:      ctx,
		nodeID:   nodeID,
		txnID:    txnID,
		caps:     caps,
	}
}

// Yield sends a yield command to TxnManager and waits for a resume value.
func (tc *TxnContext) Yield(cmd *YieldCommand) (interface{}, error) {
	if cmd == nil {
		return nil, errors.New("yield command cannot be nil")
	}

	tc.attachPendingOperations(cmd)

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
	tc.attachPendingOperations(cmd)

	select {
	case tc.yieldCh <- cmd:
		return nil
	case <-tc.ctx.Done():
		return tc.ctx.Err()
	default:
		return errors.New("yield channel is full")
	}
}

// GetCacheState gets the cache state for an address via capability provider.
func (tc *TxnContext) GetCacheState(addr Addr) capcache.State {
	if tc.caps == nil || tc.caps.Cache() == nil {
		return capcache.StateInvalid
	}
	return tc.caps.Cache().GetState(uint64(addr))
}

// ReadCache reads data from cache via capability provider.
func (tc *TxnContext) ReadCache(addr Addr) []byte {
	if tc.caps == nil || tc.caps.Cache() == nil {
		return nil
	}
	return tc.caps.Cache().GetData(uint64(addr))
}

// UpdateCache schedules a cache update operation.
func (tc *TxnContext) UpdateCache(addr Addr, state capcache.State, data []byte) {
	op := &CacheOperation{
		Addr:     addr,
		NewState: state,
	}
	if data != nil {
		copyData := make([]byte, len(data))
		copy(copyData, data)
		op.Data = copyData
	}
	tc.pendingOps = append(tc.pendingOps, op)
}

// InvalidateCache schedules a cache invalidation.
func (tc *TxnContext) InvalidateCache(addr Addr) {
	tc.pendingOps = append(tc.pendingOps, &CacheOperation{
		Addr:       addr,
		Invalidate: true,
	})
}

// GetDirectoryState returns the directory state for an address.
func (tc *TxnContext) GetDirectoryState(addr Addr) capdir.State {
	if tc.caps == nil || tc.caps.Directory() == nil {
		return capdir.StateNotPresent
	}
	return tc.caps.Directory().GetState(uint64(addr))
}

// GetDirectorySharers returns the list of sharers for an address.
func (tc *TxnContext) GetDirectorySharers(addr Addr) []int {
	if tc.caps == nil || tc.caps.Directory() == nil {
		return nil
	}
	return tc.caps.Directory().GetSharers(uint64(addr))
}

// AddDirectorySharer schedules adding a sharer.
func (tc *TxnContext) AddDirectorySharer(addr Addr, nodeID int) {
	tc.pendingOps = append(tc.pendingOps, &DirectoryOperation{
		Addr:   addr,
		Type:   DirectoryOpAddSharer,
		Sharer: nodeID,
	})
}

// RemoveDirectorySharer schedules removing a sharer.
func (tc *TxnContext) RemoveDirectorySharer(addr Addr, nodeID int) {
	tc.pendingOps = append(tc.pendingOps, &DirectoryOperation{
		Addr:   addr,
		Type:   DirectoryOpRemoveSharer,
		Sharer: nodeID,
	})
}

// SetDirectoryState schedules a directory state update.
func (tc *TxnContext) SetDirectoryState(addr Addr, state capdir.State) {
	tc.pendingOps = append(tc.pendingOps, &DirectoryOperation{
		Addr:  addr,
		Type:  DirectoryOpSetState,
		State: state,
	})
}

// SetDirectoryOwner schedules setting the directory owner.
func (tc *TxnContext) SetDirectoryOwner(addr Addr, owner int) {
	tc.pendingOps = append(tc.pendingOps, &DirectoryOperation{
		Addr:  addr,
		Type:  DirectoryOpSetOwner,
		Owner: owner,
	})
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
	tc.attachPendingOperations(cmd)
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

func (tc *TxnContext) attachPendingOperations(cmd *YieldCommand) {
	if len(tc.pendingOps) == 0 {
		return
	}
	cmd.Operations = append(cmd.Operations, tc.pendingOps...)
	tc.pendingOps = nil
}

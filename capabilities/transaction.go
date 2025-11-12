package capabilities

import (
	"fmt"

	"flow_sim/core"
	"flow_sim/hooks"
)

// TxRequestParams describes the information needed to build a request packet.
type TxRequestParams struct {
	Cycle           int
	SrcID           int
	MasterID        int
	DstID           int
	TransactionType core.CHITransactionType
	Address         uint64
	DataSize        int
	ParentPacketID  int64
}

// TransactionCreator builds request packets along with transactions.
type TransactionCreator func(params TxRequestParams) (*core.Packet, *core.Transaction, error)

// TransactionCapability exposes a creator function for request nodes.
type TransactionCapability interface {
	NodeCapability
	Creator() TransactionCreator
}

type transactionCapability struct {
	name        string
	description string
	creator     TransactionCreator
}

// NewTransactionCapability wraps a creator function into a NodeCapability.
func NewTransactionCapability(name string, creator TransactionCreator) TransactionCapability {
	if creator == nil {
		creator = func(params TxRequestParams) (*core.Packet, *core.Transaction, error) {
			return nil, nil, fmt.Errorf("transaction creator not configured")
		}
	}
	return &transactionCapability{
		name:        name,
		description: "transaction capability",
		creator:     creator,
	}
}

// NewDefaultTransactionCapability builds a capability using allocator/transaction providers.
func NewDefaultTransactionCapability(
	name string,
	allocate func() (int64, error),
	createTxn func(core.CHITransactionType, uint64, int) *core.Transaction,
) TransactionCapability {
	creator := func(params TxRequestParams) (*core.Packet, *core.Transaction, error) {
		if allocate == nil {
			return nil, nil, fmt.Errorf("packet allocator not configured")
		}
		packetID, err := allocate()
		if err != nil {
			return nil, nil, err
		}

		var txn *core.Transaction
		var txnID int64
		if createTxn != nil {
			txn = createTxn(params.TransactionType, params.Address, params.Cycle)
			if txn != nil && txn.Context != nil {
				txnID = txn.Context.TransactionID
			}
		}

		packet := &core.Packet{
			ID:              packetID,
			Type:            "request",
			SrcID:           params.SrcID,
			DstID:           params.DstID,
			GeneratedAt:     params.Cycle,
			SentAt:          0,
			MasterID:        params.MasterID,
			RequestID:       packetID,
			TransactionType: params.TransactionType,
			MessageType:     core.CHIMsgReq,
			Address:         params.Address,
			DataSize:        params.DataSize,
			TransactionID:   txnID,
			ParentPacketID:  params.ParentPacketID,
		}

		return packet, txn, nil
	}
	return NewTransactionCapability(name, creator)
}

func (c *transactionCapability) Descriptor() hooks.PluginDescriptor {
	return hooks.PluginDescriptor{
		Name:        c.name,
		Category:    hooks.PluginCategoryCapability,
		Description: c.description,
	}
}

func (c *transactionCapability) Register(broker *hooks.PluginBroker) error {
	if broker == nil {
		return nil
	}
	broker.RegisterPluginMetadata(c.Descriptor())
	return nil
}

func (c *transactionCapability) Creator() TransactionCreator {
	return c.creator
}

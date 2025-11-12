package main

import (
	"github.com/Readm/flow_sim/capabilities"
	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/hooks"
)

// TxFactory is responsible for creating transactions and initial request packets.
type TxFactory struct {
	broker    *hooks.PluginBroker
	txnMgr    *TransactionManager
	packetIDs *PacketIDAllocator
}

// NewTxFactory constructs a TxFactory with required dependencies.
func NewTxFactory(broker *hooks.PluginBroker, txnMgr *TransactionManager, packetIDs *PacketIDAllocator) *TxFactory {
	return &TxFactory{
		broker:    broker,
		txnMgr:    txnMgr,
		packetIDs: packetIDs,
	}
}

// CreateRequest builds a request packet, registers the transaction, and triggers hooks.
func (f *TxFactory) CreateRequest(params capabilities.TxRequestParams) (*core.Packet, *core.Transaction) {
	if f == nil || f.txnMgr == nil || f.packetIDs == nil {
		return nil, nil
	}

	dataSize := params.DataSize
	if dataSize == 0 {
		dataSize = DefaultCacheLineSize
	}

	txn := f.txnMgr.CreateTransaction(params.TransactionType, params.Address, params.Cycle)
	if txn == nil || txn.Context == nil {
		return nil, nil
	}

	packetID := f.packetIDs.Allocate()

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
		DataSize:        dataSize,
		TransactionID:   txn.Context.TransactionID,
		ParentPacketID:  params.ParentPacketID,
	}

	if f.broker != nil {
		ctx := &hooks.TxCreatedContext{
			Packet:      packet,
			Transaction: txn,
		}
		if err := f.broker.EmitTxCreated(ctx); err != nil {
			GetLogger().Warnf("TxFactory.OnTxCreated hook failed: %v", err)
		}
	}

	return packet, txn
}

package capabilities

import (
	"fmt"
	"testing"

	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/hooks"
)

func TestTransactionCapabilityWrapsCreator(t *testing.T) {
	called := false
	var captured TxRequestParams
	cap := NewTransactionCapability("txn-test", func(params TxRequestParams) (*core.Packet, *core.Transaction, error) {
		called = true
		captured = params
		return &core.Packet{ID: 1}, &core.Transaction{}, nil
	})

	broker := hooks.NewPluginBroker()
	if err := cap.Register(broker); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	params := TxRequestParams{
		Cycle:           10,
		SrcID:           1,
		MasterID:        1,
		DstID:           2,
		TransactionType: core.CHITxnReadNoSnp,
		Address:         0x1234,
		DataSize:        64,
	}
	packet, txn, err := cap.Creator()(params)
	if err != nil {
		t.Fatalf("Creator returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected creator to be called")
	}
	if packet == nil || packet.ID != 1 {
		t.Fatalf("unexpected packet: %+v", packet)
	}
	if txn == nil {
		t.Fatalf("expected transaction result")
	}
	if captured.Address != params.Address {
		t.Fatalf("expected address to propagate")
	}
}

func TestTransactionCapabilityDefaultError(t *testing.T) {
	cap := NewTransactionCapability("txn-default", nil)
	_, _, err := cap.Creator()(TxRequestParams{})
	if err == nil {
		t.Fatalf("expected error when creator is missing")
	}
	if err.Error() != "transaction creator not configured" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultTransactionCapabilityUsesProviders(t *testing.T) {
	allocatorCalled := false
	transactionCalled := false

	cap := NewDefaultTransactionCapability(
		"txn-default",
		func() (int64, error) {
			allocatorCalled = true
			return 42, nil
		},
		func(txType core.CHITransactionType, addr uint64, cycle int) *core.Transaction {
			transactionCalled = true
			return &core.Transaction{
				Context: &core.TransactionContext{
					TransactionID: 99,
				},
			}
		},
	)

	params := TxRequestParams{
		Cycle:           10,
		SrcID:           1,
		MasterID:        1,
		DstID:           2,
		TransactionType: core.CHITxnReadNoSnp,
		Address:         0x2000,
		DataSize:        64,
	}
	packet, txn, err := cap.Creator()(params)
	if err != nil {
		t.Fatalf("Creator returned error: %v", err)
	}
	if !allocatorCalled || !transactionCalled {
		t.Fatalf("expected allocator and transaction providers to be called")
	}
	if packet == nil || packet.ID != 42 {
		t.Fatalf("unexpected packet: %+v", packet)
	}
	if txn == nil || txn.Context.TransactionID != 99 {
		t.Fatalf("unexpected transaction: %+v", txn)
	}
}

func TestDefaultTransactionCapabilityAllocatorError(t *testing.T) {
	cap := NewDefaultTransactionCapability(
		"txn-default-error",
		func() (int64, error) {
			return 0, fmt.Errorf("alloc fail")
		},
		nil,
	)
	_, _, err := cap.Creator()(TxRequestParams{})
	if err == nil || err.Error() != "alloc fail" {
		t.Fatalf("expected allocator error, got %v", err)
	}
}

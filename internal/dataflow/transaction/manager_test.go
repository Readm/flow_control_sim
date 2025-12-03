package transaction

import (
	"context"
	"testing"
	"time"

	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/dataflow/message"
)

// TestSegmentedTransaction tests the traditional segmented transaction style
// where the transaction goroutine stays on the originating node.
func TestSegmentedTransaction(t *testing.T) {
	// Create two nodes: RN (node 1) and HN (node 2)
	rn := node.New(1)
	hn := node.New(2)

	// Create TxnManagers for both nodes
	rnManager := NewTxnManager(rn)
	hnManager := NewTxnManager(hn)

	// Channel to signal completion
	done := make(chan bool, 1)
	var receivedData []byte

	// Start segmented transaction on RN
	txnID := rnManager.Start(context.Background(), func(ctx *TxnContext) {
		// Send request to HN
		reqMsg := &message.Message{
			TransactionID: ctx.TxnID(),
			Type:          100, // Mock opcode for ReadRequest
			SourceNodeID:  ctx.NodeID(),
			TargetNodeID:  2,
			Payload:       map[string]interface{}{"addr": uint64(0x1000)},
		}

		err := ctx.Send(reqMsg)
		if err != nil {
			t.Errorf("Failed to send request: %v", err)
			done <- false
			return
		}

		// Yield and wait for response
		resp, err := ctx.Yield(&YieldCommand{
			Type: YieldTypeWaitForMessage,
			WaitFor: &WaitForMessage{
				Type: 101, // Mock opcode for ReadResponse
			},
			Timeout: 100 * time.Millisecond,
		})

		if err != nil {
			t.Errorf("Yield failed: %v", err)
			done <- false
			return
		}

		respMsg := resp.(*message.Message)
		payload := respMsg.Payload.(map[string]interface{})
		receivedData = payload["data"].([]byte)

		done <- true
	})

	// Give transaction goroutine time to execute and send
	time.Sleep(10 * time.Millisecond)

	// Tick 1: Process Send() command (YieldTypeSendOnly)
	outgoing1, err := rnManager.Tick(1, nil)
	if err != nil {
		t.Fatalf("RN Tick 1 failed: %v", err)
	}

	// Should have one outgoing message (request to HN)
	if len(outgoing1) != 1 {
		t.Fatalf("Expected 1 outgoing message from Tick 1, got %d", len(outgoing1))
	}

	// Tick 2: Process Yield() command (YieldTypeWaitForMessage)
	// This sets up the waiting state but doesn't produce outgoing messages
	_, err = rnManager.Tick(2, nil)
	if err != nil {
		t.Fatalf("RN Tick 2 failed: %v", err)
	}

	outgoing := outgoing1

	// Simulate tick on HN - receive request and send response
	respMsg := &message.Message{
		TransactionID: txnID,
		Type:          101, // ReadResponse
		SourceNodeID:  2,
		TargetNodeID:  1,
		Payload:       map[string]interface{}{"data": []byte{0xAA, 0xBB}},
	}
	_, err = hnManager.Tick(2, outgoing)
	if err != nil {
		t.Fatalf("HN Tick failed: %v", err)
	}

	// Simulate tick on RN - receive response and resume transaction
	_, err = rnManager.Tick(3, []*message.Message{respMsg})
	if err != nil {
		t.Fatalf("RN Tick (resume) failed: %v", err)
	}

	// Wait for transaction to complete
	select {
	case success := <-done:
		if !success {
			t.Fatal("Transaction failed")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Transaction timed out")
	}

	// Verify received data
	if len(receivedData) != 2 || receivedData[0] != 0xAA || receivedData[1] != 0xBB {
		t.Errorf("Unexpected data: %v", receivedData)
	}
}

// TestContinuousTransaction tests the new continuous transaction style
// where the transaction migrates across nodes.
func TestContinuousTransaction(t *testing.T) {
	// Create two nodes: RN (node 1) and HN (node 2)
	rn := node.New(1)
	hn := node.New(2)

	// Create TxnManagers
	rnManager := NewTxnManager(rn)
	hnManager := NewTxnManager(hn)

	// Track execution
	done := make(chan bool, 1)
	executionLog := []string{}

	// Start continuous transaction on RN
	rnManager.Start(context.Background(), func(ctx *TxnContext) {
		// Phase 1: On RN
		if ctx.NodeID() != 1 {
			t.Errorf("Expected to start on node 1, got %d", ctx.NodeID())
			done <- false
			return
		}
		executionLog = append(executionLog, "Phase1:RN")

		// Send request and migrate to HN
		reqMsg := &message.Message{
			TransactionID: ctx.TxnID(),
			Type:          100,
			SourceNodeID:  ctx.NodeID(),
			TargetNodeID:  2,
		}
		ctx.Send(reqMsg)

		// Migrate to HN
		newCtx, err := ctx.MigrateTo(2)
		if err != nil {
			t.Errorf("Migration failed: %v", err)
			done <- false
			return
		}

		// Phase 2: On HN (same goroutine, different node)
		if newCtx.NodeID() != 2 {
			t.Errorf("Expected to be on node 2 after migration, got %d", newCtx.NodeID())
			done <- false
			return
		}
		executionLog = append(executionLog, "Phase2:HN")

		// Verify we can access HN's resources
		hnNode := newCtx.GetNode()
		if hnNode.ID() != 2 {
			t.Errorf("Expected HN node ID 2, got %d", hnNode.ID())
		}

		// Complete transaction
		newCtx.Complete(nil)
		executionLog = append(executionLog, "Complete")
		done <- true
	})

	// Give transaction time to execute
	time.Sleep(10 * time.Millisecond)

	// Simulate ticks to process migration
	// Tick 1: RN processes Send() command (YieldTypeSendOnly)
	tickOut1, err := rnManager.Tick(1, nil)
	if err != nil {
		t.Fatalf("RN Tick 1 failed: %v", err)
	}

	// Tick 2: RN processes MigrateTo() command (YieldTypeMigrateTo)
	outgoing1, err := rnManager.Tick(2, nil)
	if err != nil {
		t.Fatalf("RN Tick 2 failed: %v", err)
	}

	// Should have migration request message (combined with any messages from Send())
	allOut := append(tickOut1, outgoing1...)
	if len(allOut) == 0 {
		t.Fatal("Expected at least one outgoing message")
	}

	// Find migration message
	var migMsg *message.Message
	for _, msg := range allOut {
		if msg.Type == MsgTypeMigrationRequest {
			migMsg = msg
			break
		}
	}
	if migMsg == nil {
		t.Fatal("Migration request not found")
	}

	// Tick 3: HN receives migration request
	_, err = hnManager.Tick(3, []*message.Message{migMsg})
	if err != nil {
		t.Fatalf("HN Tick 3 failed: %v", err)
	}

	// Give time for migrated transaction to execute
	time.Sleep(10 * time.Millisecond)

	// Tick 4: HN processes migrated transaction's Complete() command
	outgoing3, err := hnManager.Tick(4, nil)
	if err != nil {
		t.Fatalf("HN Tick 3 failed: %v", err)
	}

	// Wait for completion
	select {
	case success := <-done:
		if !success {
			t.Fatal("Transaction failed")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Transaction timed out")
	}

	// Verify execution sequence
	if len(executionLog) != 3 {
		t.Errorf("Expected 3 execution steps, got %d: %v", len(executionLog), executionLog)
	}
	if executionLog[0] != "Phase1:RN" {
		t.Errorf("Expected Phase1:RN, got %s", executionLog[0])
	}
	if executionLog[1] != "Phase2:HN" {
		t.Errorf("Expected Phase2:HN, got %s", executionLog[1])
	}

	// Verify transaction was removed from HN's migrated map
	if hnManager.ActiveCount() != 0 {
		t.Errorf("Expected 0 active transactions on HN, got %d", hnManager.ActiveCount())
	}

	// Verify outgoing messages from completion
	_ = outgoing3 // Complete may have messages
}

// TestBothModesCoexist tests that segmented and continuous transactions
// can run simultaneously in the same system.
func TestBothModesCoexist(t *testing.T) {
	// Create three nodes
	node1 := node.New(1)
	node2 := node.New(2)
	node3 := node.New(3)

	mgr1 := NewTxnManager(node1)
	mgr2 := NewTxnManager(node2)
	mgr3 := NewTxnManager(node3)

	segmentedDone := make(chan bool, 1)
	continuousDone := make(chan bool, 1)

	// Start segmented transaction on node 1
	mgr1.Start(context.Background(), func(ctx *TxnContext) {
		// Simple segmented: send message, wait, complete
		msg := &message.Message{
			TransactionID: ctx.TxnID(),
			Type:          200,
			SourceNodeID:  1,
			TargetNodeID:  2,
		}
		ctx.Send(msg)

		_, err := ctx.Yield(&YieldCommand{
			Type: YieldTypeWaitForMessage,
			WaitFor: &WaitForMessage{
				Type: 201,
			},
			Timeout: 100 * time.Millisecond,
		})

		if err == nil {
			ctx.Complete(nil)
			segmentedDone <- true
		} else {
			segmentedDone <- false
		}
	})

	// Start continuous transaction on node 1 (will migrate to node 3)
	mgr1.Start(context.Background(), func(ctx *TxnContext) {
		// Migrate to node 3
		newCtx, err := ctx.MigrateTo(3)
		if err != nil {
			continuousDone <- false
			return
		}

		// Do work on node 3
		if newCtx.NodeID() == 3 {
			newCtx.Complete(nil)
			continuousDone <- true
		} else {
			continuousDone <- false
		}
	})

	// Give transactions time to start
	time.Sleep(10 * time.Millisecond)

	// Simulate ticks
	// Tick 1: Node 1 processes segmented transaction's Send()
	out1, _ := mgr1.Tick(1, nil)

	// Tick 2: Node 1 processes segmented transaction's Yield() and continuous transaction's MigrateTo()
	out2, _ := mgr1.Tick(2, nil)

	// Combine outgoing messages
	out1 = append(out1, out2...)

	// Tick 3: Node 2 receives segmented request, sends response
	resp := &message.Message{
		Type:         201,
		SourceNodeID: 2,
		TargetNodeID: 1,
	}
	mgr2.Tick(3, out1)

	// Tick 4: Node 3 receives migration request
	var migrationMsg *message.Message
	for _, msg := range out1 {
		if msg.Type == MsgTypeMigrationRequest {
			migrationMsg = msg
			break
		}
	}
	if migrationMsg != nil {
		mgr3.Tick(4, []*message.Message{migrationMsg})
	}

	// Give migrated transaction time to execute
	time.Sleep(10 * time.Millisecond)

	// Tick 5: Node 1 receives response for segmented transaction
	mgr1.Tick(5, []*message.Message{resp})

	// Tick 6: Node 3 processes continuous transaction's complete
	mgr3.Tick(6, nil)

	// Wait for both to complete
	timeout := time.After(500 * time.Millisecond)

	segmentedOK := false
	continuousOK := false

	for i := 0; i < 2; i++ {
		select {
		case ok := <-segmentedDone:
			segmentedOK = ok
		case ok := <-continuousDone:
			continuousOK = ok
		case <-timeout:
			t.Fatal("Transactions timed out")
		}
	}

	if !segmentedOK {
		t.Error("Segmented transaction failed")
	}
	if !continuousOK {
		t.Error("Continuous transaction failed")
	}
}

// TestMigrationResult tests that MigrationResult provides correct NodeAccessor
func TestMigrationResult(t *testing.T) {
	n := node.New(10)
	accessor := NewLocalNodeAccessor(n)

	result := &MigrationResult{
		NodeAccessor: accessor,
		Message:      nil,
	}

	if result.NodeAccessor.NodeID() != 10 {
		t.Errorf("Expected NodeID 10, got %d", result.NodeAccessor.NodeID())
	}

	retrievedNode := result.NodeAccessor.GetNode()
	if retrievedNode.ID() != 10 {
		t.Errorf("Expected node ID 10, got %d", retrievedNode.ID())
	}
}

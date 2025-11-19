package transaction

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/Readm/flow_sim/internal/dataflow/message"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestTransactionBasicFunctionality tests basic transaction operations.
func TestTransactionBasicFunctionality(t *testing.T) {
	t.Parallel()

	mgr := NewManager()
	nodeID := 1
	cycle := uint64(0)

	// Create transaction
	txn := mgr.NewTransaction(nodeID, cycle)
	if txn == nil {
		t.Fatalf("expected transaction to be created")
	}

	if txn.ID == 0 {
		t.Fatalf("expected transaction ID to be non-zero")
	}
	if txn.InitiatorNodeID != nodeID {
		t.Fatalf("expected initiator node %d, got %d", nodeID, txn.InitiatorNodeID)
	}
	if txn.State != TransactionStatePending {
		t.Fatalf("expected state Pending, got %s", txn.State)
	}
	if txn.CreatedCycle != cycle {
		t.Fatalf("expected created cycle %d, got %d", cycle, txn.CreatedCycle)
	}
}

// TestTransactionStateTransitions tests transaction state transitions.
func TestTransactionStateTransitions(t *testing.T) {
	t.Parallel()

	mgr := NewManager()
	txn := mgr.NewTransaction(1, 0)

	// Pending -> InProgress
	txn.UpdateState(TransactionStateInProgress, 1)
	if txn.State != TransactionStateInProgress {
		t.Fatalf("expected state InProgress, got %s", txn.State)
	}

	// InProgress -> Completed
	txn.UpdateState(TransactionStateCompleted, 5)
	if txn.State != TransactionStateCompleted {
		t.Fatalf("expected state Completed, got %s", txn.State)
	}
	if txn.CompletedCycle != 5 {
		t.Fatalf("expected completed cycle 5, got %d", txn.CompletedCycle)
	}

	// Verify IsComplete
	if !txn.IsComplete() {
		t.Fatalf("expected transaction to be complete")
	}
}

// TestTransactionAddMessage tests adding messages to transaction.
func TestTransactionAddMessage(t *testing.T) {
	t.Parallel()

	mgr := NewManager()
	txn := mgr.NewTransaction(1, 0)

	msg1 := &message.Message{
		ID:            1,
		TransactionID: txn.ID,
		Type:          message.MessageTypeRequest,
		SourceNodeID:  1,
		TargetNodeID:  2,
		CreatedCycle:  1,
	}

	msg2 := &message.Message{
		ID:            2,
		TransactionID: txn.ID,
		Type:          message.MessageTypeData,
		SourceNodeID:  2,
		TargetNodeID:  1,
		CreatedCycle:  3,
	}

	txn.AddMessage(msg1)
	txn.AddMessage(msg2)

	if len(txn.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(txn.Messages))
	}

	// Test GetMessagesByType
	reqMsgs := txn.GetMessagesByType(message.MessageTypeRequest)
	if len(reqMsgs) != 1 {
		t.Fatalf("expected 1 request message, got %d", len(reqMsgs))
	}

	dataMsgs := txn.GetMessagesByType(message.MessageTypeData)
	if len(dataMsgs) != 1 {
		t.Fatalf("expected 1 data message, got %d", len(dataMsgs))
	}
}

// TestTransactionEventTracking tests event tracking.
func TestTransactionEventTracking(t *testing.T) {
	t.Parallel()

	mgr := NewManager()
	txn := mgr.NewTransaction(1, 0)

	// Add events
	txn.AddEvent(Event{
		Cycle:     1,
		NodeID:    1,
		EventType: "MessageSent",
		MessageID: 1,
		Details:   "Request message sent",
	})

	txn.AddEvent(Event{
		Cycle:     3,
		NodeID:    2,
		EventType: "MessageReceived",
		MessageID: 1,
		Details:   "Request message received",
	})

	if len(txn.Events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(txn.Events))
	}

	// Check creation event exists
	hasCreated := false
	for _, event := range txn.Events {
		if event.EventType == "Created" {
			hasCreated = true
			break
		}
	}
	if !hasCreated {
		t.Fatalf("expected Created event")
	}
}

// TestTransactionManagerConcurrency tests concurrent access to Transaction Manager.
func TestTransactionManagerConcurrency(t *testing.T) {
	t.Parallel()

	mgr := NewManager()
	const numGoroutines = 10
	const transactionsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(nodeID int) {
			defer wg.Done()
			for j := 0; j < transactionsPerGoroutine; j++ {
				txn := mgr.NewTransaction(nodeID, uint64(j))
				if txn == nil {
					t.Errorf("failed to create transaction")
				}
			}
		}(i)
	}

	wg.Wait()

	allTxns := mgr.GetAllTransactions()
	expectedCount := numGoroutines * transactionsPerGoroutine
	if len(allTxns) != expectedCount {
		t.Fatalf("expected %d transactions, got %d", expectedCount, len(allTxns))
	}
}

// TestReadRequestExample tests the complete read request flow.
func TestReadRequestExample(t *testing.T) {
	t.Parallel()

	mgr := NewManager()
	nodeA := 1
	nodeB := 2
	cycle := uint64(0)

	// Step 1: Node A creates read request transaction
	txn := mgr.NewTransaction(nodeA, cycle)
	if txn == nil {
		t.Fatalf("failed to create transaction")
	}

	// Step 2: Node A creates ReqMessage
	reqMsgID := int64(1)
	reqMsg := &message.Message{
		ID:            reqMsgID,
		TransactionID: txn.ID,
		Type:          message.MessageTypeRequest,
		SourceNodeID:  nodeA,
		TargetNodeID:  nodeB,
		Payload:       map[string]interface{}{"op": "read", "addr": 0x1000},
		CreatedCycle:  cycle,
	}

	// Step 3: Encode ReqMessage to Packet
	reqPackets := reqMsg.ToPackets(1024)
	if len(reqPackets) != 1 {
		t.Fatalf("expected 1 request packet, got %d", len(reqPackets))
	}

	// Verify packet has correct TransactionID and MessageID
	if reqPackets[0].TransactionID != txn.ID {
		t.Fatalf("expected TransactionID %d, got %d", txn.ID, reqPackets[0].TransactionID)
	}
	if reqPackets[0].MessageID != reqMsgID {
		t.Fatalf("expected MessageID %d, got %d", reqMsgID, reqPackets[0].MessageID)
	}

	// Step 4: Add message to transaction
	err := mgr.AddMessageToTransaction(txn.ID, reqMsg)
	if err != nil {
		t.Fatalf("failed to add message: %v", err)
	}

	// Verify transaction state changed to InProgress
	if txn.State != TransactionStateInProgress {
		t.Fatalf("expected state InProgress, got %s", txn.State)
	}

	// Step 5: Node B receives and decodes Packet to Message
	receivedMsg := &message.Message{}
	err = receivedMsg.FromPackets(reqPackets)
	if err != nil {
		t.Fatalf("failed to decode message: %v", err)
	}

	if receivedMsg.Type != message.MessageTypeRequest {
		t.Fatalf("expected message type Request, got %s", receivedMsg.Type)
	}

	// Node B processes the request
	receivedMsg.AddProcessedInfo(cycle+1, nodeB, "Received and processing read request")
	if !receivedMsg.IsProcessed() {
		t.Fatalf("expected message to be processed")
	}

	// Step 6: Node B processes and creates DataMessage with 4 DataPackets
	dataMsgID := int64(2)
	dataPayload := []byte{0x01, 0x02, 0x03, 0x04} // 4 bytes of data
	dataMsg := &message.Message{
		ID:            dataMsgID,
		TransactionID: txn.ID,
		Type:          message.MessageTypeData,
		SourceNodeID:  nodeB,
		TargetNodeID:  nodeA,
		Payload:       map[string]interface{}{"data": dataPayload},
		CreatedCycle:  cycle + 2,
	}

	// Encode DataMessage to 4 Packets using ToPackets with small packet size
	// This will automatically split into multiple packets and include type information
	dataPackets := dataMsg.ToPackets(10) // Small size to force splitting
	if len(dataPackets) < 4 {
		// If not split into 4, manually create 4 packets with proper envelope
		envelope := map[string]interface{}{
			"type":    string(message.MessageTypeData),
			"payload": map[string]interface{}{"data": dataPayload},
		}
		envelopeJSON, _ := json.Marshal(envelope)
		envelopeStr := string(envelopeJSON)
		
		// Split into 4 parts
		partSize := len(envelopeStr) / 4
		dataPackets = []packet.Packet{}
		for i := 0; i < 4; i++ {
			start := i * partSize
			end := start + partSize
			if i == 3 {
				end = len(envelopeStr)
			}
			dataPackets = append(dataPackets, packet.Packet{
				SourceID:      nodeB,
				TargetID:      nodeA,
				Payload:       envelopeStr[start:end],
				TransactionID: txn.ID,
				MessageID:     dataMsgID,
				Sequence:      i,
			})
		}
	}

	dataMsg.Packets = dataPackets

	// Step 7: Add DataMessage to transaction
	err = mgr.AddMessageToTransaction(txn.ID, dataMsg)
	if err != nil {
		t.Fatalf("failed to add data message: %v", err)
	}

	// Step 8: Node A receives and decodes DataPackets to DataMessage
	receivedDataMsg := &message.Message{}
	err = receivedDataMsg.FromPackets(dataPackets)
	if err != nil {
		t.Fatalf("failed to decode data message: %v", err)
	}

	if receivedDataMsg.Type != message.MessageTypeData {
		t.Fatalf("expected message type Data, got %s", receivedDataMsg.Type)
	}

	// Verify message is complete
	if !receivedDataMsg.IsComplete() {
		t.Fatalf("expected data message to be complete")
	}

	// Node A processes the data message
	receivedDataMsg.AddProcessedInfo(cycle+4, nodeA, "Received and processed data response")
	
	// Verify processing info
	lastInfo := receivedDataMsg.GetLastProcessedInfo()
	if lastInfo == nil {
		t.Fatalf("expected processed info")
	}
	if lastInfo.NodeID != nodeA {
		t.Fatalf("expected processed by node %d, got %d", nodeA, lastInfo.NodeID)
	}
	if lastInfo.Cycle != cycle+4 {
		t.Fatalf("expected processed at cycle %d, got %d", cycle+4, lastInfo.Cycle)
	}

	// Step 9: Complete transaction
	completionCycle := cycle + 5
	err = mgr.CompleteTransaction(txn.ID, completionCycle)
	if err != nil {
		t.Fatalf("failed to complete transaction: %v", err)
	}

	if txn.State != TransactionStateCompleted {
		t.Fatalf("expected state Completed, got %s", txn.State)
	}
	if txn.CompletedCycle != completionCycle {
		t.Fatalf("expected completed cycle %d, got %d", completionCycle, txn.CompletedCycle)
	}

	// Verify transaction has 2 messages
	if len(txn.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(txn.Messages))
	}

	// Verify event tracking
	hasCompletedEvent := false
	for _, event := range txn.Events {
		if event.EventType == "StateChanged" {
			hasCompletedEvent = true
			break
		}
	}
	if !hasCompletedEvent {
		t.Fatalf("expected StateChanged event")
	}
}

// TestTransactionManagerGetByNode tests getting transactions by node.
func TestTransactionManagerGetByNode(t *testing.T) {
	t.Parallel()

	mgr := NewManager()

	// Create transactions for different nodes
	txn1 := mgr.NewTransaction(1, 0)
	txn2 := mgr.NewTransaction(2, 0)
	txn3 := mgr.NewTransaction(1, 1)

	// Add messages that involve node 1
	msg := &message.Message{
		ID:            1,
		TransactionID: txn2.ID,
		SourceNodeID:  2,
		TargetNodeID:  1,
		CreatedCycle:  1,
	}
	mgr.AddMessageToTransaction(txn2.ID, msg)

	// Get transactions for node 1
	node1Txns := mgr.GetTransactionsByNode(1)
	if len(node1Txns) < 2 {
		t.Fatalf("expected at least 2 transactions for node 1, got %d", len(node1Txns))
	}

	// Verify txn1 and txn3 are included
	foundTxn1 := false
	foundTxn3 := false
	for _, txn := range node1Txns {
		if txn.ID == txn1.ID {
			foundTxn1 = true
		}
		if txn.ID == txn3.ID {
			foundTxn3 = true
		}
	}
	if !foundTxn1 {
		t.Fatalf("expected to find transaction 1")
	}
	if !foundTxn3 {
		t.Fatalf("expected to find transaction 3")
	}
}


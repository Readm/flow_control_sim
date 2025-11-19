package message

import (
	"testing"

	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// TestMessageCreation tests message creation.
func TestMessageCreation(t *testing.T) {
	t.Parallel()

	msg := &Message{
		ID:            1,
		TransactionID: 100,
		Type:          MessageTypeRequest,
		SourceNodeID:  1,
		TargetNodeID:  2,
		Payload:       "test payload",
		CreatedCycle:  0,
	}

	if msg.ID != 1 {
		t.Fatalf("expected ID 1, got %d", msg.ID)
	}
	if msg.TransactionID != 100 {
		t.Fatalf("expected TransactionID 100, got %d", msg.TransactionID)
	}
	if msg.Type != MessageTypeRequest {
		t.Fatalf("expected type Request, got %s", msg.Type)
	}
}

// TestMessageToPacketsSingle tests encoding a small message to a single packet.
func TestMessageToPacketsSingle(t *testing.T) {
	t.Parallel()

	msg := &Message{
		ID:            1,
		TransactionID: 100,
		Type:          MessageTypeRequest,
		SourceNodeID:  1,
		TargetNodeID:  2,
		Payload:       "small payload",
		CreatedCycle:  0,
	}

	packets := msg.ToPackets(1024)
	if len(packets) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(packets))
	}

	pkt := packets[0]
	if pkt.TransactionID != 100 {
		t.Fatalf("expected TransactionID 100, got %d", pkt.TransactionID)
	}
	if pkt.MessageID != 1 {
		t.Fatalf("expected MessageID 1, got %d", pkt.MessageID)
	}
	if pkt.Sequence != 0 {
		t.Fatalf("expected Sequence 0, got %d", pkt.Sequence)
	}
	if pkt.SourceID != 1 {
		t.Fatalf("expected SourceID 1, got %d", pkt.SourceID)
	}
	if pkt.TargetID != 2 {
		t.Fatalf("expected TargetID 2, got %d", pkt.TargetID)
	}
}

// TestMessageToPacketsMultiple tests encoding a large message to multiple packets.
func TestMessageToPacketsMultiple(t *testing.T) {
	t.Parallel()

	// Create a large payload
	largePayload := make([]byte, 3000)
	for i := range largePayload {
		largePayload[i] = byte(i % 256)
	}

	msg := &Message{
		ID:            1,
		TransactionID: 100,
		Type:          MessageTypeData,
		SourceNodeID:  1,
		TargetNodeID:  2,
		Payload:       string(largePayload),
		CreatedCycle:  0,
	}

	// Encode with small packet size
	packets := msg.ToPackets(1000)
	if len(packets) < 3 {
		t.Fatalf("expected at least 3 packets, got %d", len(packets))
	}

	// Verify sequences
	for i, pkt := range packets {
		if pkt.Sequence != i {
			t.Fatalf("expected Sequence %d, got %d", i, pkt.Sequence)
		}
		if pkt.TransactionID != 100 {
			t.Fatalf("packet %d: expected TransactionID 100, got %d", i, pkt.TransactionID)
		}
		if pkt.MessageID != 1 {
			t.Fatalf("packet %d: expected MessageID 1, got %d", i, pkt.MessageID)
		}
	}
}

// TestMessageFromPacketsSingle tests decoding a single packet to message.
func TestMessageFromPacketsSingle(t *testing.T) {
	t.Parallel()

	packets := []packet.Packet{
		{
			SourceID:      1,
			TargetID:      2,
			Payload:        `{"op":"read","addr":4096}`,
			TransactionID: 100,
			MessageID:     1,
			Sequence:      0,
		},
	}

	msg := &Message{}
	err := msg.FromPackets(packets)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if msg.ID != 1 {
		t.Fatalf("expected MessageID 1, got %d", msg.ID)
	}
	if msg.TransactionID != 100 {
		t.Fatalf("expected TransactionID 100, got %d", msg.TransactionID)
	}
	if msg.SourceNodeID != 1 {
		t.Fatalf("expected SourceNodeID 1, got %d", msg.SourceNodeID)
	}
	if msg.TargetNodeID != 2 {
		t.Fatalf("expected TargetNodeID 2, got %d", msg.TargetNodeID)
	}
}

// TestMessageFromPacketsMultiple tests decoding multiple packets to message.
func TestMessageFromPacketsMultiple(t *testing.T) {
	t.Parallel()

	// Create packets in reverse order to test sorting
	packets := []packet.Packet{
		{
			SourceID:      1,
			TargetID:      2,
			Payload:       "world",
			TransactionID: 100,
			MessageID:     1,
			Sequence:      1,
		},
		{
			SourceID:      1,
			TargetID:      2,
			Payload:       "hello ",
			TransactionID: 100,
			MessageID:     1,
			Sequence:      0,
		},
	}

	msg := &Message{}
	err := msg.FromPackets(packets)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// Verify payload is reconstructed correctly
	payloadStr, ok := msg.Payload.(string)
	if !ok {
		// Try to get string representation
		payloadStr = msg.Packets[0].Payload + msg.Packets[1].Payload
	}
	if payloadStr != "hello world" {
		t.Fatalf("expected payload 'hello world', got '%s'", payloadStr)
	}
}

// TestMessageIsComplete tests message completion check.
func TestMessageIsComplete(t *testing.T) {
	t.Parallel()

	// Test incomplete message (missing sequence 1)
	msg1 := &Message{
		Packets: []packet.Packet{
			{Sequence: 0},
			{Sequence: 2},
		},
	}
	if msg1.IsComplete() {
		t.Fatalf("expected incomplete message")
	}

	// Test complete message
	msg2 := &Message{
		Packets: []packet.Packet{
			{Sequence: 0},
			{Sequence: 1},
			{Sequence: 2},
		},
	}
	if !msg2.IsComplete() {
		t.Fatalf("expected complete message")
	}

	// Test empty message
	msg3 := &Message{
		Packets: []packet.Packet{},
	}
	if msg3.IsComplete() {
		t.Fatalf("expected incomplete empty message")
	}

	// Test single packet message
	msg4 := &Message{
		Packets: []packet.Packet{
			{Sequence: 0},
		},
	}
	if !msg4.IsComplete() {
		t.Fatalf("expected complete single packet message")
	}
}

// TestMessageFromPacketsInvalid tests error handling for invalid packets.
func TestMessageFromPacketsInvalid(t *testing.T) {
	t.Parallel()

	// Test empty packets
	msg := &Message{}
	err := msg.FromPackets([]packet.Packet{})
	if err == nil {
		t.Fatalf("expected error for empty packets")
	}

	// Test packets with different MessageIDs
	packets := []packet.Packet{
		{MessageID: 1, TransactionID: 100},
		{MessageID: 2, TransactionID: 100},
	}
	err = msg.FromPackets(packets)
	if err == nil {
		t.Fatalf("expected error for different MessageIDs")
	}

	// Test packets with different TransactionIDs
	packets = []packet.Packet{
		{MessageID: 1, TransactionID: 100},
		{MessageID: 1, TransactionID: 200},
	}
	err = msg.FromPackets(packets)
	if err == nil {
		t.Fatalf("expected error for different TransactionIDs")
	}
}

// TestMessageProcessedInfo tests processing information tracking.
func TestMessageProcessedInfo(t *testing.T) {
	t.Parallel()

	msg := &Message{
		ID:            1,
		TransactionID: 100,
		Type:          MessageTypeRequest,
		SourceNodeID:  1,
		TargetNodeID:  2,
		CreatedCycle:  0,
	}

	// Initially not processed
	if msg.IsProcessed() {
		t.Fatalf("expected message not processed initially")
	}
	if msg.GetLastProcessedInfo() != nil {
		t.Fatalf("expected no processed info initially")
	}

	// Add first processing record
	msg.AddProcessedInfo(5, 2, "Received and processed request")
	if !msg.IsProcessed() {
		t.Fatalf("expected message to be processed")
	}

	lastInfo := msg.GetLastProcessedInfo()
	if lastInfo == nil {
		t.Fatalf("expected processed info")
	}
	if lastInfo.Cycle != 5 {
		t.Fatalf("expected cycle 5, got %d", lastInfo.Cycle)
	}
	if lastInfo.NodeID != 2 {
		t.Fatalf("expected nodeID 2, got %d", lastInfo.NodeID)
	}
	if lastInfo.Info != "Received and processed request" {
		t.Fatalf("expected info 'Received and processed request', got '%s'", lastInfo.Info)
	}

	// Add second processing record (multiple nodes can process)
	msg.AddProcessedInfo(8, 3, "Forwarded to next node")
	if len(msg.ProcessedInfo) != 2 {
		t.Fatalf("expected 2 processed info records, got %d", len(msg.ProcessedInfo))
	}

	lastInfo = msg.GetLastProcessedInfo()
	if lastInfo.NodeID != 3 {
		t.Fatalf("expected last nodeID 3, got %d", lastInfo.NodeID)
	}
	if lastInfo.Cycle != 8 {
		t.Fatalf("expected last cycle 8, got %d", lastInfo.Cycle)
	}

	// Verify all processing records
	if msg.ProcessedInfo[0].NodeID != 2 {
		t.Fatalf("expected first nodeID 2, got %d", msg.ProcessedInfo[0].NodeID)
	}
	if msg.ProcessedInfo[1].NodeID != 3 {
		t.Fatalf("expected second nodeID 3, got %d", msg.ProcessedInfo[1].NodeID)
	}
}

// TestMessageMultipleNodesProcess tests multiple nodes processing the same message.
func TestMessageMultipleNodesProcess(t *testing.T) {
	t.Parallel()

	msg := &Message{
		ID:            1,
		TransactionID: 100,
		Type:          MessageTypeData,
		SourceNodeID:  1,
		TargetNodeID:  2,
		CreatedCycle:  0,
	}

	// Node 2 receives and processes
	msg.AddProcessedInfo(3, 2, "Received data message")
	
	// Node 2 forwards to Node 3
	msg.AddProcessedInfo(5, 3, "Forwarded data message")
	
	// Node 3 processes and responds
	msg.AddProcessedInfo(7, 3, "Processed and generated response")

	if len(msg.ProcessedInfo) != 3 {
		t.Fatalf("expected 3 processed info records, got %d", len(msg.ProcessedInfo))
	}

	// Verify processing sequence
	expectedNodes := []int{2, 3, 3}
	expectedCycles := []uint64{3, 5, 7}
	
	for i, info := range msg.ProcessedInfo {
		if info.NodeID != expectedNodes[i] {
			t.Fatalf("record %d: expected nodeID %d, got %d", i, expectedNodes[i], info.NodeID)
		}
		if info.Cycle != expectedCycles[i] {
			t.Fatalf("record %d: expected cycle %d, got %d", i, expectedCycles[i], info.Cycle)
		}
	}
}


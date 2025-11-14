package queue

import "testing"

func TestStageQueueRoundRobin(t *testing.T) {
	var enqueued []EntryID
	q := NewStageQueue("test", UnlimitedCapacity, nil, StageQueueHooks[int]{
		OnEnqueue: func(id EntryID, item int, cycle int) {
			enqueued = append(enqueued, id)
		},
	})

	for i := 0; i < 3; i++ {
		if _, ok := q.Enqueue(i, 0); !ok {
			t.Fatalf("enqueue %d failed", i)
		}
	}
	if len(enqueued) != 3 {
		t.Fatalf("expected 3 enqueues, got %d", len(enqueued))
	}

	order := make([]int, 0, 3)
	for range enqueued {
		id, item, ok := q.PeekNext()
		if !ok {
			t.Fatalf("expected ready entry")
		}
		order = append(order, item)
		if _, ok := q.Complete(id, 0); !ok {
			t.Fatalf("complete failed for %d", id)
		}
	}
	expected := []int{0, 1, 2}
	for i := range expected {
		if order[i] != expected[i] {
			t.Fatalf("round robin mismatch idx %d: got %d want %d", i, order[i], expected[i])
		}
	}
}

func TestStageQueueBlocking(t *testing.T) {
	changeCount := 0
	q := NewStageQueue("block", UnlimitedCapacity, nil, StageQueueHooks[string]{
		OnBlockChange: func(id EntryID, reason BlockIndex, blocked bool, cycle int) {
			changeCount++
		},
	})

	entry, ok := q.Enqueue("p0", 0)
	if !ok {
		t.Fatalf("enqueue failed")
	}
	idxA, err := NewBlockRegistry(0).Register("stall")
	if err != nil {
		t.Fatalf("register block idx: %v", err)
	}

	if err := q.SetBlocked(entry, idxA, true, 0); err != nil {
		t.Fatalf("set blocked: %v", err)
	}
	if changeCount != 1 {
		t.Fatalf("expected block change callback once, got %d", changeCount)
	}
	if _, _, ok := q.PeekNext(); ok {
		t.Fatalf("entry should be blocked")
	}
	if err := q.SetBlocked(entry, idxA, false, 1); err != nil {
		t.Fatalf("clear blocked: %v", err)
	}
	if changeCount != 2 {
		t.Fatalf("expected second block change callback, got %d", changeCount)
	}
	if _, item, ok := q.PeekNext(); !ok || item != "p0" {
		t.Fatalf("expected entry ready")
	}
}

func TestStageQueueResetPending(t *testing.T) {
	q := NewStageQueue("pending", UnlimitedCapacity, nil, StageQueueHooks[int]{})
	entry, ok := q.Enqueue(1, 0)
	if !ok {
		t.Fatalf("enqueue failed")
	}
	if _, _, ok := q.PeekNext(); !ok {
		t.Fatalf("entry should be ready")
	}
	// PeekNext marks pending, another peek should skip until reset.
	if _, _, ok := q.PeekNext(); ok {
		t.Fatalf("expected pending entry to block second peek")
	}
	q.ResetPending(entry)
	if _, item, ok := q.PeekNext(); !ok || item != 1 {
		t.Fatalf("expected peek after reset")
	}
}

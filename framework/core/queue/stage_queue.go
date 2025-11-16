package queue

import "fmt"

// EntryID uniquely identifies an entry in a StageQueue.
type EntryID uint64

// StageQueueHooks extends QueueHooks with block related callbacks.
type StageQueueHooks[T any] struct {
	OnEnqueue     func(entryID EntryID, item T, cycle int)
	OnDequeue     func(entryID EntryID, item T, cycle int)
	OnBlockChange func(entryID EntryID, reason BlockIndex, blocked bool, cycle int)
}

type stageEntry[T any] struct {
	id      EntryID
	item    T
	blocks  blockBitmap
	ready   bool
	pending bool
}

// StageQueue implements a queue with dynamic blocking and round-robin pick.
type StageQueue[T any] struct {
	name     string
	capacity int
	mutate   MutateFunc
	hooks    StageQueueHooks[T]
	entries  []*stageEntry[T]
	nextPick int
	nextID   EntryID
}

// NewStageQueue creates an empty StageQueue with specified capacity (use queue.UnlimitedCapacity for no limit).
func NewStageQueue[T any](name string, capacity int, mutate MutateFunc, hooks StageQueueHooks[T]) *StageQueue[T] {
	q := &StageQueue[T]{
		name:     name,
		capacity: capacity,
		mutate:   mutate,
		hooks:    hooks,
	}
	q.notify()
	return q
}

// Name returns the queue name.
func (q *StageQueue[T]) Name() string {
	if q == nil {
		return ""
	}
	return q.name
}

// Capacity returns the queue capacity (-1 for unlimited).
func (q *StageQueue[T]) Capacity() int {
	if q == nil {
		return 0
	}
	return q.capacity
}

// SetCapacity updates the queue capacity.
func (q *StageQueue[T]) SetCapacity(capacity int) {
	if q == nil {
		return
	}
	q.capacity = capacity
	q.notify()
}

// Len returns current entry count.
func (q *StageQueue[T]) Len() int {
	if q == nil {
		return 0
	}
	return len(q.entries)
}

// Enqueue inserts an item and returns the entry ID along with success flag.
func (q *StageQueue[T]) Enqueue(item T, cycle int) (EntryID, bool) {
	if q == nil {
		return 0, false
	}
	if q.capacity >= 0 && len(q.entries) >= q.capacity {
		return 0, false
	}
	entry := &stageEntry[T]{
		id:    q.nextID,
		item:  item,
		ready: true,
	}
	q.nextID++
	q.entries = append(q.entries, entry)
	q.notify()
	if q.hooks.OnEnqueue != nil {
		q.hooks.OnEnqueue(entry.id, item, cycle)
	}
	return entry.id, true
}

// Remove deletes the entry by ID.
func (q *StageQueue[T]) Remove(id EntryID, cycle int) (T, bool) {
	if q == nil {
		var zero T
		return zero, false
	}
	idx, entry := q.find(id)
	if entry == nil {
		var zero T
		return zero, false
	}
	item := entry.item
	q.removeAt(idx)
	if q.hooks.OnDequeue != nil {
		q.hooks.OnDequeue(entry.id, item, cycle)
	}
	q.notify()
	return item, true
}

// RemoveMatch deletes the first entry matching predicate.
func (q *StageQueue[T]) RemoveMatch(match func(T) bool, cycle int) (T, bool) {
	var zero T
	if q == nil || match == nil {
		return zero, false
	}
	for idx, entry := range q.entries {
		if match(entry.item) {
			item := entry.item
			q.removeAt(idx)
			if q.hooks.OnDequeue != nil {
				q.hooks.OnDequeue(entry.id, item, cycle)
			}
			q.notify()
			return item, true
		}
	}
	return zero, false
}

// PeekNext returns the next ready entry according to round robin policy without removing it.
func (q *StageQueue[T]) PeekNext() (EntryID, T, bool) {
	if q == nil || len(q.entries) == 0 {
		var zero T
		return 0, zero, false
	}
	start := q.nextPick
	count := len(q.entries)
	for i := 0; i < count; i++ {
		idx := (start + i) % count
		entry := q.entries[idx]
		if entry.ready {
			if entry.pending {
				continue
			}
			entry.pending = true
			return entry.id, entry.item, true
		}
	}
	var zero T
	return 0, zero, false
}

// Complete marks the entry as processed and removes it.
func (q *StageQueue[T]) Complete(id EntryID, cycle int) (T, bool) {
	if q == nil {
		var zero T
		return zero, false
	}
	idx, entry := q.find(id)
	if entry == nil {
		var zero T
		return zero, false
	}
	item := entry.item
	q.removeAt(idx)
	if q.hooks.OnDequeue != nil {
		q.hooks.OnDequeue(id, item, cycle)
	}
	q.notify()
	return item, true
}

// SetBlocked toggles a block bit for a specific entry.
func (q *StageQueue[T]) SetBlocked(id EntryID, reason BlockIndex, blocked bool, cycle int) error {
	if q == nil {
		return fmt.Errorf("stage queue is nil")
	}
	_, entry := q.find(id)
	if entry == nil {
		return fmt.Errorf("entry %d not found", id)
	}
	changed := false
	if blocked {
		changed = entry.blocks.set(reason)
	} else {
		changed = entry.blocks.clear(reason)
	}
	if !changed {
		return nil
	}
	wasReady := entry.ready
	entry.ready = entry.blocks.isZero()
	if !entry.ready {
		entry.pending = false
	}
	if wasReady != entry.ready && entry.ready {
		entry.pending = false
	}
	if q.hooks.OnBlockChange != nil {
		q.hooks.OnBlockChange(id, reason, blocked, cycle)
	}
	return nil
}

// ResetPending clears the pending flag for an entry (e.g. when processing failed).
func (q *StageQueue[T]) ResetPending(id EntryID) {
	if q == nil {
		return
	}
	_, entry := q.find(id)
	if entry == nil {
		return
	}
	entry.pending = false
}

// ForEach iterates over entries in enqueue order.
func (q *StageQueue[T]) ForEach(fn func(id EntryID, item T, ready bool)) {
	if q == nil || fn == nil {
		return
	}
	for _, entry := range q.entries {
		fn(entry.id, entry.item, entry.ready && !entry.pending)
	}
}

func (q *StageQueue[T]) find(id EntryID) (int, *stageEntry[T]) {
	for idx, entry := range q.entries {
		if entry.id == id {
			return idx, entry
		}
	}
	return -1, nil
}

func (q *StageQueue[T]) removeAt(idx int) {
	last := len(q.entries) - 1
	if idx < 0 || idx > last {
		return
	}
	if idx == last {
		q.entries = q.entries[:last]
	} else {
		copy(q.entries[idx:], q.entries[idx+1:])
		q.entries = q.entries[:last]
	}
	if len(q.entries) == 0 {
		q.nextPick = 0
	} else if q.nextPick >= len(q.entries) {
		q.nextPick = q.nextPick % len(q.entries)
	}
}

func (q *StageQueue[T]) notify() {
	if q == nil || q.mutate == nil {
		return
	}
	q.mutate(len(q.entries), q.capacity)
}

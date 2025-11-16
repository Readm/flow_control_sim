package queue

const UnlimitedCapacity = -1

// MutateFunc is invoked after queue length or capacity changes.
type MutateFunc func(length int, capacity int)

// QueueHooks defines callbacks for queue lifecycle events.
type QueueHooks[T any] struct {
	OnEnqueue func(item T, cycle int)
	OnDequeue func(item T, cycle int)
}

// TrackedQueue maintains items with length/capacity bookkeeping and hooks.
type TrackedQueue[T any] struct {
	name     string
	capacity int
	items    []T
	hooks    QueueHooks[T]
	mutate   MutateFunc
}

// NewTrackedQueue constructs a tracked queue with optional hooks and mutate callback.
func NewTrackedQueue[T any](name string, capacity int, mutate MutateFunc, hooks QueueHooks[T]) *TrackedQueue[T] {
	q := &TrackedQueue[T]{
		name:     name,
		capacity: capacity,
		hooks:    hooks,
		mutate:   mutate,
	}
	q.notify()
	return q
}

// Name returns the queue name.
func (q *TrackedQueue[T]) Name() string {
	if q == nil {
		return ""
	}
	return q.name
}

// Capacity returns current capacity (-1 for unlimited).
func (q *TrackedQueue[T]) Capacity() int {
	if q == nil {
		return 0
	}
	return q.capacity
}

// SetCapacity updates queue capacity and triggers mutate callback.
func (q *TrackedQueue[T]) SetCapacity(capacity int) {
	if q == nil {
		return
	}
	q.capacity = capacity
	q.notify()
}

// Len returns the number of items.
func (q *TrackedQueue[T]) Len() int {
	if q == nil {
		return 0
	}
	return len(q.items)
}

// CanAccept checks if the queue can accept additional itemsCount entries respecting capacity.
func (q *TrackedQueue[T]) CanAccept(itemsCount int) bool {
	if q == nil {
		return true
	}
	if q.capacity < 0 {
		return true
	}
	return len(q.items)+itemsCount <= q.capacity
}

// Enqueue appends an item. Returns false if capacity exceeded.
func (q *TrackedQueue[T]) Enqueue(item T, cycle int) bool {
	if q == nil {
		return false
	}
	if q.capacity >= 0 && len(q.items) >= q.capacity {
		return false
	}
	q.items = append(q.items, item)
	if q.hooks.OnEnqueue != nil {
		q.hooks.OnEnqueue(item, cycle)
	}
	q.notify()
	return true
}

// PopFront removes and returns the front item.
func (q *TrackedQueue[T]) PopFront(cycle int) (T, bool) {
	var zero T
	if q == nil || len(q.items) == 0 {
		return zero, false
	}
	item := q.items[0]
	q.items = q.items[1:]
	if q.hooks.OnDequeue != nil {
		q.hooks.OnDequeue(item, cycle)
	}
	q.notify()
	return item, true
}

// RemoveAt deletes the item at index.
func (q *TrackedQueue[T]) RemoveAt(idx int, cycle int) (T, bool) {
	var zero T
	if q == nil || idx < 0 || idx >= len(q.items) {
		return zero, false
	}
	item := q.items[idx]
	q.items = append(q.items[:idx], q.items[idx+1:]...)
	if q.hooks.OnDequeue != nil {
		q.hooks.OnDequeue(item, cycle)
	}
	q.notify()
	return item, true
}

// RemoveMatch removes the first item matching predicate.
func (q *TrackedQueue[T]) RemoveMatch(match func(T) bool, cycle int) (T, bool) {
	var zero T
	if q == nil || match == nil {
		return zero, false
	}
	for i, item := range q.items {
		if match(item) {
			return q.RemoveAt(i, cycle)
		}
	}
	return zero, false
}

// Items exposes the underlying slice (read-only operations only).
func (q *TrackedQueue[T]) Items() []T {
	if q == nil {
		return nil
	}
	return q.items
}

func (q *TrackedQueue[T]) notify() {
	if q == nil || q.mutate == nil {
		return
	}
	q.mutate(len(q.items), q.capacity)
}

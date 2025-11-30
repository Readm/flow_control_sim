package node

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Readm/flow_sim/internal/core/capability/cache"
	"github.com/Readm/flow_sim/internal/core/capability/directory"
	"github.com/Readm/flow_sim/internal/dataflow/packet"
)

// ProcessHook allows callers to implement custom processing logic.
type ProcessHook func(ctx context.Context, cycle uint64, buffer []packet.Packet) ([]packet.Packet, error)

// InputQueue describes the behaviors Node needs from an input buffer.
type InputQueue interface {
	Pick() []packet.Packet
	Tick(cycle int) error
	Length() int
	Capacity() int
	IsFull() bool
}

// OutputQueue describes the behaviors Node needs from an output buffer.
type OutputQueue interface {
	Tick(cycle int) error
	Length() int
	Capacity() int
	IsFull() bool
	InjectPackets(cycle int, packets []packet.Packet) error
}

// Node is a schedulable processing element that aggregates multiple InputQueues,
// OutputQueues, optional cache/directory capabilities, and a pluggable process hook.
type Node struct {
	id int

	inputs  []InputQueue
	outputs []OutputQueue

	caches      []cache.Cache
	directories []directory.Directory

	bufferMu      sync.Mutex
	processBuffer []packet.Packet
	processHook   ProcessHook
	currentCycle  uint64
}

// New creates a Node with the provided identifier.
func New(id int) *Node {
	return &Node{
		id:            id,
		inputs:        make([]InputQueue, 0),
		outputs:       make([]OutputQueue, 0),
		caches:        make([]cache.Cache, 0),
		directories:   make([]directory.Directory, 0),
		processBuffer: make([]packet.Packet, 0),
		currentCycle:  0,
	}
}

// ID returns the immutable identifier assigned to the Node.
func (n *Node) ID() int { return n.id }

// AddInputQueue registers an InputQueue.
func (n *Node) AddInputQueue(q InputQueue) error {
	if q == nil {
		return errors.New("input queue cannot be nil")
	}
	n.inputs = append(n.inputs, q)
	return nil
}

// AddOutputQueue registers an OutputQueue.
func (n *Node) AddOutputQueue(q OutputQueue) error {
	if q == nil {
		return errors.New("output queue cannot be nil")
	}
	n.outputs = append(n.outputs, q)
	return nil
}

// InputQueues returns the registered inputs.
func (n *Node) InputQueues() []InputQueue {
	cp := make([]InputQueue, len(n.inputs))
	copy(cp, n.inputs)
	return cp
}

// OutputQueues returns the registered outputs.
func (n *Node) OutputQueues() []OutputQueue {
	cp := make([]OutputQueue, len(n.outputs))
	copy(cp, n.outputs)
	return cp
}

// AddCache attaches a cache to the Node.
func (n *Node) AddCache(c cache.Cache) {
	if c != nil {
		n.caches = append(n.caches, c)
	}
}

// Caches returns attached caches.
func (n *Node) Caches() []cache.Cache {
	cp := make([]cache.Cache, len(n.caches))
	copy(cp, n.caches)
	return cp
}

// AddDirectory attaches a directory.
func (n *Node) AddDirectory(d directory.Directory) {
	if d != nil {
		n.directories = append(n.directories, d)
	}
}

// Directories returns attached directories.
func (n *Node) Directories() []directory.Directory {
	cp := make([]directory.Directory, len(n.directories))
	copy(cp, n.directories)
	return cp
}

// SetProcessHook configures the hook invoked after packets are collected.
func (n *Node) SetProcessHook(hook ProcessHook) {
	n.bufferMu.Lock()
	defer n.bufferMu.Unlock()
	n.processHook = hook
}

// ProcessBuffer returns the last stored buffer.
func (n *Node) ProcessBuffer() []packet.Packet {
	n.bufferMu.Lock()
	defer n.bufferMu.Unlock()
	buf := make([]packet.Packet, len(n.processBuffer))
	copy(buf, n.processBuffer)
	return buf
}

// Tick executes a cycle of computation on the Node.
func (n *Node) Tick(ctx context.Context, cycle uint64, _ time.Duration) error {
	buffer := n.collectPackets()

	if hook := n.getProcessHook(); hook != nil {
		processed, err := hook(ctx, cycle, buffer)
		if err != nil {
			return err
		}
		if processed != nil {
			buffer = processed
		}
	}

	n.storeProcessBuffer(buffer)
	return n.tickQueuesConcurrently(int(cycle))
}

func (n *Node) collectPackets() []packet.Packet {
	collected := make([]packet.Packet, 0)
	for _, input := range n.inputs {
		if packets := input.Pick(); len(packets) > 0 {
			collected = append(collected, packets...)
		}
	}
	return collected
}

func (n *Node) getProcessHook() ProcessHook {
	n.bufferMu.Lock()
	defer n.bufferMu.Unlock()
	return n.processHook
}

func (n *Node) storeProcessBuffer(buffer []packet.Packet) {
	n.bufferMu.Lock()
	defer n.bufferMu.Unlock()
	n.processBuffer = append(n.processBuffer[:0], buffer...)
}

func (n *Node) tickQueuesConcurrently(cycle int) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(n.inputs)+len(n.outputs))

	for _, input := range n.inputs {
		wg.Add(1)
		go func(q InputQueue) {
			defer wg.Done()
			if err := q.Tick(cycle); err != nil {
				errCh <- err
			}
		}(input)
	}

	for _, output := range n.outputs {
		wg.Add(1)
		go func(q OutputQueue) {
			defer wg.Done()
			if err := q.Tick(cycle); err != nil {
				errCh <- err
			}
		}(output)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

// Advance executes the configured number of cycles sequentially using Background context.
func (n *Node) Advance(cycles int) error {
	if cycles <= 0 {
		return nil
	}

	ctx := context.Background()
	for i := 0; i < cycles; i++ {
		cycle := n.currentCycle
		if err := n.Tick(ctx, cycle, 0); err != nil {
			return err
		}
		n.currentCycle++
	}
	return nil
}

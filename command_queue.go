package main

import (
	"context"

	"github.com/Readm/flow_sim/visual"
)

// CommandQueue abstracts command delivery to simulator.
type CommandQueue interface {
	Enqueue(cmd visual.ControlCommand) bool
	TryDequeue() (visual.ControlCommand, bool)
	Next(ctx context.Context) (visual.ControlCommand, bool)
}

type channelCommandQueue struct {
	ch chan visual.ControlCommand
}

func newChannelCommandQueue(buffer int) CommandQueue {
	return &channelCommandQueue{ch: make(chan visual.ControlCommand, buffer)}
}

func (q *channelCommandQueue) Enqueue(cmd visual.ControlCommand) bool {
	select {
	case q.ch <- cmd:
		return true
	default:
		return false
	}
}

func (q *channelCommandQueue) TryDequeue() (visual.ControlCommand, bool) {
	select {
	case cmd := <-q.ch:
		return cmd, true
	default:
		return visual.ControlCommand{Type: visual.CommandNone}, false
	}
}

func (q *channelCommandQueue) Next(ctx context.Context) (visual.ControlCommand, bool) {
	select {
	case cmd := <-q.ch:
		return cmd, true
	case <-ctx.Done():
		return visual.ControlCommand{Type: visual.CommandNone}, false
	}
}

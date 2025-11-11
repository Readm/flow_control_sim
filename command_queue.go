package main

import "context"

// CommandQueue abstracts command delivery to simulator.
type CommandQueue interface {
	Enqueue(cmd ControlCommand) bool
	TryDequeue() (ControlCommand, bool)
	Next(ctx context.Context) (ControlCommand, bool)
}

type channelCommandQueue struct {
	ch chan ControlCommand
}

func newChannelCommandQueue(buffer int) CommandQueue {
	return &channelCommandQueue{ch: make(chan ControlCommand, buffer)}
}

func (q *channelCommandQueue) Enqueue(cmd ControlCommand) bool {
	select {
	case q.ch <- cmd:
		return true
	default:
		return false
	}
}

func (q *channelCommandQueue) TryDequeue() (ControlCommand, bool) {
	select {
	case cmd := <-q.ch:
		return cmd, true
	default:
		return ControlCommand{Type: CommandNone}, false
	}
}

func (q *channelCommandQueue) Next(ctx context.Context) (ControlCommand, bool) {
	select {
	case cmd := <-q.ch:
		return cmd, true
	case <-ctx.Done():
		return ControlCommand{Type: CommandNone}, false
	}
}


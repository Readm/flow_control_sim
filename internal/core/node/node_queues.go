//go:build !async
// +build !async

// Copyright (c) 2025

package node

// tickQueuesConcurrently executes all queue Tick operations synchronously (serially).
// This is the default behavior optimized for reducing goroutine overhead.
// To enable concurrent queue execution, build with: go build -tags async
func (n *Node) tickQueuesConcurrently(cycle int) error {
	// Process input queues serially
	for _, input := range n.inputs {
		if err := input.Tick(cycle); err != nil {
			return err
		}
	}

	// Process output queues serially
	for _, output := range n.outputs {
		if err := output.Tick(cycle); err != nil {
			return err
		}
	}

	return nil
}

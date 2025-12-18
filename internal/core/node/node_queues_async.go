//go:build async
// +build async

// Copyright (c) 2025

package node

import "sync"

// tickQueuesConcurrently executes all queue Tick operations concurrently using goroutines.
// This version is available when building with: go build -tags async
// It spawns one goroutine per queue for parallel execution.
func (n *BaseNode) tickQueuesConcurrently(cycle int) error {
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

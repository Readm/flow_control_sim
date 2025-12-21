package node

import (
	"sync"
	"testing"
)

func TestBaseNode_Concurrency(t *testing.T) {
	node := NewBaseNode(1, nil)

	const (
		numGoroutines = 100
		numUpdates    = 1000
	)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Test UpdateData (map-wide)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numUpdates; j++ {
				node.UpdateData(func(data map[string]interface{}) {
					count, _ := data["counter"].(int)
					data["counter"] = count + 1
				})
			}
		}()
	}

	wg.Wait()

	finalCount := node.GetData("counter").(int)
	expectedCount := numGoroutines * numUpdates
	if finalCount != expectedCount {
		t.Errorf("Concurrent UpdateData: expected %d, got %d", expectedCount, finalCount)
	}

	// Test UpdateKeyData (single key)
	node.SetData("key_counter", 0)
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numUpdates; j++ {
				node.UpdateKeyData("key_counter", func(val interface{}) interface{} {
					count, _ := val.(int)
					return count + 1
				})
			}
		}()
	}

	wg.Wait()

	finalKeyCount := node.GetData("key_counter").(int)
	if finalKeyCount != expectedCount {
		t.Errorf("Concurrent UpdateKeyData: expected %d, got %d", expectedCount, finalKeyCount)
	}
}

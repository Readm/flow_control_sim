package main

import (
	"fmt"
	"flow_sim/core"
)

// ExampleCacheLog demonstrates how to view cache mechanism execution logs
func ExampleCacheLog() {
	fmt.Println("=== Cache Mechanism Execution Log Example ===")
	fmt.Println()
	fmt.Println("To view cache execution logs, run:")
	fmt.Println("  go test -v -run TestReadOnceCacheMechanism")
	fmt.Println()
	fmt.Println("The logs will show:")
	fmt.Println("  1. ReadOnce request received at HomeNode")
	fmt.Println("  2. Cache lookup result (HIT or MISS)")
	fmt.Println("  3. Cache update when data arrives from SN")
	fmt.Println("  4. Response generation and routing")
	fmt.Println()
	fmt.Println("Example log output:")
	fmt.Println("  [Cache] HomeNode 2: Received ReadOnce request (TxnID=1, Addr=0x1000, Cycle=4)")
	fmt.Println("  [Cache] HomeNode 2: CACHE MISS for address 0x1000 (TxnID=1, Cycle=4) - forwarding to SN")
	fmt.Println("  [Cache] HomeNode 2: Received CompData from SN, updating cache for address 0x1000 (TxnID=1, Cycle=8)")
	fmt.Println("  [Cache] HomeNode 2: Cache updated for address 0x1000 (TxnID=1)")
	fmt.Println("  [Cache] HomeNode 2: Received ReadOnce request (TxnID=2, Addr=0x1000, Cycle=24)")
	fmt.Println("  [Cache] HomeNode 2: CACHE HIT for address 0x1000 (TxnID=2, Cycle=24)")
	fmt.Println("  [Cache] HomeNode 2: Sending CompData response directly to RN 0 (TxnID=2, PacketID=4, Cycle=24)")
	fmt.Println()
	fmt.Println("To view transaction metadata:")
	fmt.Println("  - Check Transaction.Context.Metadata for cache_hit, cache_miss, cache_updated flags")
	fmt.Println()
}

// PrintTransactionCacheInfo prints cache-related information from a transaction
func PrintTransactionCacheInfo(txn *core.Transaction) {
	if txn == nil || txn.Context == nil {
		return
	}
	
	fmt.Printf("Transaction ID: %d\n", txn.Context.TransactionID)
	fmt.Printf("Address: 0x%x\n", txn.Context.Address)
	fmt.Printf("Type: %s\n", txn.Context.TransactionType)
	
	if txn.Context.Metadata != nil {
		if hit, ok := txn.Context.Metadata["cache_hit"]; ok {
			fmt.Printf("Cache Hit: %s\n", hit)
		}
		if miss, ok := txn.Context.Metadata["cache_miss"]; ok {
			fmt.Printf("Cache Miss: %s\n", miss)
		}
		if updated, ok := txn.Context.Metadata["cache_updated"]; ok {
			fmt.Printf("Cache Updated: %s\n", updated)
		}
	}
}


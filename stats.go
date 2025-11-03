package main

import (
    "fmt"
)

func PrintStats(stats *SimulationStats) {
    if stats == nil || stats.Global == nil {
        fmt.Println("No stats available")
        return
    }
    g := stats.Global
    fmt.Println("=== Global Statistics ===")
    fmt.Printf("Total Requests: %d\n", g.TotalRequests)
    fmt.Printf("Completed Requests: %d\n", g.Completed)
    fmt.Printf("Completion Rate: %.2f%%\n", g.CompletionRate)
    fmt.Printf("Average End-to-End Delay: %.2f cycles\n", g.AvgEndToEndDelay)
    fmt.Printf("Max Delay: %d cycles\n", g.MaxDelay)
    fmt.Printf("Min Delay: %d cycles\n", g.MinDelay)

    fmt.Println()
    fmt.Println("=== Master Statistics ===")
    for i, st := range stats.PerMaster {
        if st == nil { continue }
        fmt.Printf("Master %d: Completed=%d, AvgDelay=%.2f, MaxDelay=%d, MinDelay=%d\n",
            i, st.CompletedRequests, st.AvgDelay, st.MaxDelay, st.MinDelay)
    }

    fmt.Println()
    fmt.Println("=== Slave Statistics ===")
    for i, st := range stats.PerSlave {
        if st == nil { continue }
        fmt.Printf("Slave %d: TotalProcessed=%d, MaxQueueLength=%d, AvgQueueLength=%.2f\n",
            i, st.TotalProcessed, st.MaxQueueLength, st.AvgQueueLength)
    }
}



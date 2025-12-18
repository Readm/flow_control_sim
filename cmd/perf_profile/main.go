package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"time"

	"github.com/Readm/flow_sim/internal/core/node"
)

func main() {
	var (
		duration     = flag.Duration("duration", 10*time.Second, "Duration to run the benchmark")
		cpuProfile   = flag.String("cpuprofile", "cpu.prof", "Write CPU profile to file")
		memProfile   = flag.String("memprofile", "mem.prof", "Write memory profile to file")
		mutexProfile = flag.String("mutexprofile", "mutex.prof", "Write mutex profile to file")
		blockProfile = flag.String("blockprofile", "block.prof", "Write block profile to file")
		traceFile    = flag.String("trace", "", "Write trace to file")
		nodeCount    = flag.Int("nodes", 4, "Number of nodes in the ring")
	)
	flag.Parse()

	// Enable profiles
	runtime.SetMutexProfileFraction(1)
	runtime.SetBlockProfileRate(1)

	// Start CPU profile
	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			panic(err)
		}
		defer pprof.StopCPUProfile()
	}

	// Start trace
	if *traceFile != "" {
		f, err := os.Create(*traceFile)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if err := trace.Start(f); err != nil {
			panic(err)
		}
		defer trace.Stop()
	}

	fmt.Printf("Running BufferlessRing benchmark for %v with %d nodes...\n", *duration, *nodeCount)

	// Create ring
	workers, _, components := node.NewBufferlessRing(*nodeCount, 8, 1, 1)

	// Inject packets continuously
	injectionInterval := 2 // Inject every 2 cycles
	nextInjection := uint64(0)

	ctx := context.Background()
	startTime := time.Now()
	cycle := uint64(0)

	// Run for specified duration
	for time.Since(startTime) < *duration {
		// Inject packets
		if cycle >= nextInjection {
			for i := 0; i < *nodeCount; i++ {
				src := i
				dst := (i + 1) % *nodeCount
				pkt := node.CreatePacket(src, dst, fmt.Sprintf("C%d-S%d", cycle, src))
				// workers[src] is now *node.WorkerNode which implements Node
				if err := workers[src].InjectPacket(pkt); err != nil {
					panic(err)
				}
			}
			nextInjection = cycle + uint64(injectionInterval)
		}

		// Tick all components in parallel to avoid deadlocks
		var wg sync.WaitGroup
		for _, comp := range components {
			wg.Add(1)
			go func(c node.Tickable) {
				defer wg.Done()
				if err := c.Tick(ctx, cycle, 0); err != nil {
					panic(err)
				}
			}(comp)
		}
		wg.Wait()

		cycle++
	}

	elapsed := time.Since(startTime)
	cyclesPerSec := float64(cycle) / elapsed.Seconds()
	nsPerCycle := float64(elapsed.Nanoseconds()) / float64(cycle)

	fmt.Printf("\n=== Performance Results ===\n")
	fmt.Printf("Total cycles:     %d\n", cycle)
	fmt.Printf("Elapsed time:     %v\n", elapsed)
	fmt.Printf("Cycles/sec:       %.0f\n", cyclesPerSec)
	fmt.Printf("ns/cycle:         %.2f\n", nsPerCycle)
	fmt.Printf("μs/cycle:         %.2f\n", nsPerCycle/1000)

	// Write memory profile
	if *memProfile != "" {
		f, err := os.Create(*memProfile)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err != nil {
			panic(err)
		}
	}

	// Write mutex profile
	if *mutexProfile != "" {
		f, err := os.Create(*mutexProfile)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if err := pprof.Lookup("mutex").WriteTo(f, 0); err != nil {
			panic(err)
		}
	}

	// Write block profile
	if *blockProfile != "" {
		f, err := os.Create(*blockProfile)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		if err := pprof.Lookup("block").WriteTo(f, 0); err != nil {
			panic(err)
		}
	}

	fmt.Println("\nProfile files written successfully")
}

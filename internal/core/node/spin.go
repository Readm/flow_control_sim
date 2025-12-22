package node

import (
	"math/rand"
	"time"
)

// SpinWaitCycles busy-waits for approximately the specified number of CPU cycles.
// This is more precise than SpinWait (us) if the CPU frequency is stable.
func SpinWaitCycles(cycles uint64) {
	start := GetCPUCycles()
	for {
		if GetCPUCycles()-start >= cycles {
			break
		}
	}
}

// CalibrateCyclesPerUS measures and returns the number of CPU cycles per microsecond.
// It samples for the specified duration (e.g. 100ms) to average out noise.
func CalibrateCyclesPerUS(duration time.Duration) float64 {
	start := time.Now()
	startCycles := GetCPUCycles()

	// Busy, active wait to avoid sleep state frequency scaling
	// But we need to reference time.
	for time.Since(start) < duration {
		// spin
	}

	endCycles := GetCPUCycles()
	elapsed := time.Since(start)

	return float64(endCycles-startCycles) / float64(elapsed.Microseconds())
}

// SpinWait simulates processing time by busy-waiting.
// This is an estimate of GEM5 O3 CPU core execution time for a typical node operation.
// It occupies the CPU instead of yielding, which better simulates heavy compute loads.
func SpinWait(minUs, maxUs int) {
	delayUs := minUs
	if maxUs > minUs {
		delayUs += rand.Intn(maxUs - minUs)
	}
	if delayUs <= 0 {
		return
	}

	d := time.Duration(delayUs) * time.Microsecond
	start := time.Now()
	for {
		if time.Since(start) >= d {
			break
		}
		// Busy wait - keeps the CPU core active
	}
}

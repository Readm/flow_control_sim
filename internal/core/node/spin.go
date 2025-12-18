package node

import (
	"math/rand"
	"time"
)

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

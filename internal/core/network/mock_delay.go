package network

import "time"

var (
	mockDelayEnabled bool
	mockDelay        time.Duration
)

// EnableMockDelay configures a static delay between cycles. Intended for mock
// or test scenarios only so真实环境不会受影响。
func EnableMockDelay(d time.Duration) {
	mockDelay = d
	mockDelayEnabled = d > 0
}

// DisableMockDelay removes any previously configured mock delay.
func DisableMockDelay() {
	mockDelay = 0
	mockDelayEnabled = false
}




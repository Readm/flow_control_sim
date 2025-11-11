package main

import (
	"sync"
	"time"
)

type metricsCollector struct {
	mu             sync.Mutex
	interval       time.Duration
	cycleCount     int
	backpressure   int
	lastReportTime time.Time
}

func newMetricsCollector(interval time.Duration) *metricsCollector {
	return &metricsCollector{
		interval:       interval,
		lastReportTime: time.Now(),
	}
}

func (m *metricsCollector) RecordCycles(count int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.cycleCount += count
	m.emitIfNeeded()
	m.mu.Unlock()
}

func (m *metricsCollector) RecordBackpressure() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.backpressure++
	m.emitIfNeeded()
	m.mu.Unlock()
}

func (m *metricsCollector) emitIfNeeded() {
	now := time.Now()
	if now.Sub(m.lastReportTime) < m.interval {
		return
	}
	duration := now.Sub(m.lastReportTime).Seconds()
	throughput := float64(m.cycleCount)
	if duration > 0 {
		throughput = throughput / duration
	}
	GetLogger().Infof("Throughput %.0f cycles/s, backpressure events %d", throughput, m.backpressure)
	m.cycleCount = 0
	m.backpressure = 0
	m.lastReportTime = now
}

var metrics = newMetricsCollector(5 * time.Second)


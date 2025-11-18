package app

import (
	"math/rand"

	"github.com/Readm/flow_sim/framework/core"
)

// RequestGenerationResult contains the result of request generation decision
type RequestGenerationResult struct {
	ShouldGenerate  bool
	SlaveIndex      int
	Target          string
	TransactionType core.CHITransactionType
	Address         uint64 // 0 means auto-increment
	DataSize        int    // 0 means default (DefaultCacheLineSize)
}

// RequestGenerator defines the interface for generating requests
type RequestGenerator interface {
	// ShouldGenerate determines if request(s) should be generated at the given cycle
	// Parameters:
	//   - cycle: current simulation cycle
	//   - masterIndex: index of the master (RN) asking (0-based)
	//   - numSlaves: total number of slaves available
	// Returns: slice of RequestGenerationResult, empty slice means no generation
	// Note: Can return multiple results for multiple requests in same cycle
	ShouldGenerate(cycle int, masterIndex int, numSlaves int) []RequestGenerationResult

	// Reset resets the generator state (called on simulation reset)
	Reset()
}

// BaseGenerator provides common functionality for generators
type BaseGenerator struct {
	// Common fields if needed
}

func (bg *BaseGenerator) Reset() {
	// Default implementation (no-op for most generators)
}

// ProbabilityGenerator implements probability-based request generation
type ProbabilityGenerator struct {
	BaseGenerator
	RequestRate  float64
	SlaveWeights []int
	rng          *rand.Rand
}

// NewProbabilityGenerator creates a new probability-based request generator
func NewProbabilityGenerator(requestRate float64, slaveWeights []int, rng *rand.Rand) *ProbabilityGenerator {
	return &ProbabilityGenerator{
		RequestRate:  requestRate,
		SlaveWeights: slaveWeights,
		rng:          rng,
	}
}

func (pg *ProbabilityGenerator) ShouldGenerate(cycle int, masterIndex int, numSlaves int) []RequestGenerationResult {
	if pg.rng.Float64() >= pg.RequestRate {
		return nil
	}

	var slaveIndex int
	if len(pg.SlaveWeights) == 0 {
		if numSlaves <= 0 {
			return nil
		}
		slaveIndex = pg.rng.Intn(numSlaves)
	} else {
		slaveIndex = weightedChoose(pg.rng, pg.SlaveWeights)
	}
	if slaveIndex < 0 || slaveIndex >= numSlaves {
		return nil
	}

	return []RequestGenerationResult{
		{
			ShouldGenerate:  true,
			SlaveIndex:      slaveIndex,
			TransactionType: core.CHITxnReadNoSnp,
		},
	}
}

// ScheduleItem defines a single request in the schedule
type ScheduleItem struct {
	SlaveIndex      int
	Target          string
	TransactionType core.CHITransactionType
	Address         uint64 // 0 means auto-increment
	DataSize        int    // 0 means default (DefaultCacheLineSize)
}

// ScheduleGenerator implements deterministic request generation based on a schedule
// Schedule format: cycle -> []ScheduleItem (per requester node)
type ScheduleGenerator struct {
	BaseGenerator
	schedule         map[int][]ScheduleItem
	originalSchedule map[int][]ScheduleItem
}

// NewScheduleGenerator creates a new schedule-based request generator
func NewScheduleGenerator(schedule map[int][]ScheduleItem) *ScheduleGenerator {
	original := make(map[int][]ScheduleItem, len(schedule))
	for cycle, items := range schedule {
		copied := make([]ScheduleItem, len(items))
		copy(copied, items)
		original[cycle] = copied
	}
	cloned := make(map[int][]ScheduleItem, len(schedule))
	for cycle, items := range schedule {
		copied := make([]ScheduleItem, len(items))
		copy(copied, items)
		cloned[cycle] = copied
	}
	return &ScheduleGenerator{
		schedule:         cloned,
		originalSchedule: original,
	}
}

func (sg *ScheduleGenerator) ShouldGenerate(cycle int, masterIndex int, numSlaves int) []RequestGenerationResult {
	items, exists := sg.schedule[cycle]
	if !exists || len(items) == 0 {
		return nil
	}
	results := make([]RequestGenerationResult, 0, len(items))
	for _, item := range items {
		results = append(results, RequestGenerationResult{
			ShouldGenerate:  true,
			SlaveIndex:      item.SlaveIndex,
			Target:          item.Target,
			TransactionType: item.TransactionType,
			Address:         item.Address,
			DataSize:        item.DataSize,
		})
	}
	delete(sg.schedule, cycle)
	return results
}

func (sg *ScheduleGenerator) Reset() {
	sg.schedule = make(map[int][]ScheduleItem, len(sg.originalSchedule))
	for cycle, items := range sg.originalSchedule {
		copied := make([]ScheduleItem, len(items))
		copy(copied, items)
		sg.schedule[cycle] = copied
	}
}

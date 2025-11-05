package main

import (
	"math/rand"
)

// RequestGenerationResult contains the result of request generation decision
type RequestGenerationResult struct {
	ShouldGenerate bool
	SlaveIndex     int
	TransactionType CHITransactionType
	Address        uint64  // 0 means auto-increment
	DataSize       int     // 0 means default (DefaultCacheLineSize)
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
	
	slaveIndex := weightedChoose(pg.rng, pg.SlaveWeights)
	if slaveIndex < 0 || slaveIndex >= numSlaves {
		return nil
	}
	
	return []RequestGenerationResult{
		{
			ShouldGenerate: true,
			SlaveIndex:     slaveIndex,
			TransactionType: CHITxnReadNoSnp,
		},
	}
}

// ScheduleItem defines a single request in the schedule
type ScheduleItem struct {
	SlaveIndex      int
	TransactionType CHITransactionType
	Address         uint64  // 0 means auto-increment
	DataSize        int     // 0 means default (DefaultCacheLineSize)
}

// ScheduleGenerator implements deterministic request generation based on a schedule
// Schedule format: cycle -> masterIndex -> []ScheduleItem
// Supports multiple requests per cycle per master
type ScheduleGenerator struct {
	BaseGenerator
	schedule     map[int]map[int][]ScheduleItem  // cycle -> masterIndex -> []ScheduleItem
	originalSchedule map[int]map[int][]ScheduleItem  // backup for Reset()
	currentCycle int
}

// NewScheduleGenerator creates a new schedule-based request generator
func NewScheduleGenerator(schedule map[int]map[int][]ScheduleItem) *ScheduleGenerator {
	// Deep copy schedule for reset capability
	originalSchedule := make(map[int]map[int][]ScheduleItem)
	for cycle, masterMap := range schedule {
		originalSchedule[cycle] = make(map[int][]ScheduleItem)
		for masterIdx, items := range masterMap {
			itemsCopy := make([]ScheduleItem, len(items))
			copy(itemsCopy, items)
			originalSchedule[cycle][masterIdx] = itemsCopy
		}
	}
	
	return &ScheduleGenerator{
		schedule:         schedule,
		originalSchedule: originalSchedule,
		currentCycle:    0,
	}
}

func (sg *ScheduleGenerator) ShouldGenerate(cycle int, masterIndex int, numSlaves int) []RequestGenerationResult {
	sg.currentCycle = cycle
	
	cycleSchedule, exists := sg.schedule[cycle]
	if !exists {
		return nil
	}
	
	masterSchedule, exists := cycleSchedule[masterIndex]
	if !exists || len(masterSchedule) == 0 {
		return nil
	}
	
	// Return all items for this master at this cycle
	results := make([]RequestGenerationResult, 0, len(masterSchedule))
	for _, item := range masterSchedule {
		results = append(results, RequestGenerationResult{
			ShouldGenerate: true,
			SlaveIndex:     item.SlaveIndex,
			TransactionType: item.TransactionType,
			Address:        item.Address,
			DataSize:       item.DataSize,
		})
	}
	
	// Remove consumed items from schedule
	delete(sg.schedule[cycle], masterIndex)
	if len(sg.schedule[cycle]) == 0 {
		delete(sg.schedule, cycle)
	}
	
	return results
}

func (sg *ScheduleGenerator) Reset() {
	// Restore original schedule
	sg.schedule = make(map[int]map[int][]ScheduleItem)
	for cycle, masterMap := range sg.originalSchedule {
		sg.schedule[cycle] = make(map[int][]ScheduleItem)
		for masterIdx, items := range masterMap {
			itemsCopy := make([]ScheduleItem, len(items))
			copy(itemsCopy, items)
			sg.schedule[cycle][masterIdx] = itemsCopy
		}
	}
	sg.currentCycle = 0
}


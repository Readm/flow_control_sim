package recorder

import (
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/core/pipeline"
	"github.com/Readm/flow_sim/pkg/visual/frame"
)

// Recorder observes network cycles and emits visualization frames.
type Recorder struct {
	frames chan *frame.Frame

	mu     sync.Mutex
	paused bool
	last   *frame.Frame
	closed bool
}

// New creates a recorder with the provided buffered channel size.
func New(buffer int) *Recorder {
	if buffer <= 0 {
		buffer = 8
	}
	return &Recorder{
		frames: make(chan *frame.Frame, buffer),
	}
}

// OnCycleEnd implements network.CycleHook and records a new frame.
func (r *Recorder) OnCycleEnd(cycle uint64, nodes []node.Node, links []*link.Link) {
	frame := &frame.Frame{
		Cycle:         int(cycle),
		Paused:        r.isPaused(),
		Nodes:         buildNodes(nodes),
		Edges:         buildEdges(links, cycle),
		InFlightCount: countInFlight(links),
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.last = frame
	r.mu.Unlock()

	select {
	case r.frames <- frame:
	default:
		// Drop oldest to keep stream fresh.
		select {
		case <-r.frames:
		default:
		}
		r.frames <- frame
	}
}

// Frames exposes the streaming channel of captured frames.
func (r *Recorder) Frames() <-chan *frame.Frame {
	return r.frames
}

// Latest returns a copy of the last recorded frame.
func (r *Recorder) Latest() *frame.Frame {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == nil {
		return nil
	}
	clone := *r.last
	clone.Nodes = append([]frame.Node(nil), r.last.Nodes...)
	clone.Edges = append([]frame.Edge(nil), r.last.Edges...)
	return &clone
}

// SetPaused updates the paused flag reflected in subsequent frames.
func (r *Recorder) SetPaused(paused bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paused = paused
	if r.last != nil {
		r.last.Paused = paused
	}
}

// Close stops the recorder and closes the frame channel.
func (r *Recorder) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.frames)
	r.mu.Unlock()
}

func (r *Recorder) isPaused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paused
}

func buildNodes(nodes []node.Node) []frame.Node {
	result := make([]frame.Node, 0, len(nodes))
	for _, n := range nodes {
		if n == nil {
			continue
		}
		flows := n.Flows()
		totalProcessed := 0
		queues := make([]frame.Queue, 0)
		inQueueBackpressure := false
		outQueueBackpressure := false
		downstreamBackpressure := false

		for _, f := range flows {
			totalProcessed += processedCount(f)

			// Capture in_queue information (simplified - can't access internal state)
			inQueueLen := getInQueueLength(f)
			inQueueCap := getInQueueCapacity(f)

			queues = append(queues, frame.Queue{
				Name:     fmt.Sprintf("Flow-%d-in", f.ID()),
				Length:   inQueueLen,
				Capacity: inQueueCap,
			})

			// Capture output ports information (replaces dispatch_queue)
			outPorts := f.OutPorts()
			for i := range outPorts {
				// We can't directly access port state, so use placeholder values
				queues = append(queues, frame.Queue{
					Name:     fmt.Sprintf("Flow-%d-out-%d", f.ID(), i),
					Length:   0,  // Not accessible without breaking encapsulation
					Capacity: -1, // Not accessible without breaking encapsulation
				})
			}

			// Backpressure signals are no longer available in the new interface
			// Set to false as default
			inQueueBackpressure = false
			outQueueBackpressure = false
			downstreamBackpressure = false
		}

		result = append(result, frame.Node{
			ID:     n.ID(),
			Label:  fmt.Sprintf("Node %d", n.ID()),
			Type:   "generic",
			Queues: queues,
			Payload: map[string]any{
				"processed": totalProcessed,
			},
			InQueueBackpressure:    inQueueBackpressure,
			OutQueueBackpressure:   outQueueBackpressure,
			DownstreamBackpressure: downstreamBackpressure,
		})
	}
	return result
}

func buildEdges(links []*link.Link, currentCycle uint64) []frame.Edge {
	result := make([]frame.Edge, 0, len(links))
	for _, l := range links {
		if l == nil {
			continue
		}
		occupancy := l.SnapshotOccupancy()
		latency := l.Latency()
		// Use latency as slotCount (slots are based on latency)
		slotCount := latency
		if slotCount == 0 {
			slotCount = 1
		}

		// Build stages in order from target to source
		// Stage 0 is closest to target (arriving soon), Stage latency-1 is closest to source (just sent)
		stages := make([]frame.PipelineStage, latency)
		for stageIdx := 0; stageIdx < latency; stageIdx++ {
			// Calculate which slot corresponds to this stage
			// Stage 0 (closest to target): packets arriving in 1 cycle → slot (currentCycle + 1) % slotCount
			// Stage 1: packets arriving in 2 cycles → slot (currentCycle + 2) % slotCount
			// ...
			// Stage latency-1 (closest to source): packets arriving in latency cycles → slot (currentCycle + latency) % slotCount
			// So: slotIdx = (currentCycle + stageIdx + 1) % slotCount
			slotIdx := int((currentCycle + uint64(stageIdx) + 1) % uint64(slotCount))
			packetCount := 0
			if slotIdx < len(occupancy) {
				packetCount = occupancy[slotIdx]
			}
			stages[stageIdx] = frame.PipelineStage{
				StageIndex:  stageIdx,
				PacketCount: packetCount,
			}
		}

		result = append(result, frame.Edge{
			Source:         l.SourceID(),
			Target:         l.TargetID(),
			Label:          fmt.Sprintf("%d→%d", l.SourceID(), l.TargetID()),
			Latency:        int(latency),
			BandwidthLimit: int(l.Bandwidth()),
			PipelineStages: stages,
			Backpressured:  false, // Backpressure is no longer tracked in the new interface
		})
	}
	return result
}

func countInFlight(links []*link.Link) int {
	total := 0
	for _, l := range links {
		for _, count := range l.SnapshotOccupancy() {
			total += count
		}
	}
	return total
}

func processedCount(f pipeline.Pipeline) int {
	if f == nil {
		return 0
	}
	return f.ProcessedCount()
}

// getInQueueLength returns the length of the in_queue (mailbox channel).
// Note: We can't directly access len() of a send-only channel, so we return 0.
// The frontend should use IsInQueueFull() to determine if it's full.
func getInQueueLength(f pipeline.Pipeline) int {
	// For now, we can't get the length without breaking encapsulation
	// This would require adding a method to Flow interface
	return 0
}

// getInQueueCapacity returns the capacity of the in_queue.
// Note: We can't directly access cap() of a send-only channel, so we return -1.
// The frontend should use IsInQueueFull() to determine if it's full.
func getInQueueCapacity(f pipeline.Pipeline) int {
	// For now, we can't get the capacity without breaking encapsulation
	// This would require adding a method to Flow interface
	return -1
}

// Note: getOutQueueLength and getOutQueueCapacity have been removed.
// Use dispatch queue methods instead (DrainDispatchQueue, IsDispatchQueueFull).

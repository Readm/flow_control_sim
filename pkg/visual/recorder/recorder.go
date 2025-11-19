package recorder

import (
	"fmt"
	"sync"

	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/node"
	"github.com/Readm/flow_sim/internal/dataflow/flow"
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
		Edges:         buildEdges(links),
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
		for _, f := range flows {
			totalProcessed += processedCount(f)
		}
		result = append(result, frame.Node{
			ID:    n.ID(),
			Label: fmt.Sprintf("Node %d", n.ID()),
			Type:  "generic",
			Payload: map[string]any{
				"processed": totalProcessed,
			},
		})
	}
	return result
}

func buildEdges(links []*link.Link) []frame.Edge {
	result := make([]frame.Edge, 0, len(links))
	for _, l := range links {
		if l == nil {
			continue
		}
		stages := make([]frame.PipelineStage, 0)
		for idx, packets := range l.SnapshotOccupancy() {
			stages = append(stages, frame.PipelineStage{
				StageIndex:  idx,
				PacketCount: packets,
			})
		}
		result = append(result, frame.Edge{
			Source:         l.SourceID(),
			Target:         l.TargetID(),
			Label:          fmt.Sprintf("%d→%d", l.SourceID(), l.TargetID()),
			Latency:        int(l.Latency()),
			BandwidthLimit: int(l.SlotCount()),
			PipelineStages: stages,
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

func processedCount(f flow.Flow) int {
	if f == nil {
		return 0
	}
	return f.ProcessedCount()
}

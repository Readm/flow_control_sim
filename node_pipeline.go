package main

import (
	"github.com/Readm/flow_sim/core"
	"github.com/Readm/flow_sim/queue"
)

type pipelineStageName string

const (
	pipelineStageIn      pipelineStageName = "in_queue"
	pipelineStageProcess pipelineStageName = "process_queue"
	pipelineStageOut     pipelineStageName = "out_queue"
)

// PipelineCapacities configures queue capacity for each stage (-1 for unlimited).
type PipelineCapacities struct {
	In      int
	Process int
	Out     int
}

// PipelineHooks configures queue callbacks for each stage.
type PipelineHooks struct {
	In      queue.StageQueueHooks[*PipelineMessage]
	Process queue.StageQueueHooks[*PipelineMessage]
	Out     queue.StageQueueHooks[*PipelineMessage]
}

// PipelineMessage is the payload carried through pipeline queues.
type PipelineMessage struct {
	Packet        *core.Packet
	Kind          string
	TargetID      int
	DefaultTarget int
	Latency       int
	Metadata      map[string]any
}

// PacketPipeline maintains the three-stage queues with shared block registry.
type PacketPipeline struct {
	blockRegistry *queue.BlockRegistry
	inQueue       *queue.StageQueue[*PipelineMessage]
	processQueue  *queue.StageQueue[*PipelineMessage]
	outQueue      *queue.StageQueue[*PipelineMessage]
}

func newPacketPipeline(
	makeMutator func(stage pipelineStageName) queue.MutateFunc,
	caps PipelineCapacities,
	hooks PipelineHooks,
) *PacketPipeline {
	if caps.In == 0 {
		caps.In = queue.UnlimitedCapacity
	}
	if caps.Process == 0 {
		caps.Process = queue.UnlimitedCapacity
	}
	if caps.Out == 0 {
		caps.Out = queue.UnlimitedCapacity
	}
	p := &PacketPipeline{
		blockRegistry: queue.NewBlockRegistry(0),
		inQueue:       queue.NewStageQueue(string(pipelineStageIn), caps.In, makeMutator(pipelineStageIn), hooks.In),
		processQueue:  queue.NewStageQueue(string(pipelineStageProcess), caps.Process, makeMutator(pipelineStageProcess), hooks.Process),
		outQueue:      queue.NewStageQueue(string(pipelineStageOut), caps.Out, makeMutator(pipelineStageOut), hooks.Out),
	}
	return p
}

func (p *PacketPipeline) registerBlockReason(name string) (queue.BlockIndex, error) {
	if p == nil {
		return queue.InvalidBlockIndex, nil
	}
	return p.blockRegistry.Register(name)
}

func (p *PacketPipeline) queue(stage pipelineStageName) *queue.StageQueue[*PipelineMessage] {
	if p == nil {
		return nil
	}
	switch stage {
	case pipelineStageIn:
		return p.inQueue
	case pipelineStageProcess:
		return p.processQueue
	case pipelineStageOut:
		return p.outQueue
	default:
		return nil
	}
}

func (p *PacketPipeline) setCapacity(stage pipelineStageName, capacity int) {
	if q := p.queue(stage); q != nil {
		q.SetCapacity(capacity)
	}
}

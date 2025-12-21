package node

import (
	"time"

	"github.com/Readm/flow_sim/internal/core/ahead_port"
	"github.com/Readm/flow_sim/internal/core/link"
	"github.com/Readm/flow_sim/internal/core/queue"
)

type linkTickable struct {
	l *link.Link
}

func (lt *linkTickable) Tick(cycle uint64, _ time.Duration) error {
	return lt.l.Tick(int(cycle), int(cycle))
}

// NewBufferlessRing creates a complete bufferless ring network.
// Returns slices of workers, routers, and all tickable components (including links).
func NewBufferlessRing(nodeCount int, queueSize int, ringLatency int, queueBandwidth int) ([]*WorkerNode, []*BufferlessRingRouterNode, []Tickable) {
	routers := make([]*BufferlessRingRouterNode, nodeCount)
	workers := make([]*WorkerNode, nodeCount)
	var components []Tickable

	for i := 0; i < nodeCount; i++ {
		routerID := 100 + i
		workerID := i
		// Use a default buffer capacity for the router (e.g., 8)
		routers[i] = NewBufferlessRingRouter(routerID, workerID, 8)
		workers[i] = NewWorkerNode(workerID)
	}

	// Create queues
	ringInQueues := make([]*queue.InputQueue, nodeCount)
	ringOutQueues := make([]*queue.OutputQueue, nodeCount)
	localInQueues := make([]*queue.InputQueue, nodeCount)
	localOutQueues := make([]*queue.OutputQueue, nodeCount)
	workerInQueues := make([]*queue.InputQueue, nodeCount)
	workerOutQueues := make([]*queue.OutputQueue, nodeCount)

	for i := 0; i < nodeCount; i++ {
		ringInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		ringOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth)
		localInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		localOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth)
		workerInQueues[i] = queue.NewInputQueue(queueSize, queueBandwidth)
		workerOutQueues[i] = queue.NewOutputQueue(queueSize, queueBandwidth)
	}

	// Connect routers
	for i := 0; i < nodeCount; i++ {
		routers[i].AddInputQueue(ringInQueues[i])
		routers[i].AddInputQueue(localInQueues[i])
		routers[i].AddOutputQueue(ringOutQueues[i])
		routers[i].AddOutputQueue(localOutQueues[i])
	}

	// Connect workers
	for i := 0; i < nodeCount; i++ {
		workers[i].AddInputQueue(workerInQueues[i])
		workers[i].AddOutputQueue(workerOutQueues[i])
		components = append(components, workers[i])
		components = append(components, routers[i])
	}

	// Create ring links
	for i := 0; i < nodeCount; i++ {
		nextRouter := (i + 1) % nodeCount
		sourceID := 100 + i
		targetID := 100 + nextRouter

		fc := link.NewBufferlessLinkHandler()
		ringLink := link.NewLinkWithHandler(sourceID, targetID, ringLatency, 1, fc)

		// OutputQueue -> Link
		p1 := ahead_port.NewPort()
		ringOutQueues[i].SetDownstreamPort(p1.AsInPort())
		ringLink.SetUpstreamPort(p1.AsOutPort())

		// Link -> InputQueue
		p2 := ahead_port.NewPort()
		ringLink.SetDownstreamPort(p2.AsInPort())
		ringInQueues[nextRouter].SetUpstreamPort(p2.AsOutPort())

		components = append(components, &linkTickable{ringLink})
	}

	// Create local connections (with 1 cycle latency to avoid deadlocks)
	for i := 0; i < nodeCount; i++ {
		// Worker -> Router
		fc1 := link.NewBufferlessLinkHandler()
		l1 := link.NewLinkWithHandler(i, 100+i, 1, queueBandwidth, fc1)

		p1_out := ahead_port.NewPort()
		workerOutQueues[i].SetDownstreamPort(p1_out.AsInPort())
		l1.SetUpstreamPort(p1_out.AsOutPort())

		p1_in := ahead_port.NewPort()
		l1.SetDownstreamPort(p1_in.AsInPort())
		localInQueues[i].SetUpstreamPort(p1_in.AsOutPort())

		// Router -> Worker
		fc2 := link.NewBufferlessLinkHandler()
		l2 := link.NewLinkWithHandler(100+i, i, 1, queueBandwidth, fc2)

		p2_out := ahead_port.NewPort()
		localOutQueues[i].SetDownstreamPort(p2_out.AsInPort())
		l2.SetUpstreamPort(p2_out.AsOutPort())

		p2_in := ahead_port.NewPort()
		l2.SetDownstreamPort(p2_in.AsInPort())
		workerInQueues[i].SetUpstreamPort(p2_in.AsOutPort())

		components = append(components, &linkTickable{l1}, &linkTickable{l2})
	}

	return workers, routers, components
}

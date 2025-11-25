package node

import (
	"context"
	"time"

	"github.com/Readm/flow_sim/internal/core/pipeline"
)

// Node describes the minimum contract the Network scheduler relies on. Higher
// level logic (e.g. coherence, routing) can be composed inside each Node
// implementation without changing this interface.
type Node interface {
	ID() int
	Flows() []pipeline.Pipeline
	Tick(ctx context.Context, cycle uint64, linkDelay time.Duration) error
}

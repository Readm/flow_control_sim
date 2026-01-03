//go:build !trace

package link

import (
	"github.com/Readm/flow_sim/internal/core/ahead_port"
)

// NewTracedInPort returns the original port when tracing is disabled.
func NewTracedInPort(original ahead_port.InPort, link *Link) ahead_port.InPort {
	return original
}

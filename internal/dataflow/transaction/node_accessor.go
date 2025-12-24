package transaction

import (
	"github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/components/decoder"
	"github.com/Readm/flow_sim/internal/components/directory"
	"github.com/Readm/flow_sim/internal/core/node"
)

// NodeAccessor provides abstract access to node resources.
// This abstraction allows Transaction to access node resources regardless
// of whether it's executing on the local node or has migrated to another node.
type NodeAccessor interface {
	// GetCache returns the Cache capability of the current node.
	GetCache() cache.Cache

	// GetDirectory returns the Directory capability of the current node.
	GetDirectory() directory.Directory

	// GetDecoder returns the Decoder capability of the current node.
	GetDecoder() decoder.Decoder

	// GetNode returns the underlying Node object.
	// This is provided for compatibility with existing code.
	GetNode() node.Node

	// NodeID returns the ID of the current node.
	NodeID() int
}

// LocalNodeAccessor provides direct access to a local node's resources.
// This is a zero-overhead implementation for accessing the current node.
type LocalNodeAccessor struct {
	node node.Node
}

// NewLocalNodeAccessor creates a LocalNodeAccessor for the given node.
func NewLocalNodeAccessor(n node.Node) *LocalNodeAccessor {
	return &LocalNodeAccessor{node: n}
}

// GetCache implements NodeAccessor.
func (a *LocalNodeAccessor) GetCache() cache.Cache {
	caches := a.node.Caches()
	if len(caches) == 0 {
		return nil
	}
	return caches[0]
}

// GetDirectory implements NodeAccessor.
func (a *LocalNodeAccessor) GetDirectory() directory.Directory {
	dirs := a.node.Directories()
	if len(dirs) == 0 {
		return nil
	}
	return dirs[0]
}

// GetDecoder implements NodeAccessor.
func (a *LocalNodeAccessor) GetDecoder() decoder.Decoder {
	// Try to get decoder from node data
	// This assumes protocol-specific setup (e.g., CHI_Decoder)
	if decData := a.node.GetData("CHI_Decoder"); decData != nil {
		if dec, ok := decData.(decoder.Decoder); ok {
			return dec
		}
	}

	// Could also try other decoder keys or return nil
	return nil
}

// GetNode implements NodeAccessor.
func (a *LocalNodeAccessor) GetNode() node.Node {
	return a.node
}

// NodeID implements NodeAccessor.
func (a *LocalNodeAccessor) NodeID() int {
	return a.node.ID()
}

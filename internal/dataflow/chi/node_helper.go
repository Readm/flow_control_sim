package chi

import (
	"fmt"

	"github.com/Readm/flow_sim/internal/components/cache"
	"github.com/Readm/flow_sim/internal/components/decoder"
	"github.com/Readm/flow_sim/internal/components/directory"
	"github.com/Readm/flow_sim/internal/core/node"
)

// CHI-specific data keys
const (
	DataKeyRole           = "CHI_Role"
	DataKeyDecoder        = "CHI_Decoder"
	DataKeyMessageBuilder = "CHI_MessageBuilder"
)

// NodeRole defines CHI node roles
type NodeRole string

const (
	RoleRN NodeRole = "RN" // Request Node
	RoleHN NodeRole = "HN" // Home Node
	RoleSN NodeRole = "SN" // Slave Node
)

// SetupCHINode configures a Node for CHI protocol.
func SetupCHINode(
	n node.Node,
	role NodeRole,
	dec decoder.Decoder,
	c cache.Cache,
	dir directory.Directory,
) {
	n.SetData(DataKeyRole, string(role))
	n.SetData(DataKeyDecoder, dec)
	n.SetData(DataKeyMessageBuilder, NewMessageBuilder(n.ID()))

	if c != nil {
		n.AddCache(c)
	}
	if dir != nil {
		n.AddDirectory(dir)
	}
}

// GetCHIRole retrieves the CHI role from a Node.
func GetCHIRole(n node.Node) (NodeRole, error) {
	roleData := n.GetData(DataKeyRole)
	if roleData == nil {
		return "", fmt.Errorf("CHI role not set for node %d", n.ID())
	}
	role, ok := roleData.(string)
	if !ok {
		return "", fmt.Errorf("CHI role has invalid type for node %d", n.ID())
	}
	return NodeRole(role), nil
}

// GetCHIDecoder retrieves the Decoder from a Node.
func GetCHIDecoder(n node.Node) (decoder.Decoder, error) {
	decData := n.GetData(DataKeyDecoder)
	if decData == nil {
		return nil, fmt.Errorf("CHI decoder not set for node %d", n.ID())
	}
	dec, ok := decData.(decoder.Decoder)
	if !ok {
		return nil, fmt.Errorf("CHI decoder has invalid type for node %d", n.ID())
	}
	return dec, nil
}

// GetCHIMessageBuilder retrieves the MessageBuilder from a Node.
func GetCHIMessageBuilder(n node.Node) (*MessageBuilder, error) {
	builderData := n.GetData(DataKeyMessageBuilder)
	if builderData == nil {
		return nil, fmt.Errorf("CHI message builder not set for node %d", n.ID())
	}
	builder, ok := builderData.(*MessageBuilder)
	if !ok {
		return nil, fmt.Errorf("CHI message builder has invalid type for node %d", n.ID())
	}
	return builder, nil
}

// GetCHICache retrieves the first Cache from a Node.
func GetCHICache(n node.Node) cache.Cache {
	caches := n.Caches()
	if len(caches) == 0 {
		return nil
	}
	return caches[0]
}

// GetCHIDirectory retrieves the first Directory from a Node.
func GetCHIDirectory(n node.Node) directory.Directory {
	dirs := n.Directories()
	if len(dirs) == 0 {
		return nil
	}
	return dirs[0]
}

// IsCHINode checks if a Node is configured for CHI protocol.
func IsCHINode(n node.Node) bool {
	return n.HasData(DataKeyRole)
}

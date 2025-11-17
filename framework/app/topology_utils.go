package app

import "strings"

type graphNodeRole int

const (
	graphRoleUnknown graphNodeRole = iota
	graphRoleRequester
	graphRoleHome
	graphRoleSlave
	graphRoleRouter
)

func classifyGraphRole(capabilities []string) graphNodeRole {
	role := graphRoleUnknown
	for _, raw := range capabilities {
		token := normalizeCapabilityToken(raw)
		switch token {
		case "requester", "rn":
			return graphRoleRequester
		case "home_directory", "hn", "home":
			return graphRoleHome
		case "slave_target", "sn", "target":
			if role == graphRoleUnknown {
				role = graphRoleSlave
			}
		case "router", "relay", "ring_router":
			if role == graphRoleUnknown {
				role = graphRoleRouter
			}
		}
	}
	return role
}

func normalizeCapabilityToken(raw string) string {
	token := strings.ToLower(strings.TrimSpace(raw))
	if token == "" {
		return ""
	}
	if idx := strings.IndexRune(token, ':'); idx >= 0 {
		token = token[:idx]
	}
	return token
}

func countGraphRoles(graph *GraphConfig) (masters int, slaves int, homes int) {
	if graph == nil {
		return 0, 0, 0
	}
	for _, node := range graph.Nodes {
		switch classifyGraphRole(node.Capabilities) {
		case graphRoleRequester:
			masters++
		case graphRoleSlave:
			slaves++
		case graphRoleHome:
			homes++
		}
	}
	return masters, slaves, homes
}

func positiveOrDefault(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func applyGraphPosition(receiver NodeReceiver, pos *Position) {
	if pos == nil {
		return
	}
	position := Position{X: pos.X, Y: pos.Y}
	switch n := receiver.(type) {
	case *RequestNode:
		n.Position = position
	case *HomeNode:
		n.Position = position
	case *SlaveNode:
		n.Position = position
	case *RingRouterNode:
		n.Position = position
	}
}

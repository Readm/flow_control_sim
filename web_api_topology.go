package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"
)

type TopologyNode struct {
	ID       int            `json:"id"`
	Label    string         `json:"label"`
	Type     string         `json:"type"`
	PosX     float64        `json:"posX,omitempty"`
	PosY     float64        `json:"posY,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type TopologyEdge struct {
	Source  int    `json:"source"`
	Target  int    `json:"target"`
	Label   string `json:"label"`
	Latency int    `json:"latency,omitempty"`
}

type TopologyDraft struct {
	Nodes     []TopologyNode `json:"nodes"`
	Edges     []TopologyEdge `json:"edges"`
	Version   int64          `json:"version"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Source    string         `json:"source"`
}

func (ws *WebServer) handleTopology(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleTopologyGet(w, r)
	case http.MethodPut:
		ws.handleTopologyPut(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ws *WebServer) handleTopologyGet(w http.ResponseWriter, r *http.Request) {
	draft := ws.snapshotTopology()
	if draft == nil {
		http.Error(w, "Topology unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(draft)
}

func (ws *WebServer) handleTopologyPut(w http.ResponseWriter, r *http.Request) {
	var draft TopologyDraft
	if err := json.NewDecoder(r.Body).Decode(&draft); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := validateTopologyDraft(&draft); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	draft.Version = time.Now().UnixNano()
	draft.UpdatedAt = time.Now().UTC()
	draft.Source = "draft"

	ws.topologyMu.Lock()
	ws.topologyDraft = deepCopyDraft(&draft)
	ws.topologyMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(draft)
}

func (ws *WebServer) snapshotTopology() *TopologyDraft {
	ws.topologyMu.RLock()
	if ws.topologyDraft != nil {
		draft := deepCopyDraft(ws.topologyDraft)
		ws.topologyMu.RUnlock()
		return draft
	}
	ws.topologyMu.RUnlock()

	ws.mu.RLock()
	frame := ws.latestFrame
	ws.mu.RUnlock()
	if frame == nil {
		return nil
	}

	nodes := make([]TopologyNode, len(frame.Nodes))
	for i, node := range frame.Nodes {
		nodes[i] = TopologyNode{
			ID:       node.ID,
			Label:    node.Label,
			Type:     string(node.Type),
			Metadata: copyMetadata(node.Payload),
		}
	}

	edges := make([]TopologyEdge, len(frame.Edges))
	for i, edge := range frame.Edges {
		edges[i] = TopologyEdge{
			Source:  edge.Source,
			Target:  edge.Target,
			Label:   edge.Label,
			Latency: edge.Latency,
		}
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})

	draft := &TopologyDraft{
		Nodes:     nodes,
		Edges:     edges,
		Version:   time.Now().UnixNano(),
		UpdatedAt: time.Now().UTC(),
		Source:    "frame",
	}

	ws.topologyMu.Lock()
	ws.topologyCached = deepCopyDraft(draft)
	ws.topologyMu.Unlock()
	return draft
}

func validateTopologyDraft(draft *TopologyDraft) error {
	if draft == nil {
		return errors.New("topology draft is required")
	}
	if len(draft.Nodes) == 0 {
		return errors.New("topology must contain at least one node")
	}

	ids := make(map[int]struct{}, len(draft.Nodes))
	for _, node := range draft.Nodes {
		if _, exists := ids[node.ID]; exists {
			return errors.New("duplicate node id detected")
		}
		ids[node.ID] = struct{}{}
		if node.Label == "" {
			return errors.New("node label cannot be empty")
		}
		if node.Type == "" {
			return errors.New("node type cannot be empty")
		}
	}

	for _, edge := range draft.Edges {
		if _, ok := ids[edge.Source]; !ok {
			return errors.New("edge references unknown source node")
		}
		if _, ok := ids[edge.Target]; !ok {
			return errors.New("edge references unknown target node")
		}
	}
	return nil
}

func deepCopyDraft(draft *TopologyDraft) *TopologyDraft {
	if draft == nil {
		return nil
	}
	copied := &TopologyDraft{
		Nodes:     make([]TopologyNode, len(draft.Nodes)),
		Edges:     make([]TopologyEdge, len(draft.Edges)),
		Version:   draft.Version,
		UpdatedAt: draft.UpdatedAt,
		Source:    draft.Source,
	}
	for i := range draft.Nodes {
		copied.Nodes[i] = TopologyNode{
			ID:       draft.Nodes[i].ID,
			Label:    draft.Nodes[i].Label,
			Type:     draft.Nodes[i].Type,
			PosX:     draft.Nodes[i].PosX,
			PosY:     draft.Nodes[i].PosY,
			Metadata: copyMetadata(draft.Nodes[i].Metadata),
		}
	}
	copy(copied.Edges, draft.Edges)
	for i := range copied.Nodes {
		if len(draft.Nodes[i].Metadata) > 0 {
			copied.Nodes[i].Metadata = copyMetadata(draft.Nodes[i].Metadata)
		}
	}
	return copied
}

func copyMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

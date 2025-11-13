package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Readm/flow_sim/hooks"
)

type PolicyDraft struct {
	Incentives []string  `json:"incentives"`
	Version    int64     `json:"version"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type policyOption struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type policyResponse struct {
	Incentives policySection `json:"incentives"`
}

type policySection struct {
	Available []policyOption `json:"available"`
	Selected  []string       `json:"selected"`
	Version   int64          `json:"version"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

func (ws *WebServer) handlePolicy(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handlePolicyGet(w, r)
	case http.MethodPut:
		ws.handlePolicyPut(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ws *WebServer) handlePolicyGet(w http.ResponseWriter, r *http.Request) {
	response := ws.buildPolicyResponse()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (ws *WebServer) handlePolicyPut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Incentives []string `json:"incentives"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	available := ws.listIncentiveOptions()
	valid := make(map[string]struct{}, len(available))
	for _, opt := range available {
		valid[opt.Name] = struct{}{}
	}

	unique := make([]string, 0, len(req.Incentives))
	seen := make(map[string]struct{})
	for _, name := range req.Incentives {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := valid[trimmed]; !ok {
			http.Error(w, "Unknown incentive plugin: "+trimmed, http.StatusBadRequest)
			return
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}

	draft := &PolicyDraft{
		Incentives: unique,
		Version:    time.Now().UnixNano(),
		UpdatedAt:  time.Now().UTC(),
	}

	ws.policyMu.Lock()
	ws.policyDraft = draft
	ws.policyMu.Unlock()

	response := ws.buildPolicyResponse()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (ws *WebServer) buildPolicyResponse() policyResponse {
	options := ws.listIncentiveOptions()
	ws.policyMu.RLock()
	draft := ws.policyDraft
	ws.policyMu.RUnlock()

	selected := make([]string, 0)
	version := int64(0)
	updatedAt := time.Time{}
	if draft != nil {
		selected = append(selected, draft.Incentives...)
		version = draft.Version
		updatedAt = draft.UpdatedAt
	}

	return policyResponse{
		Incentives: policySection{
			Available: options,
			Selected:  selected,
			Version:   version,
			UpdatedAt: updatedAt,
		},
	}
}

func (ws *WebServer) listIncentiveOptions() []policyOption {
	ws.mu.RLock()
	reg := ws.pluginRegistry
	ws.mu.RUnlock()
	if reg == nil {
		return nil
	}
	broker := reg.Broker()
	if broker == nil {
		return nil
	}

	descriptors := broker.ListPlugins(hooks.PluginCategoryInstrumentation)
	options := make([]policyOption, 0, len(descriptors))
	for _, desc := range descriptors {
		if !strings.HasPrefix(desc.Name, "incentive/") {
			continue
		}
		name := strings.TrimPrefix(desc.Name, "incentive/")
		options = append(options, policyOption{
			Name:        name,
			Description: desc.Description,
		})
	}

	sort.Slice(options, func(i, j int) bool {
		return options[i].Name < options[j].Name
	})
	return options
}

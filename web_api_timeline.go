package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (ws *WebServer) handleTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ws.txnMgr == nil {
		http.Error(w, "Transaction manager not available", http.StatusServiceUnavailable)
		return
	}

	stateFilter := r.URL.Query().Get("state")
	summaries := ws.txnMgr.GetAllTransactionSummaries()
	if stateFilter != "" {
		filtered := make([]*TransactionSummary, 0)
		for _, s := range summaries {
			if string(s.State) == stateFilter {
				filtered = append(filtered, s)
			}
		}
		summaries = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(summaries); err != nil {
		http.Error(w, "Failed to encode transactions", http.StatusInternalServerError)
	}
}

func (ws *WebServer) handleTransactionTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ws.txnMgr == nil {
		http.Error(w, "Transaction manager not available", http.StatusServiceUnavailable)
		return
	}

	path := r.URL.Path
	prefix := "/api/transaction/"
	if !strings.HasPrefix(path, prefix) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	path = strings.TrimPrefix(path, prefix)
	path = strings.TrimSuffix(path, "/timeline")

	var txnID int64
	if _, err := fmt.Sscanf(path, "%d", &txnID); err != nil {
		http.Error(w, "Invalid transaction ID", http.StatusBadRequest)
		return
	}

	timeline := ws.txnMgr.GetTransactionTimeline(txnID)
	if timeline == nil {
		http.Error(w, "Transaction not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(timeline); err != nil {
		http.Error(w, "Failed to encode timeline", http.StatusInternalServerError)
	}
}

func (ws *WebServer) handleTransactionTimelines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ws.txnMgr == nil {
		http.Error(w, "Transaction manager not available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		TransactionIDs []int64 `json:"transactionIDs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	timelines := make([]*TransactionTimeline, 0, len(req.TransactionIDs))
	for _, txnID := range req.TransactionIDs {
		if timeline := ws.txnMgr.GetTransactionTimeline(txnID); timeline != nil {
			timelines = append(timelines, timeline)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(timelines); err != nil {
		http.Error(w, "Failed to encode timelines", http.StatusInternalServerError)
	}
}

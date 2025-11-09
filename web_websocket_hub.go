package main

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/websocket"
)

type wsHub struct {
	upgrader  websocket.Upgrader
	clients   map[*websocket.Conn]bool
	register  chan *websocket.Conn
	remove    chan *websocket.Conn
	broadcast chan []byte
}

func newHub() *wsHub {
	hub := &wsHub{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		clients:   make(map[*websocket.Conn]bool),
		register:  make(chan *websocket.Conn),
		remove:    make(chan *websocket.Conn),
		broadcast: make(chan []byte, 16),
	}
	go hub.run()
	return hub
}

func (h *wsHub) run() {
	for {
		select {
		case conn := <-h.register:
			h.clients[conn] = true
		case conn := <-h.remove:
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
		case msg := <-h.broadcast:
			for conn := range h.clients {
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					GetLogger().Warnf("Failed to send frame to WebSocket client: %v", err)
					delete(h.clients, conn)
					conn.Close()
				}
			}
		}
	}
}

func (h *wsHub) handle(ws *WebServer, w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		GetLogger().Errorf("WebSocket upgrade failed: %v", err)
		return
	}

	h.register <- conn

	ws.mu.RLock()
	if ws.latestFrame != nil {
		if data, err := json.Marshal(ws.latestFrame); err == nil {
			conn.WriteMessage(websocket.TextMessage, data)
		}
	}
	ws.mu.RUnlock()

	go func() {
		defer func() { h.remove <- conn }()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					GetLogger().Warnf("WebSocket error: %v", err)
				}
				break
			}

			var req controlRequest
			if err := json.Unmarshal(message, &req); err == nil {
				if cmd, err := ws.processControlRequest(&req); err == nil {
					ws.queueCommand(*cmd)
				}
			}
		}
	}()
}

func (h *wsHub) broadcastFrame(frame *SimulationFrame) {
	if frame == nil {
		return
	}
	data, err := json.Marshal(frame)
	if err != nil {
		GetLogger().Errorf("Failed to marshal frame for WebSocket: %v", err)
		return
	}
	h.broadcast <- data
}

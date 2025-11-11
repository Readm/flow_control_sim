package main

import "net/http"

// Router wires HTTP/WS handlers for the server.
type Router struct {
	mux *http.ServeMux
}

// NewRouter constructs router with provided handlers.
func NewRouter(server *WebServer) *Router {
	mux := http.NewServeMux()
	server.registerHandlers(mux)
	return &Router{mux: mux}
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r == nil || r.mux == nil {
		http.NotFound(w, req)
		return
	}
	r.mux.ServeHTTP(w, req)
}


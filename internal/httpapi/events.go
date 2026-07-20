package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// heartbeatInterval keeps the connection alive through proxies that time out
// idle streams, and lets the client notice a dead server.
const heartbeatInterval = 25 * time.Second

// handleEvents streams workspace changes as server-sent events.
//
// SSE rather than a WebSocket: the traffic is one-directional, SSE reconnects
// on its own, and it needs no protocol upgrade — which keeps the remote-mode
// story simpler behind a reverse proxy. Nothing here is load-bearing for
// correctness; the stream only makes the UI live (spec 02 section 3.4).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s.opts.Watcher == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "EVENTS_UNAVAILABLE",
			"This process is not watching the workspace for changes.")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, r, http.StatusInternalServerError, "EVENTS_UNAVAILABLE",
			"This server cannot stream events.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Defeats response buffering in reverse proxies, which would otherwise
	// hold events until the stream closed.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	changes, cancel := s.opts.Watcher.Subscribe()
	defer cancel()

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return

		case batch, open := <-changes:
			if !open {
				return
			}
			payload, err := json.Marshal(batch)
			if err != nil {
				s.log.Error("encode change batch", "error", err)
				continue
			}
			if _, err := fmt.Fprintf(w, "event: documents\ndata: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()

		case <-heartbeat.C:
			// A comment frame: valid SSE, ignored by EventSource.
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

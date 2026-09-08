package client

import (
	"context"
	"net/http"
	"time"
)

// readinessChecker is implemented by stores whose backing datastore can be
// reached out to. The file-backed JSON store does not implement it, so a
// deployment on that store always reports its datastore check as skipped.
type readinessChecker interface {
	Ping(ctx context.Context) error
}

// BeginDraining marks the server as shutting down. From that point /readyz
// reports NOT ready, so a load balancer or orchestrator stops routing new
// traffic here before in-flight connections are cut during shutdown.
func (s *Server) BeginDraining() {
	s.draining.Store(true)
}

// handleHealthz is an unauthenticated liveness probe: it returns 200 as long as
// the process can serve HTTP. It never touches the database.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": BuildVersion(),
	})
}

// handleReadyz is an unauthenticated readiness probe: it returns 200 only when
// the process is ready to take traffic, i.e. it is not draining and its backing
// store (if it has a reachable one) responds to a ping.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store, max-age=0")

	checks := map[string]string{}
	ready := true

	if s.draining.Load() {
		ready = false
		checks["accepting_traffic"] = "draining"
	} else {
		checks["accepting_traffic"] = "ok"
	}

	switch store := s.storeForHealth().(type) {
	case readinessChecker:
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := store.Ping(ctx); err != nil {
			ready = false
			checks["datastore"] = "unreachable"
		} else {
			checks["datastore"] = "ok"
		}
	default:
		checks["datastore"] = "skipped"
	}

	status := http.StatusOK
	state := "ready"
	if !ready {
		status = http.StatusServiceUnavailable
		state = "not_ready"
	}
	writeJSON(w, status, map[string]any{
		"status": state,
		"checks": checks,
	})
}

func (s *Server) storeForHealth() any {
	if s.managers == nil {
		return nil
	}
	return s.managers.Store()
}

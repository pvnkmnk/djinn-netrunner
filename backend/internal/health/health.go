// Package health serves a minimal HTTP liveness/readiness endpoint for
// headless binaries (worker, agent) that otherwise expose no port. Docker
// healthchecks hit /healthz: the handler only reports healthy when the
// process is up AND the database answers a trivial query, so a wedged DB
// connection marks the container unhealthy and the compose restart policy
// recovers it — a process-only check cannot do that.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"

	"gorm.io/gorm"
)

// Server runs the health endpoint. Use New to construct it.
type Server struct {
	srv  *http.Server
	addr string // resolved listener address (useful when configured as :0)
	db   *gorm.DB
}

// New starts an HTTP server on addr serving /healthz and returns immediately.
// The listener is bound before New returns, so a port conflict fails fast.
// Database may be nil, in which case the check only reports process liveness.
func New(addr string, db *gorm.DB) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	h := &Server{db: db, addr: ln.Addr().String()}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.handle)
	h.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := h.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Warn("health server stopped", "addr", addr, "error", err)
		}
	}()
	return h, nil
}

// Addr returns the resolved listen address (e.g. "127.0.0.1:53122").
func (h *Server) Addr() string { return h.addr }

func (h *Server) handle(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	body := map[string]string{"status": "ok"}

	if h.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := h.db.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
			status = http.StatusServiceUnavailable
			body["status"] = "degraded"
			body["database"] = "unreachable"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Stop gracefully shuts the server down.
func (h *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.srv.Shutdown(ctx); err != nil {
		slog.Warn("health server shutdown", "error", err)
	}
}

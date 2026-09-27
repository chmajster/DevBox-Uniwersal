package monitoring

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type Module struct {
	collector *Collector
}

func NewModule(collector *Collector) *Module {
	return &Module{collector: collector}
}

func (m *Module) Name() string { return "monitoring" }

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	viewer := func(handler http.Handler) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleViewer, handler))
	}
	mux.Handle("GET /api/v1/monitoring/snapshot", viewer(http.HandlerFunc(m.snapshot)))
	mux.Handle("GET /api/v1/monitoring/stream", viewer(http.HandlerFunc(m.stream)))
}

func (m *Module) snapshot(w http.ResponseWriter, r *http.Request) {
	snapshot, err := m.collector.Collect(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "monitoring_failed", err.Error(), nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, snapshot)
}

func (m *Module) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, "stream_unsupported", "HTTP response streaming is not supported", nil)
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		snapshot, err := m.collector.Collect(r.Context())
		if err != nil {
			payload, _ := json.Marshal(map[string]string{"message": err.Error()})
			_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
			flusher.Flush()
			return
		}
		payload, err := json.Marshal(snapshot)
		if err != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", payload); err != nil {
			return
		}
		flusher.Flush()

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

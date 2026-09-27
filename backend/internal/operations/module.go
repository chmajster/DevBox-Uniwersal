package operations

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type Module struct {
	registry *Registry
}

func NewModule(registry *Registry) *Module {
	return &Module{registry: registry}
}

func (m *Module) Name() string { return "operations" }

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	viewer := func(handler http.Handler) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleViewer, handler))
	}
	mux.Handle("GET /api/v1/logs/sources", viewer(http.HandlerFunc(m.listSources)))
	mux.Handle("GET /api/v1/logs", viewer(http.HandlerFunc(m.listLogs)))
	mux.Handle("GET /api/v1/logs/stream", viewer(http.HandlerFunc(m.streamLogs)))
	mux.Handle("GET /api/v1/jobs/{id}/logs", viewer(http.HandlerFunc(m.listJobLogs)))
	mux.Handle("GET /api/v1/jobs/{id}/logs/stream", viewer(http.HandlerFunc(m.streamJobLogs)))
}

func (m *Module) listSources(w http.ResponseWriter, _ *http.Request) {
	api.WriteJSON(w, http.StatusOK, m.registry.Sources())
}

func (m *Module) listLogs(w http.ResponseWriter, r *http.Request) {
	sourceName := r.URL.Query().Get("source")
	if sourceName == "" {
		sourceName = "devbox"
	}
	source, ok := m.registry.Source(sourceName)
	if !ok {
		api.WriteError(w, http.StatusNotFound, "source_unavailable", "log source is not available: "+sourceName, nil)
		return
	}
	filter := filterFromRequest(r)
	items, err := source.List(r.Context(), filter)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "logs_failed", err.Error(), nil)
		return
	}
	api.WriteJSONMeta(w, http.StatusOK, items, map[string]any{"source": sourceName, "limit": filter.Limit})
}

func (m *Module) listJobLogs(w http.ResponseWriter, r *http.Request) {
	source, ok := m.registry.Source("job")
	if !ok {
		api.WriteError(w, http.StatusNotFound, "source_unavailable", "job log source is not available", nil)
		return
	}
	filter := filterFromRequest(r)
	filter.JobID = r.PathValue("id")
	items, err := source.List(r.Context(), filter)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "logs_failed", err.Error(), nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, items)
}

func (m *Module) streamLogs(w http.ResponseWriter, r *http.Request) {
	sourceName := r.URL.Query().Get("source")
	if sourceName == "" {
		sourceName = "devbox"
	}
	source, ok := m.registry.Source(sourceName)
	if !ok {
		api.WriteError(w, http.StatusNotFound, "source_unavailable", "log source is not available: "+sourceName, nil)
		return
	}
	m.streamSource(w, r, source, filterFromRequest(r))
}

func (m *Module) streamJobLogs(w http.ResponseWriter, r *http.Request) {
	source, ok := m.registry.Source("job")
	if !ok {
		api.WriteError(w, http.StatusNotFound, "source_unavailable", "job log source is not available", nil)
		return
	}
	filter := filterFromRequest(r)
	filter.JobID = r.PathValue("id")
	m.streamSource(w, r, source, filter)
}

func (m *Module) streamSource(w http.ResponseWriter, r *http.Request, source LogSource, filter LogFilter) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		api.WriteError(w, http.StatusInternalServerError, "stream_unsupported", "HTTP response streaming is not supported", nil)
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		items, err := source.List(r.Context(), filter)
		if err != nil {
			payload, _ := json.Marshal(map[string]string{"message": err.Error()})
			_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
			flusher.Flush()
			return
		}
		for _, item := range items {
			payload, err := json.Marshal(item)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: log\nid: %d\ndata: %s\n\n", item.Cursor, payload); err != nil {
				return
			}
			if item.Cursor > filter.AfterCursor {
				filter.AfterCursor = item.Cursor
			}
		}
		flusher.Flush()

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func filterFromRequest(r *http.Request) LogFilter {
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	after, _ := strconv.ParseInt(query.Get("after"), 10, 64)
	return LogFilter{
		ProjectID:   query.Get("project"),
		JobID:       query.Get("job"),
		Level:       query.Get("level"),
		Search:      query.Get("search"),
		AfterCursor: after,
		Limit:       normalizeLimit(limit),
	}
}

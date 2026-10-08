package applications

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/http"
)

func (m *Module) exportRuntime(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := m.service.Get(r.Context(), id); err != nil {
		m.fail(w, err)
		return
	}
	reader, ok := m.service.logs.(interface {
		RuntimeArtifacts(string) (map[string][]byte, error)
	})
	if !ok {
		m.fail(w, ErrProviderUnavailable)
		return
	}
	files, err := reader.RuntimeArtifacts(id)
	if err != nil {
		m.fail(w, fmt.Errorf("%w: deploy before exporting runtime files", ErrNotFound))
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="devbox-runtime.zip"`)
	archive := zip.NewWriter(w)
	defer archive.Close()
	for _, name := range []string{"Dockerfile", "compose.yaml", "metadata.json"} {
		data, exists := files[name]
		if !exists {
			continue
		}
		entry, err := archive.Create(name)
		if err != nil {
			return
		}
		if _, err := entry.Write(data); err != nil {
			return
		}
	}
}

func (m *Module) stats(w http.ResponseWriter, r *http.Request) {
	app, err := m.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	provider, ok := m.service.logs.(interface {
		ApplicationStats(*http.Request, []string) ([]json.RawMessage, error)
	})
	if !ok {
		m.fail(w, ErrProviderUnavailable)
		return
	}
	ids := []string{}
	for _, workload := range app.Workloads {
		if workload.DriverResourceID != "" {
			ids = append(ids, workload.DriverResourceID)
		}
	}
	values, err := provider.ApplicationStats(r, ids)
	if err != nil {
		m.fail(w, err)
		return
	}
	writeApplicationData(w, http.StatusOK, values)
}

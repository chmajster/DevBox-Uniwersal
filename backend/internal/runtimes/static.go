package runtimes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

type staticServerState struct {
	server    *http.Server
	listener  net.Listener
	startedAt time.Time
}

type StaticRuntime struct {
	mu      sync.RWMutex
	servers map[string]*staticServerState
}

func NewStaticRuntime() *StaticRuntime {
	return &StaticRuntime{servers: make(map[string]*staticServerState)}
}

func (r *StaticRuntime) Name() string { return "static" }

func (r *StaticRuntime) Inspect(context.Context) RuntimeInfo {
	return RuntimeInfo{
		Runtime: "static",
		Status:  AvailabilityAvailable,
		Version: "builtin",
		Dependencies: []DependencyInfo{
			{Name: "devbox-http", Status: AvailabilityAvailable, Version: "builtin"},
		},
	}
}

func (r *StaticRuntime) Detect(_ context.Context, project ProjectContext) (Detection, error) {
	if err := validateWorkDir(project); err != nil {
		return Detection{}, err
	}
	if !fileExists(project.WorkDir, "index.html") {
		return Detection{Runtime: r.Name()}, nil
	}
	return newDetection(
		r.Name(),
		"Static",
		50,
		[]string{"index.html"},
		"",
		"DevBox built-in static HTTP server",
		map[string]any{"document_root": "."},
	), nil
}

func (r *StaticRuntime) Validate(_ context.Context, project ProjectContext) (ValidationResult, error) {
	result := ValidationResult{Valid: true}
	if err := validateWorkDir(project); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	if !fileExists(project.WorkDir, "index.html") {
		result.Valid = false
		result.Errors = append(result.Errors, "index.html was not found")
	}
	if _, ok := projectPort(project); !ok {
		result.Warnings = append(result.Warnings, "runtime port is not configured; Start requires a project port")
	}
	return result, nil
}

func (r *StaticRuntime) InstallDependencies(context.Context, ProjectContext) error { return nil }
func (r *StaticRuntime) Build(context.Context, ProjectContext) error               { return nil }

func (r *StaticRuntime) Start(_ context.Context, project ProjectContext) error {
	if err := validateWorkDir(project); err != nil {
		return err
	}
	port, ok := projectPort(project)
	if !ok {
		return fmt.Errorf("runtime port is not configured")
	}
	key := processName(project)

	r.mu.RLock()
	_, exists := r.servers[key]
	r.mu.RUnlock()
	if exists {
		return fmt.Errorf("static runtime is already running for project %s", project.ProjectID)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("listen on static runtime port: %w", err)
	}
	server := &http.Server{
		Handler:           http.FileServer(http.Dir(project.WorkDir)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	state := &staticServerState{server: server, listener: listener, startedAt: time.Now().UTC()}
	r.mu.Lock()
	r.servers[key] = state
	r.mu.Unlock()

	go func() {
		_ = server.Serve(listener)
		r.mu.Lock()
		if current := r.servers[key]; current == state {
			delete(r.servers, key)
		}
		r.mu.Unlock()
	}()
	return nil
}

func (r *StaticRuntime) Stop(ctx context.Context, project ProjectContext) error {
	key := processName(project)
	r.mu.RLock()
	state, exists := r.servers[key]
	r.mu.RUnlock()
	if !exists {
		return nil
	}
	if err := state.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("stop static runtime: %w", err)
	}
	r.mu.Lock()
	if current := r.servers[key]; current == state {
		delete(r.servers, key)
	}
	r.mu.Unlock()
	return nil
}

func (r *StaticRuntime) Restart(ctx context.Context, project ProjectContext) error {
	if err := r.Stop(ctx, project); err != nil {
		return err
	}
	return r.Start(ctx, project)
}

func (r *StaticRuntime) Status(_ context.Context, project ProjectContext) (ProcessStatus, error) {
	key := processName(project)
	r.mu.RLock()
	state, exists := r.servers[key]
	r.mu.RUnlock()
	if !exists {
		return ProcessStatus{State: "stopped"}, nil
	}
	startedAt := state.startedAt
	return ProcessStatus{State: "running", StartedAt: &startedAt}, nil
}

func (r *StaticRuntime) Logs(context.Context, ProjectContext, LogOptions) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (r *StaticRuntime) HealthCheck(ctx context.Context, project ProjectContext) (HealthResult, error) {
	checkedAt := time.Now().UTC()
	port, ok := projectPort(project)
	if !ok {
		return HealthResult{Healthy: false, Message: "runtime port is not configured", CheckedAt: checkedAt}, nil
	}
	started := time.Now()
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodHead, fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
	if err != nil {
		return HealthResult{}, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return HealthResult{Healthy: false, Message: err.Error(), Latency: time.Since(started), CheckedAt: checkedAt}, nil
	}
	defer response.Body.Close()
	return HealthResult{
		Healthy:   response.StatusCode >= 200 && response.StatusCode < 500,
		Message:   response.Status,
		Latency:   time.Since(started),
		CheckedAt: checkedAt,
	}, nil
}

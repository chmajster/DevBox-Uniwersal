package runtimes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type managedProcess struct {
	spec      providers.ProcessSpec
	cmd       *exec.Cmd
	state     string
	pid       *int
	startedAt *time.Time
	exitCode  *int
	logPath   string
	done      chan struct{}
}

type LocalProcessManager struct {
	mu        sync.RWMutex
	processes map[string]*managedProcess
}

func NewLocalProcessManager() *LocalProcessManager {
	return &LocalProcessManager{processes: make(map[string]*managedProcess)}
}

func (m *LocalProcessManager) Start(ctx context.Context, spec providers.ProcessSpec) (providers.ProcessInfo, error) {
	if err := ctx.Err(); err != nil {
		return providers.ProcessInfo{}, err
	}
	if spec.Name == "" || spec.Command == "" || spec.WorkDir == "" {
		return providers.ProcessInfo{}, fmt.Errorf("process name, command and work directory are required")
	}

	m.mu.Lock()
	if current, exists := m.processes[spec.Name]; exists && current.state == "running" {
		m.mu.Unlock()
		return providers.ProcessInfo{}, fmt.Errorf("process %q is already running", spec.Name)
	}
	m.mu.Unlock()

	logDir := filepath.Join(spec.WorkDir, ".devbox", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return providers.ProcessInfo{}, fmt.Errorf("create runtime log directory: %w", err)
	}
	logPath := filepath.Join(logDir, spec.Name+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return providers.ProcessInfo{}, fmt.Errorf("open runtime log: %w", err)
	}

	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = spec.WorkDir
	cmd.Env = mergedEnvironment(spec.Environment)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return providers.ProcessInfo{}, fmt.Errorf("start process %q: %w", spec.Name, err)
	}

	now := time.Now().UTC()
	pidValue := cmd.Process.Pid
	process := &managedProcess{
		spec:      spec,
		cmd:       cmd,
		state:     "running",
		pid:       &pidValue,
		startedAt: &now,
		logPath:   logPath,
		done:      make(chan struct{}),
	}

	m.mu.Lock()
	m.processes[spec.Name] = process
	m.mu.Unlock()

	go func() {
		err := cmd.Wait()
		_ = logFile.Close()
		exitCode := 0
		state := "stopped"
		if err != nil {
			state = "failed"
			var exitError *exec.ExitError
			if errors.As(err, &exitError) {
				exitCode = exitError.ExitCode()
			} else {
				exitCode = -1
			}
		} else if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}

		m.mu.Lock()
		if current := m.processes[spec.Name]; current == process {
			current.state = state
			current.exitCode = &exitCode
			current.pid = nil
		}
		m.mu.Unlock()
		close(process.done)
	}()

	return providers.ProcessInfo{Name: spec.Name, State: "running", PID: &pidValue, StartedAt: &now}, nil
}

func (m *LocalProcessManager) Stop(ctx context.Context, name string) error {
	m.mu.RLock()
	process, exists := m.processes[name]
	running := exists && process.state == "running" && process.cmd != nil && process.cmd.Process != nil
	m.mu.RUnlock()
	if !running {
		return nil
	}

	_ = process.cmd.Process.Signal(os.Interrupt)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-process.done:
		return nil
	case <-ctx.Done():
		_ = process.cmd.Process.Kill()
		return ctx.Err()
	case <-timer.C:
		if err := process.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("kill process %q: %w", name, err)
		}
	}

	select {
	case <-process.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *LocalProcessManager) Restart(ctx context.Context, name string) error {
	m.mu.RLock()
	process, exists := m.processes[name]
	if !exists {
		m.mu.RUnlock()
		return fmt.Errorf("process %q has not been started", name)
	}
	spec := process.spec
	m.mu.RUnlock()

	if err := m.Stop(ctx, name); err != nil {
		return err
	}
	_, err := m.Start(ctx, spec)
	return err
}

func (m *LocalProcessManager) Status(_ context.Context, name string) (providers.ProcessInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	process, exists := m.processes[name]
	if !exists {
		return providers.ProcessInfo{Name: name, State: "stopped"}, nil
	}
	return providers.ProcessInfo{
		Name:      name,
		State:     process.state,
		PID:       process.pid,
		StartedAt: process.startedAt,
	}, nil
}

func (m *LocalProcessManager) Logs(ctx context.Context, name string, tail int, follow bool) (io.ReadCloser, error) {
	m.mu.RLock()
	process, exists := m.processes[name]
	m.mu.RUnlock()
	if !exists || process.logPath == "" {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}

	if !follow {
		content, err := os.ReadFile(process.logPath)
		if err != nil {
			return nil, fmt.Errorf("read runtime log: %w", err)
		}
		return io.NopCloser(bytes.NewReader(tailLines(content, tail))), nil
	}

	file, err := os.Open(process.logPath)
	if err != nil {
		return nil, fmt.Errorf("open runtime log: %w", err)
	}
	content, err := os.ReadFile(process.logPath)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	offset := int64(len(content) - len(tailLines(content, tail)))
	if offset < 0 {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, err
	}
	followCtx, cancel := context.WithCancel(ctx)
	return &pollingFileReader{file: file, ctx: followCtx, cancel: cancel}, nil
}

type pollingFileReader struct {
	file   *os.File
	ctx    context.Context
	cancel context.CancelFunc
}

func (r *pollingFileReader) Read(buffer []byte) (int, error) {
	for {
		count, err := r.file.Read(buffer)
		if count > 0 {
			return count, nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return count, err
		}
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (r *pollingFileReader) Close() error {
	r.cancel()
	return r.file.Close()
}

func tailLines(content []byte, tail int) []byte {
	if tail <= 0 || len(content) == 0 {
		return content
	}
	hasTrailingNewline := bytes.HasSuffix(content, []byte("\n"))
	trimmed := bytes.TrimSuffix(content, []byte("\n"))
	lines := bytes.Split(trimmed, []byte("\n"))
	if len(lines) <= tail {
		return content
	}
	result := bytes.Join(lines[len(lines)-tail:], []byte("\n"))
	if hasTrailingNewline {
		result = append(result, '\n')
	}
	return result
}

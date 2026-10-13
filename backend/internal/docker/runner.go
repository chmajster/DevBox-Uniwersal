package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

type commandRunner interface {
	Run(ctx context.Context, args ...string) ([]byte, []byte, error)
	Stream(ctx context.Context, args ...string) (io.ReadCloser, error)
}

type execRunner struct {
	binary string
}

func (r execRunner) Run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	return r.run(ctx, nil, args...)
}

func (r execRunner) RunEnv(ctx context.Context, environment map[string]string, args ...string) ([]byte, []byte, error) {
	return r.run(ctx, environment, args...)
}

func (r execRunner) run(ctx context.Context, environment map[string]string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, r.binary, args...)
	if len(environment) > 0 {
		cmd.Env = mergeProcessEnvironment(os.Environ(), environment)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Introspection may contain plaintext environment values. Only lifecycle
	// output is forwarded to a job, whose sink masks resolved secret values.
	loggable := lifecycleOutputAllowed(args)
	if loggable {
		outSink := &jobOutputWriter{ctx: ctx, stream: "stdout"}
		errSink := &jobOutputWriter{ctx: ctx, stream: "stderr"}
		cmd.Stdout = io.MultiWriter(&stdout, outSink)
		cmd.Stderr = io.MultiWriter(&stderr, errSink)
		defer outSink.Flush()
		defer errSink.Flush()
	}
	err := cmd.Run()
	if err != nil {
		return stdout.Bytes(), stderr.Bytes(), commandError(r.binary, stderr.String(), err)
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

func mergeProcessEnvironment(base []string, overrides map[string]string) []string {
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, replaced := overrides[key]; replaced {
				continue
			}
		}
		result = append(result, entry)
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, key+"="+overrides[key])
	}
	return result
}

func (r execRunner) Stream(ctx context.Context, args ...string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, r.binary, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, commandError(r.binary, "", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return &commandStream{ReadCloser: stdout, cmd: cmd, done: done}, nil
}

type commandStream struct {
	io.ReadCloser
	cmd  *exec.Cmd
	done chan error
	once sync.Once
	err  error
}

func (s *commandStream) Close() error {
	s.once.Do(func() {
		_ = s.ReadCloser.Close()
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		err := <-s.done
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) && !errors.Is(err, context.Canceled) {
				s.err = err
			}
		}
	})
	return s.err
}

func commandError(binary, stderr string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%w: %s CLI not found", ErrUnavailable, binary)
	}
	message := compactCommandOutput(stderr, 6000)
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "cannot connect to the docker daemon"),
		strings.Contains(lower, "is the docker daemon running"),
		strings.Contains(lower, "error during connect"),
		strings.Contains(lower, "the system cannot find the file specified"):
		return fmt.Errorf("%w: %s", ErrUnavailable, message)
	case dockerResourceNotFound(lower):
		return fmt.Errorf("%w: %s", ErrNotFound, message)
	}
	if message == "" {
		message = err.Error()
	}
	return fmt.Errorf("docker command failed: %s", message)
}

func compactCommandOutput(value string, limit int) string {
	message := strings.TrimSpace(value)
	if message == "" || limit <= 0 {
		return message
	}
	runes := []rune(message)
	if len(runes) <= limit {
		return message
	}

	head := limit / 3
	tail := limit - head
	return strings.TrimSpace(string(runes[:head])) +
		"\n... docker output truncated; final error follows ...\n" +
		strings.TrimSpace(string(runes[len(runes)-tail:]))
}

func dockerResourceNotFound(message string) bool {
	for _, marker := range []string{
		"no such container",
		"no such image",
		"no such volume",
		"no such network",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	if !strings.Contains(message, " not found") {
		return false
	}
	for _, resource := range []string{"container ", "image ", "volume ", "network "} {
		if strings.Contains(message, resource) {
			return true
		}
	}
	return false
}

type jobOutputWriter struct {
	ctx     context.Context
	stream  string
	pending string
}

func (w *jobOutputWriter) Write(data []byte) (int, error) {
	w.pending += string(data)
	for {
		line, rest, ok := strings.Cut(w.pending, "\n")
		if !ok {
			break
		}
		jobs.EmitOutput(w.ctx, w.stream, line)
		w.pending = rest
	}
	if len(w.pending) > 16384 {
		jobs.EmitOutput(w.ctx, w.stream, w.pending)
		w.pending = ""
	}
	return len(data), nil
}

func (w *jobOutputWriter) Flush() {
	if w.pending != "" {
		jobs.EmitOutput(w.ctx, w.stream, w.pending)
		w.pending = ""
	}
}

func lifecycleOutputAllowed(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "build" {
		return true
	}
	if args[0] == "buildx" {
		return len(args) > 1 && args[1] == "build"
	}
	if args[0] != "compose" {
		return false
	}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "-p" || arg == "-f" || arg == "--project-name" || arg == "--env-file" || arg == "--project-directory" {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg == "up" || arg == "build" || arg == "pull"
	}
	return false
}

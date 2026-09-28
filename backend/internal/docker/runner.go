package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
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
	cmd := exec.CommandContext(ctx, r.binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.Bytes(), stderr.Bytes(), commandError(r.binary, stderr.String(), err)
	}
	return stdout.Bytes(), stderr.Bytes(), nil
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
	message := strings.TrimSpace(stderr)
	if len(message) > 600 {
		message = message[:600]
	}
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "cannot connect to the docker daemon"),
		strings.Contains(lower, "is the docker daemon running"),
		strings.Contains(lower, "error during connect"),
		strings.Contains(lower, "the system cannot find the file specified"):
		return fmt.Errorf("%w: %s", ErrUnavailable, message)
	case strings.Contains(lower, "no such container"),
		strings.Contains(lower, "no such image"),
		strings.Contains(lower, "no such volume"),
		strings.Contains(lower, "no such network"):
		return fmt.Errorf("%w: %s", ErrNotFound, message)
	}
	if message == "" {
		message = err.Error()
	}
	return fmt.Errorf("docker command failed: %s", message)
}

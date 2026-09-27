package system

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type fakeRunner struct {
	paths   map[string]string
	outputs map[string]string
	errors  map[string]error
}

func (f fakeRunner) LookPath(file string) (string, error) {
	if path, ok := f.paths[file]; ok {
		return path, nil
	}
	return "", errors.New("not found")
}

func (f fakeRunner) CombinedOutput(_ context.Context, name string, _ ...string) ([]byte, error) {
	return []byte(f.outputs[name]), f.errors[name]
}

func TestParseVersionOutput(t *testing.T) {
	cases := map[string]string{
		"git version 2.45.2\n":                 "git version 2.45.2",
		"\r\nPHP 8.3.6 (cli)\r\nCopyright\r\n": "PHP 8.3.6 (cli)",
		"":                                     "",
	}
	for input, want := range cases {
		if got := ParseVersionOutput(input); got != want {
			t.Fatalf("ParseVersionOutput(%q)=%q want %q", input, got, want)
		}
	}
}

func TestDetectComponentsWithRunner(t *testing.T) {
	paths := map[string]string{}
	outputs := map[string]string{}
	for _, spec := range componentSpecs {
		candidate := spec.candidates[0]
		path := fmt.Sprintf("/usr/bin/%s", candidate)
		paths[candidate] = path
		outputs[path] = candidate + " test-version\n"
	}
	statuses := DetectComponentsWithRunner(context.Background(), fakeRunner{paths: paths, outputs: outputs, errors: map[string]error{}})
	if len(statuses) != len(componentSpecs) {
		t.Fatalf("got %d statuses want %d", len(statuses), len(componentSpecs))
	}
	for _, status := range statuses {
		if !status.Installed || status.State != "available" || status.Version == "" {
			t.Fatalf("unexpected status: %+v", status)
		}
	}
}

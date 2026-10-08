package runtimes

import (
	"context"
	"strings"
)

type GoRuntime struct{}

func NewGoRuntime() *GoRuntime    { return &GoRuntime{} }
func (r *GoRuntime) Name() string { return "go" }

func (r *GoRuntime) Detect(_ context.Context, project ProjectContext) (Detection, error) {
	if err := validateWorkDir(project); err != nil {
		return Detection{}, err
	}
	content, err := readProjectFile(project.WorkDir, "go.mod")
	if err != nil {
		return Detection{}, err
	}
	if content == nil {
		return Detection{Runtime: r.Name()}, nil
	}
	version := ""
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "go" {
			version = fields[1]
			break
		}
	}
	detection := newDetection(
		r.Name(),
		"Go",
		96,
		[]string{"go.mod"},
		"go mod download && go build -o .devbox/build/app .",
		".devbox/build/app",
		map[string]any{"binary": "/app/server"},
	)
	detection.Version = version
	return detection, nil
}

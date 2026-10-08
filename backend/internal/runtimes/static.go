package runtimes

import (
	"context"
)

type StaticRuntime struct{}

func NewStaticRuntime() *StaticRuntime { return &StaticRuntime{} }
func (r *StaticRuntime) Name() string  { return "static" }

func (r *StaticRuntime) Detect(_ context.Context, project ProjectContext) (Detection, error) {
	if err := validateWorkDir(project); err != nil {
		return Detection{}, err
	}
	if !fileExists(project.WorkDir, "index.html") && !fileExists(project.WorkDir, "index.htm") {
		return Detection{Runtime: r.Name()}, nil
	}
	return newDetection(
		r.Name(),
		"Static",
		50,
		[]string{"index.html", "index.htm"},
		"",
		"nginx -g 'daemon off;'",
		map[string]any{"document_root": "."},
	), nil
}

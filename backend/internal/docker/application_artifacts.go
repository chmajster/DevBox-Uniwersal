package docker

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

func (p *CLIProvider) RuntimeArtifacts(id string) (map[string][]byte, error) {
	if p.runtimeRoot == "" {
		return nil, ErrNotFound
	}
	if err := validateProjectName(id); err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, name := range []string{"Dockerfile", "compose.yaml", "metadata.json"} {
		data, err := os.ReadFile(filepath.Join(p.runtimeRoot, id, "runtime", name))
		if err != nil {
			return nil, err
		}
		files[name] = data
	}
	return files, nil
}

func (p *CLIProvider) ApplicationStats(request *http.Request, ids []string) ([]json.RawMessage, error) {
	if len(ids) == 0 {
		return []json.RawMessage{}, nil
	}
	args := []string{"stats", "--no-stream", "--format", "{{json .}}"}
	for _, id := range ids {
		if err := validateContainerRef(id); err != nil {
			return nil, err
		}
		args = append(args, id)
	}
	out, _, err := p.runner.Run(request.Context(), args...)
	if err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err := decodeJSONLines(out, &items); err != nil {
		return nil, err
	}
	return items, nil
}

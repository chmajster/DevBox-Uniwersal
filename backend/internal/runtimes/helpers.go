package runtimes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func newDetection(runtimeName, framework string, confidence int, files []string, buildCommand, startCommand string, metadata map[string]any) Detection {
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["framework"] = framework
	metadata["confidence"] = confidence
	metadata["detected_files"] = append([]string(nil), files...)
	metadata["suggested_build_command"] = buildCommand
	metadata["suggested_start_command"] = startCommand
	return Detection{Detected: true, Runtime: runtimeName, Metadata: metadata}
}

func detectionConfidence(detection Detection) int {
	value, ok := detection.Metadata["confidence"]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}

func detectionString(detection Detection, key string) string {
	value, _ := detection.Metadata[key].(string)
	return value
}

func detectionStrings(detection Detection, key string) []string {
	value, ok := detection.Metadata[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func fileExists(root, name string) bool {
	info, err := os.Stat(filepath.Join(root, name))
	return err == nil && !info.IsDir()
}

func readProjectFile(root, name string) ([]byte, error) {
	content, err := os.ReadFile(filepath.Join(root, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return content, nil
}

func existingFiles(root string, names ...string) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		if fileExists(root, name) {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}

func firstExistingFile(root string, names ...string) string {
	for _, name := range names {
		if fileExists(root, name) {
			return name
		}
	}
	return ""
}

func validateWorkDir(project ProjectContext) error {
	if strings.TrimSpace(project.WorkDir) == "" {
		return fmt.Errorf("project work directory is not configured")
	}
	info, err := os.Stat(project.WorkDir)
	if err != nil {
		return fmt.Errorf("project work directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("project work directory is not a directory")
	}
	return nil
}

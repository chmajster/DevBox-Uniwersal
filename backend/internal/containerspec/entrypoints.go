package containerspec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type StartOption struct {
	Name      string
	Framework string
	Command   string
}

// Source inspection never imports or executes application code. It deliberately
// skips symlinks, dependency trees and deep/large trees. Unknown targets remain
// editable commands, rather than guessed application objects.
func StartOptions(root, runtime, profile string, port int, reload bool) []StartOption {
	p := strconv.Itoa(port)
	switch NormalizeRuntime(runtime) {
	case "go":
		packages := map[string]bool{}
		inspectSourceFiles(root, ".go", func(name string, data []byte) {
			dir := filepath.ToSlash(filepath.Dir(name))
			if dir != "." && dir != "cmd" && !strings.HasPrefix(dir, "cmd/") {
				return
			}
			file, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
			if err != nil || file.Name.Name != "main" || strings.HasSuffix(name, "_test.go") {
				return
			}
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "main" {
					packages[dir] = true
				}
			}
		})
		out := []StartOption{}
		for dir := range packages {
			target := "."
			if dir != "." {
				target = "./" + dir
			}
			if !safeSourceTarget.MatchString(target) {
				continue
			}
			program := "devbox-go-start"
			if reload {
				program = "devbox-go-watch"
			}
			out = append(out, StartOption{target, "Go", program + " " + target})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out
	case "python":
		out := []StartOption{}
		inspectSourceFiles(root, ".py", func(name string, data []byte) {
			name = filepath.ToSlash(name)
			module := strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(name, "src/"), ".py"), "/", ".")
			if !pythonModule.MatchString(module) {
				return
			}
			if name == "manage.py" {
				command := "python manage.py runserver 0.0.0.0:" + p
				if !reload {
					command += " --noreload"
				}
				out = append(out, StartOption{name, "Django", command})
				return
			}
			matches := pythonHTTPObject.FindAllSubmatch(data, -1)
			for _, match := range matches {
				object, framework := string(match[1]), string(match[2])
				command := "python -m uvicorn " + module + ":" + object + " --host 0.0.0.0 --port " + p
				if framework == "Flask" {
					command = "python -m flask --app " + module + ":" + object + " run --host 0.0.0.0 --port " + p
					if reload {
						command += " --reload"
					}
				} else if reload {
					command += " --reload"
				}
				out = append(out, StartOption{module + ":" + object, framework, command})
			}
			if len(matches) == 0 && (name == "main.py" || name == "app.py") {
				out = append(out, StartOption{name, "Python", "python " + name})
			}
		})
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out
	default:
		if command := legacySuggestedCommand(root, runtime, profile, port); command != "" {
			return []StartOption{{Name: runtime, Command: command}}
		}
		return nil
	}
}

// ApplyDevelopmentReload only changes known development commands. A custom
// executable keeps responsibility for its own restart/reload behavior.
func ApplyDevelopmentReload(command []string, runtime string, reload bool) []string {
	if len(command) == 0 {
		return command
	}
	result := append([]string(nil), command...)
	if runtime == "go" && (result[0] == "devbox-go-start" || result[0] == "devbox-go-watch") {
		result[0] = "devbox-go-start"
		if reload {
			result[0] = "devbox-go-watch"
		}
		return result
	}
	if runtime != "python" {
		return result
	}
	flag, opposite := "", ""
	if len(result) > 2 && result[0] == "python" && result[1] == "-m" && result[2] == "uvicorn" {
		if reload {
			flag = "--reload"
		}
		opposite = "--reload"
	} else if len(result) > 2 && result[0] == "python" && result[1] == "manage.py" && result[2] == "runserver" {
		if !reload {
			flag = "--noreload"
		}
		opposite = "--noreload"
	} else if len(result) > 2 && result[0] == "python" && result[1] == "-m" && result[2] == "flask" {
		flag, opposite = "--no-reload", "--reload"
		if reload {
			flag, opposite = "--reload", "--no-reload"
		}
	}
	if opposite == "" {
		return result
	}
	filtered := result[:0]
	for _, arg := range result {
		if arg != opposite && arg != flag {
			filtered = append(filtered, arg)
		}
	}
	if flag != "" {
		filtered = append(filtered, flag)
	}
	return filtered
}

var pythonHTTPObject = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*)\s*(?::[^=\n]+)?\s*=\s*(?:[A-Za-z_][A-Za-z0-9_]*\.)?(FastAPI|Starlette|Flask)\s*\(`)
var pythonModule = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
var safeSourceTarget = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

func inspectSourceFiles(root, extension string, inspect func(string, []byte)) {
	visited := 0
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		visited++
		if visited > 2000 {
			return fs.SkipAll
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if entry.IsDir() && (len(parts) > 4 || managedSourceExcluded(entry.Name())) {
			return fs.SkipDir
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(rel, extension) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil {
			inspect(rel, data)
		}
		return nil
	})
}

func managedSourceExcluded(name string) bool {
	return strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "venv" || name == "__pycache__"
}

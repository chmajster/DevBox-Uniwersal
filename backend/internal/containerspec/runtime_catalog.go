package containerspec

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// RuntimeCatalog is the single source of runtime choices for the API, detector
// and image generator. Versions are image tags, never host package versions.
type RuntimeOption struct {
	Name           string   `json:"name"`
	Label          string   `json:"label"`
	Versions       []string `json:"versions"`
	DefaultVersion string   `json:"default_version"`
	ContainerPort  int      `json:"container_port"`
	ImageTemplate  string   `json:"image_template"`
}

func RuntimeCatalog() []RuntimeOption {
	return []RuntimeOption{
		{"php", "PHP", []string{"8.4", "8.3", "8.2"}, "8.4", 8080, "php:{version}-apache-bookworm"},
		{"node", "Node.js", []string{"24", "22", "20"}, "22", 8080, "node:{version}-bookworm-slim"},
		{"python", "Python", []string{"3.13", "3.12", "3.11"}, "3.13", 8080, "python:{version}-slim-bookworm"},
		{"go", "Go", []string{"1.26", "1.25", "1.24", "1.23"}, "1.26", 8080, "golang:{version}-bookworm"},
		{"static", "Static HTML (nginx)", []string{"1.28", "1.27"}, "1.28", 8080, "nginxinc/nginx-unprivileged:{version}-alpine"},
	}
}

// ParseStartCommand uses a small argv grammar with quotes and escaping. Shell
// expansion/operators are intentionally unsupported; Docker receives argv.
func ParseStartCommand(command string) ([]string, error) {
	if len(command) > 4096 || strings.ContainsAny(command, "\x00\r\n") {
		return nil, fmt.Errorf("invalid start command")
	}
	var args []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	for _, ch := range command {
		if escaped {
			word.WriteRune(ch)
			escaped = false
			started = true
			continue
		}
		if ch == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				word.WriteRune(ch)
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
			started = true
		case ' ', '\t':
			if started {
				args = append(args, word.String())
				word.Reset()
				started = false
			}
		case ';', '|', '&', '<', '>', '`', '$':
			return nil, fmt.Errorf("shell operators are unsupported; provide an executable and arguments")
		default:
			word.WriteRune(ch)
			started = true
		}
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("unclosed quote or escape in start command")
	}
	if started {
		args = append(args, word.String())
	}
	if len(args) > 0 && (args[0] == "" || strings.HasPrefix(args[0], "-")) {
		return nil, fmt.Errorf("start command requires an executable")
	}
	return args, nil
}

func SuggestedCommand(root, runtime, profile string, port int) string {
	has := func(name string) bool {
		info, err := os.Stat(filepath.Join(root, name))
		return err == nil && info.Mode().IsRegular()
	}
	p := strconv.Itoa(port)
	if profile == "wordpress" {
		return "apache2-foreground"
	}
	switch NormalizeRuntime(runtime) {
	case "php":
		return "apache2-foreground"
	case "node":
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		data, _ := os.ReadFile(filepath.Join(root, "package.json"))
		_ = json.Unmarshal(data, &pkg)
		if pkg.Scripts["start"] != "" {
			return "npm start"
		}
		if pkg.Scripts["dev"] != "" {
			return "npm run dev -- --host 0.0.0.0 --port " + p
		}
		if has("server.js") {
			return "node server.js"
		}
		if has("index.js") {
			return "node index.js"
		}
	case "python":
		for _, entry := range []string{"main", "app"} {
			if data, err := os.ReadFile(filepath.Join(root, entry+".py")); err == nil {
				if strings.Contains(string(data), "FastAPI(") || strings.Contains(string(data), "Starlette(") {
					return "python -m uvicorn " + entry + ":app --host 0.0.0.0 --port " + p
				}
				if strings.Contains(string(data), "Flask(") {
					return "python -m flask --app " + entry + " run --host 0.0.0.0 --port " + p
				}
			}
		}
		if has("manage.py") {
			return "python manage.py runserver 0.0.0.0:" + p
		}
		if has("main.py") {
			data, _ := os.ReadFile(filepath.Join(root, "main.py"))
			if strings.Contains(string(data), "FastAPI") || strings.Contains(string(data), "Starlette") {
				return "python -m uvicorn main:app --host 0.0.0.0 --port " + p
			}
			return "python main.py"
		}
		if has("app.py") {
			return "python app.py"
		}
	case "go":
		entries, _ := filepath.Glob(filepath.Join(root, "cmd", "*", "main.go"))
		if !has("main.go") && len(entries) == 1 {
			return "devbox-go-start ./cmd/" + filepath.Base(filepath.Dir(entries[0]))
		}
		return "devbox-go-start ."
	case "static":
		return "nginx -g 'daemon off;'"
	}
	return ""
}

package containerspec

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Module struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type ModuleOption struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Versioned   bool   `json:"versioned"`
}

type DeploymentSpec struct {
	ProjectID      string
	Runtime        string
	Version        string
	ContextDir     string
	Dockerfile     string
	DockerfilePath string
	Image          string
	ContainerName  string
	HostPort       int
	ContainerPort  int
	Environment    map[string]string
	Labels         map[string]string
	Fingerprint    string
	ReadOnly       bool
}

type moduleDef struct {
	option       ModuleOption
	aptPackages  []string
	phpExtension string
	phpConfigure string
}

var safeVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var catalogs = map[string][]moduleDef{
	"php": {
		{option: ModuleOption{Name: "pdo", Label: "PDO", Description: "PHP Data Objects; dostępne w bazowym obrazie PHP."}},
		{option: ModuleOption{Name: "pdo_mysql", Label: "PDO MySQL", Description: "Sterownik PDO dla MySQL/MariaDB."}, phpExtension: "pdo_mysql"},
		{option: ModuleOption{Name: "mysqli", Label: "MySQLi", Description: "Rozszerzenie MySQL Improved."}, phpExtension: "mysqli"},
		{option: ModuleOption{Name: "mbstring", Label: "mbstring", Description: "Obsługa wielobajtowych ciągów znaków."}, aptPackages: []string{"libonig-dev"}, phpExtension: "mbstring"},
		{option: ModuleOption{Name: "intl", Label: "intl", Description: "Internationalization / ICU."}, aptPackages: []string{"libicu-dev"}, phpExtension: "intl"},
		{option: ModuleOption{Name: "gd", Label: "GD", Description: "Przetwarzanie obrazów JPEG/PNG/FreeType."}, aptPackages: []string{"libpng-dev", "libjpeg62-turbo-dev", "libfreetype6-dev"}, phpExtension: "gd", phpConfigure: "docker-php-ext-configure gd --with-freetype --with-jpeg"},
		{option: ModuleOption{Name: "curl", Label: "cURL", Description: "Klient HTTP/libcurl."}, aptPackages: []string{"libcurl4-openssl-dev"}, phpExtension: "curl"},
		{option: ModuleOption{Name: "zip", Label: "ZIP", Description: "Obsługa archiwów ZIP."}, aptPackages: []string{"libzip-dev"}, phpExtension: "zip"},
		{option: ModuleOption{Name: "bcmath", Label: "BCMath", Description: "Arytmetyka dużej precyzji."}, phpExtension: "bcmath"},
		{option: ModuleOption{Name: "opcache", Label: "OPcache", Description: "Cache kodu bajtowego PHP."}, phpExtension: "opcache"},
		{option: ModuleOption{Name: "xml", Label: "XML", Description: "Obsługa XML."}, aptPackages: []string{"libxml2-dev"}, phpExtension: "xml"},
		{option: ModuleOption{Name: "soap", Label: "SOAP", Description: "Klient/serwer SOAP."}, aptPackages: []string{"libxml2-dev"}, phpExtension: "soap"},
		{option: ModuleOption{Name: "sockets", Label: "Sockets", Description: "Niskopoziomowe gniazda sieciowe."}, phpExtension: "sockets"},
		{option: ModuleOption{Name: "pcntl", Label: "PCNTL", Description: "Kontrola procesów."}, phpExtension: "pcntl"},
		{option: ModuleOption{Name: "exif", Label: "EXIF", Description: "Metadane EXIF obrazów."}, phpExtension: "exif"},
	},
	"node": {
		{option: ModuleOption{Name: "build-essential", Label: "Build tools", Description: "make/g++ dla natywnych modułów Node."}, aptPackages: []string{"build-essential"}},
		{option: ModuleOption{Name: "python3", Label: "Python 3", Description: "Python wymagany przez część node-gyp."}, aptPackages: []string{"python3"}},
		{option: ModuleOption{Name: "git", Label: "Git", Description: "Git dla zależności pobieranych z repozytoriów."}, aptPackages: []string{"git"}},
	},
	"python": {
		{option: ModuleOption{Name: "build-essential", Label: "Build tools", Description: "Kompilator i narzędzia dla pakietów z rozszerzeniami C."}, aptPackages: []string{"build-essential"}},
		{option: ModuleOption{Name: "libpq-dev", Label: "PostgreSQL headers", Description: "Nagłówki libpq dla sterowników PostgreSQL."}, aptPackages: []string{"libpq-dev"}},
		{option: ModuleOption{Name: "mysqlclient", Label: "MySQL client headers", Description: "Biblioteki potrzebne do budowania mysqlclient."}, aptPackages: []string{"default-libmysqlclient-dev", "pkg-config"}},
		{option: ModuleOption{Name: "libffi-dev", Label: "libffi", Description: "Nagłówki FFI."}, aptPackages: []string{"libffi-dev"}},
		{option: ModuleOption{Name: "libssl-dev", Label: "OpenSSL headers", Description: "Nagłówki OpenSSL."}, aptPackages: []string{"libssl-dev"}},
	},
	"go": {
		{option: ModuleOption{Name: "build-essential", Label: "Build tools", Description: "GCC/make dla projektów korzystających z CGO."}, aptPackages: []string{"build-essential"}},
		{option: ModuleOption{Name: "git", Label: "Git", Description: "Git dla modułów pobieranych z repozytoriów."}, aptPackages: []string{"git"}},
		{option: ModuleOption{Name: "ca-certificates", Label: "CA certificates", Description: "Dodatkowe certyfikaty CA w etapie budowania."}, aptPackages: []string{"ca-certificates"}},
	},
	"static": {},
}

func NormalizeRuntime(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "nodejs", "node.js":
		return "node"
	case "golang":
		return "go"
	default:
		return value
	}
}

func DefaultVersion(runtime string) string {
	switch NormalizeRuntime(runtime) {
	case "php":
		return "8.3"
	case "node":
		return "22"
	case "python":
		return "3.12"
	case "go":
		return "1.23"
	case "static":
		return "1.27"
	default:
		return ""
	}
}

func Catalog(runtime string) ([]ModuleOption, error) {
	runtime = NormalizeRuntime(runtime)
	defs, ok := catalogs[runtime]
	if !ok {
		return nil, fmt.Errorf("unsupported managed runtime %q", runtime)
	}
	out := make([]ModuleOption, 0, len(defs))
	for _, def := range defs {
		out = append(out, def.option)
	}
	return out, nil
}

func Validate(runtime, version string, modules []Module) error {
	runtime = NormalizeRuntime(runtime)
	defs, ok := catalogs[runtime]
	if !ok {
		return fmt.Errorf("unsupported managed runtime %q", runtime)
	}
	if version != "" && !safeVersion.MatchString(version) {
		return fmt.Errorf("invalid runtime version")
	}
	allowed := make(map[string]moduleDef, len(defs))
	for _, def := range defs {
		allowed[def.option.Name] = def
	}
	seen := map[string]struct{}{}
	for _, module := range modules {
		name := strings.ToLower(strings.TrimSpace(module.Name))
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("module %q is not allowed for runtime %s", module.Name, runtime)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("module %q is duplicated", name)
		}
		seen[name] = struct{}{}
		if strings.TrimSpace(module.Version) != "" {
			return fmt.Errorf("module %q does not support an independent version constraint", name)
		}
	}
	return nil
}

func GenerateManaged(projectID, workDir, runtime, version string, modules []Module, revision string, hostPort int) (DeploymentSpec, error) {
	runtime = NormalizeRuntime(runtime)
	if version == "" {
		version = DefaultVersion(runtime)
	}
	if err := Validate(runtime, version, modules); err != nil {
		return DeploymentSpec{}, err
	}
	if hostPort < 1 || hostPort > 65535 {
		return DeploymentSpec{}, fmt.Errorf("host port is out of range")
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return DeploymentSpec{}, fmt.Errorf("resolve build context: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return DeploymentSpec{}, fmt.Errorf("build context is not an existing directory")
	}
	digest, err := contextDigest(abs)
	if err != nil {
		return DeploymentSpec{}, err
	}
	sorted := append([]Module(nil), modules...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	h := sha256.New()
	_, _ = io.WriteString(h, runtime+"\n"+version+"\n"+revision+"\n"+digest+"\n")
	for _, module := range sorted {
		_, _ = io.WriteString(h, strings.ToLower(strings.TrimSpace(module.Name))+"="+strings.TrimSpace(module.Version)+"\n")
	}
	fingerprint := hex.EncodeToString(h.Sum(nil))
	short := sanitizeIdentifier(projectID)
	if len(short) > 24 {
		short = short[:24]
	}
	if short == "" {
		return DeploymentSpec{}, fmt.Errorf("invalid project id")
	}
	image := "devbox/runtime-" + short + ":" + fingerprint[:16]
	name := "devbox-app-" + short
	dockerfile, port, readOnly, env, err := dockerfileFor(runtime, version, sorted)
	if err != nil {
		return DeploymentSpec{}, err
	}
	return DeploymentSpec{
		ProjectID: projectID, Runtime: runtime, Version: version, ContextDir: abs, Dockerfile: dockerfile,
		Image: image, ContainerName: name, HostPort: hostPort, ContainerPort: port,
		Environment: env,
		Labels: map[string]string{
			"io.devbox.managed":     "true",
			"io.devbox.project":     projectID,
			"io.devbox.runtime":     runtime,
			"io.devbox.fingerprint": fingerprint,
		},
		Fingerprint: fingerprint, ReadOnly: readOnly,
	}, nil
}

func GenerateCustomDockerfile(projectID, workDir string, hostPort int) (DeploymentSpec, error) {
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return DeploymentSpec{}, err
	}
	dockerfile := filepath.Join(abs, "Dockerfile")
	if info, statErr := os.Stat(dockerfile); statErr != nil || !info.Mode().IsRegular() {
		return DeploymentSpec{}, fmt.Errorf("custom Dockerfile was not found")
	}
	if hostPort < 1 || hostPort > 65535 {
		return DeploymentSpec{}, fmt.Errorf("host port is out of range")
	}
	digest, err := contextDigest(abs)
	if err != nil {
		return DeploymentSpec{}, err
	}
	data, err := os.ReadFile(dockerfile)
	if err != nil {
		return DeploymentSpec{}, err
	}
	h := sha256.Sum256(append(data, []byte("\n"+digest)...))
	fingerprint := hex.EncodeToString(h[:])
	short := sanitizeIdentifier(projectID)
	if len(short) > 24 {
		short = short[:24]
	}
	port := dockerfileExposePort(string(data))
	if port == 0 {
		port = 8080
	}
	return DeploymentSpec{
		ProjectID: projectID, Runtime: "custom", ContextDir: abs, DockerfilePath: dockerfile,
		Image: "devbox/custom-" + short + ":" + fingerprint[:16], ContainerName: "devbox-app-" + short,
		HostPort: hostPort, ContainerPort: port,
		Labels: map[string]string{
			"io.devbox.managed":     "true",
			"io.devbox.project":     projectID,
			"io.devbox.runtime":     "custom",
			"io.devbox.fingerprint": fingerprint,
		},
		Fingerprint: fingerprint,
	}, nil
}

func dockerfileFor(runtime, version string, modules []Module) (string, int, bool, map[string]string, error) {
	defs := make(map[string]moduleDef)
	for _, def := range catalogs[runtime] {
		defs[def.option.Name] = def
	}
	aptSet := map[string]struct{}{}
	phpExt := make([]string, 0)
	configure := make([]string, 0)
	for _, module := range modules {
		def := defs[strings.ToLower(strings.TrimSpace(module.Name))]
		for _, pkg := range def.aptPackages {
			aptSet[pkg] = struct{}{}
		}
		if def.phpExtension != "" {
			phpExt = append(phpExt, def.phpExtension)
		}
		if def.phpConfigure != "" {
			configure = append(configure, def.phpConfigure)
		}
	}
	apt := sortedSet(aptSet)
	sort.Strings(phpExt)
	sort.Strings(configure)

	switch runtime {
	case "php":
		var run []string
		if len(apt) > 0 {
			run = append(run, "apt-get update && apt-get install -y --no-install-recommends "+strings.Join(apt, " "))
		}
		run = append(run, configure...)
		if len(phpExt) > 0 {
			run = append(run, "docker-php-ext-install -j$(nproc) "+strings.Join(phpExt, " "))
		}
		if len(apt) > 0 {
			run = append(run, "rm -rf /var/lib/apt/lists/*")
		}
		runLine := ""
		if len(run) > 0 {
			runLine = "RUN " + strings.Join(run, " && ") + "\n"
		}
		return "FROM composer:2 AS composer\nFROM php:" + version + "-cli-bookworm\n" +
				"COPY --from=composer /usr/bin/composer /usr/local/bin/composer\n" +
				runLine +
				"WORKDIR /app\nCOPY . /app\n" +
				"RUN if [ -f composer.json ]; then composer install --no-interaction --prefer-dist --optimize-autoloader; fi\n" +
				"RUN useradd -u 10001 -r -s /usr/sbin/nologin devbox && chown -R 10001:0 /app\n" +
				"USER 10001\nENV APP_PORT=8080\nEXPOSE 8080\n" +
				"CMD [\"sh\",\"-lc\",\"if [ -d public ]; then exec php -S 0.0.0.0:8080 -t public; else exec php -S 0.0.0.0:8080 -t .; fi\"]\n",
			8080, false, map[string]string{"APP_PORT": "8080"}, nil
	case "node":
		install := ""
		if len(apt) > 0 {
			install = "RUN apt-get update && apt-get install -y --no-install-recommends " + strings.Join(apt, " ") + " && rm -rf /var/lib/apt/lists/*\n"
		}
		return "FROM node:" + version + "-bookworm-slim\n" + install +
				"WORKDIR /app\nCOPY . /app\nRUN corepack enable && if [ -f pnpm-lock.yaml ]; then pnpm install --frozen-lockfile; elif [ -f yarn.lock ]; then yarn install --frozen-lockfile; elif [ -f package-lock.json ]; then npm ci; elif [ -f package.json ]; then npm install; fi\n" +
				"RUN if [ -f package.json ]; then npm run build --if-present; fi\n" +
				"USER node\nENV PORT=8080 HOST=0.0.0.0\nEXPOSE 8080\n" +
				"CMD [\"sh\",\"-lc\",\"if [ -f package.json ] && node -e 'const p=require(\\\"./package.json\\\");process.exit(p.scripts&&p.scripts.start?0:1)'; then exec npm start; elif [ -f server.js ]; then exec node server.js; elif [ -f index.js ]; then exec node index.js; else echo 'No Node start script/server.js/index.js found' >&2; exit 1; fi\"]\n",
			8080, false, map[string]string{"PORT": "8080", "HOST": "0.0.0.0"}, nil
	case "python":
		install := ""
		if len(apt) > 0 {
			install = "RUN apt-get update && apt-get install -y --no-install-recommends " + strings.Join(apt, " ") + " && rm -rf /var/lib/apt/lists/*\n"
		}
		return "FROM python:" + version + "-slim-bookworm\n" + install +
				"WORKDIR /app\nCOPY . /app\nRUN python -m pip install --no-cache-dir --upgrade pip && if [ -f requirements.txt ]; then pip install --no-cache-dir -r requirements.txt; elif [ -f pyproject.toml ]; then pip install --no-cache-dir .; fi\n" +
				"RUN useradd -u 10001 -r -s /usr/sbin/nologin devbox && chown -R 10001:0 /app\n" +
				"USER 10001\nENV PORT=8080 PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1\nEXPOSE 8080\n" +
				"CMD [\"sh\",\"-lc\",\"if [ -f manage.py ]; then exec python manage.py runserver 0.0.0.0:8080; elif [ -f main.py ] && python -c 'import uvicorn' >/dev/null 2>&1; then exec python -m uvicorn main:app --host 0.0.0.0 --port 8080; elif [ -f app.py ] && python -c 'import uvicorn' >/dev/null 2>&1; then exec python -m uvicorn app:app --host 0.0.0.0 --port 8080; elif [ -f app.py ]; then exec python app.py; elif [ -f main.py ]; then exec python main.py; else echo 'No Python entry point found' >&2; exit 1; fi\"]\n",
			8080, false, map[string]string{"PORT": "8080", "PYTHONDONTWRITEBYTECODE": "1", "PYTHONUNBUFFERED": "1"}, nil
	case "go":
		install := ""
		if len(apt) > 0 {
			install = "RUN apt-get update && apt-get install -y --no-install-recommends " + strings.Join(apt, " ") + " && rm -rf /var/lib/apt/lists/*\n"
		}
		return "FROM golang:" + version + "-bookworm AS build\n" + install +
				"WORKDIR /src\nCOPY . .\nRUN if [ -f go.mod ]; then go mod download; fi\nRUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/app .\n" +
				"FROM gcr.io/distroless/static-debian12:nonroot\nCOPY --from=build /out/app /app\nENV PORT=8080\nEXPOSE 8080\nENTRYPOINT [\"/app\"]\n",
			8080, true, map[string]string{"PORT": "8080"}, nil
	case "static":
		return "FROM nginxinc/nginx-unprivileged:" + version + "-alpine\nCOPY . /usr/share/nginx/html\nEXPOSE 8080\n", 8080, false, nil, nil
	default:
		return "", 0, false, nil, fmt.Errorf("unsupported managed runtime %q", runtime)
	}
}

func dockerfileExposePort(content string) int {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) < 7 || strings.ToUpper(line[:6]) != "EXPOSE" {
			continue
		}
		fields := strings.Fields(line[6:])
		for _, field := range fields {
			field = strings.SplitN(field, "/", 2)[0]
			if port, err := strconv.Atoi(field); err == nil && port > 0 && port <= 65535 {
				return port
			}
		}
	}
	return 0
}

func contextDigest(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if ignoredPath(parts) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		_, _ = io.WriteString(h, filepath.ToSlash(rel)+"\n")
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(h, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return "", fmt.Errorf("hash build context: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ignoredPath(parts []string) bool {
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts[:len(parts)-1] {
		switch part {
		case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".idea", ".vscode":
			return true
		}
	}
	base := parts[len(parts)-1]
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	switch base {
	case ".DS_Store", "devbox.db":
		return true
	}
	return false
}

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sanitizeIdentifier(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-_")
}

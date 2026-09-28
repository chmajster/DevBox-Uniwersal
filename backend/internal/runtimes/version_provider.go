package runtimes

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type AvailableVersion struct {
	RuntimeType        string `json:"runtime_type"`
	Version            string `json:"version"`
	Platform           string `json:"platform"`
	Architecture       string `json:"architecture"`
	InstallationMethod string `json:"installation_method"`
	Installable        bool   `json:"installable"`
	Source             string `json:"source"`
}

type DetectedRuntime struct {
	RuntimeType    string
	Version        string
	ExecutablePath string
	Source         string
	Tools          map[string]string
}

type ManagedRuntime struct {
	ExecutablePath   string
	InstallationRoot string
	Tools            map[string]string
}

type InstallReporter interface {
	Stage(message string)
	Log(message string)
}

type RuntimeVersionProvider interface {
	RuntimeType() string
	ListAvailableVersions(ctx context.Context) ([]AvailableVersion, error)
	DetectInstalled(ctx context.Context) ([]DetectedRuntime, error)
	Install(ctx context.Context, version, targetRoot string, reporter InstallReporter) (ManagedRuntime, error)
	ValidateInstallation(ctx context.Context, installation Installation) error
	Remove(ctx context.Context, installation Installation, reporter InstallReporter) error
}

type providerBase struct {
	runtimeType string
	root        string
	client      *http.Client
}

func NewRuntimeVersionProviders(root string, client *http.Client) (map[string]RuntimeVersionProvider, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("runtime root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime root: %w", err)
	}
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	base := func(runtimeType string) providerBase {
		return providerBase{runtimeType: runtimeType, root: absolute, client: client}
	}
	return map[string]RuntimeVersionProvider{
		"node":   &nodeVersionProvider{providerBase: base("node"), indexURL: "https://nodejs.org/dist/index.json", distURL: "https://nodejs.org/dist"},
		"go":     &goVersionProvider{providerBase: base("go"), indexURL: "https://go.dev/dl/?mode=json&include=all", distURL: "https://go.dev/dl"},
		"php":    &phpVersionProvider{providerBase: base("php"), releasesURL: "https://www.php.net/releases/index.php?json&version=8&max=100", distURL: "https://www.php.net/distributions"},
		"python": &pythonVersionProvider{providerBase: base("python"), indexURL: "https://www.python.org/ftp/python/", distURL: "https://www.python.org/ftp/python"},
	}, nil
}

func (b providerBase) finalRoot(version string) string {
	return filepath.Join(b.root, b.runtimeType, version)
}

func (b providerBase) validateRoot(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	allowed := filepath.Join(b.root, b.runtimeType)
	relative, err := filepath.Rel(allowed, absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("runtime path escapes managed runtime root")
	}
	return nil
}

func (b providerBase) remove(ctx context.Context, installation Installation, reporter InstallReporter) error {
	if !installation.ManagedByDevBox {
		return errors.New("system runtime cannot be removed by DevBox")
	}
	if err := b.validateRoot(installation.InstallationRoot); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	reporter.Stage("Removing runtime")
	path := installation.InstallationRoot
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	tmp := path + ".removing-" + installation.ID
	_ = os.RemoveAll(tmp)
	if err := os.Rename(path, tmp); err != nil {
		return fmt.Errorf("stage runtime removal: %w", err)
	}
	if err := os.RemoveAll(tmp); err != nil {
		_ = os.Rename(tmp, path)
		return fmt.Errorf("remove runtime: %w", err)
	}
	return nil
}

type nodeVersionProvider struct {
	providerBase
	indexURL string
	distURL  string
}

func (p *nodeVersionProvider) RuntimeType() string { return "node" }

func (p *nodeVersionProvider) ListAvailableVersions(ctx context.Context) ([]AvailableVersion, error) {
	var releases []struct {
		Version string   `json:"version"`
		Files   []string `json:"files"`
	}
	if err := getJSON(ctx, p.client, p.indexURL, &releases); err != nil {
		return nil, err
	}
	arch := nodeArch()
	fileMarker := "linux-" + arch
	items := []AvailableVersion{}
	for _, release := range releases {
		version := strings.TrimPrefix(strings.TrimSpace(release.Version), "v")
		if !validExactVersion(version) || !containsString(release.Files, fileMarker) {
			continue
		}
		items = append(items, AvailableVersion{RuntimeType: p.RuntimeType(), Version: version, Platform: "linux", Architecture: goruntime.GOARCH, InstallationMethod: "official-archive", Installable: goruntime.GOOS == "linux", Source: "nodejs.org"})
	}
	sortAvailable(items)
	return items, nil
}

func (p *nodeVersionProvider) DetectInstalled(ctx context.Context) ([]DetectedRuntime, error) {
	return detectLocalExecutables(ctx, "node", []string{"node"}, []string{"/usr/bin/node", "/usr/local/bin/node"}, []string{"--version"})
}

func (p *nodeVersionProvider) Install(ctx context.Context, version, targetRoot string, reporter InstallReporter) (ManagedRuntime, error) {
	if goruntime.GOOS != "linux" {
		return ManagedRuntime{}, errors.New("managed Node.js installation is supported inside Linux/WSL")
	}
	if !validExactVersion(version) || targetRoot != p.finalRoot(version) {
		return ManagedRuntime{}, errors.New("invalid Node.js installation target")
	}
	filename := fmt.Sprintf("node-v%s-linux-%s.tar.gz", version, nodeArch())
	baseURL := strings.TrimRight(p.distURL, "/") + "/v" + version
	reporter.Stage("Resolving checksum")
	checksums, err := getText(ctx, p.client, baseURL+"/SHASUMS256.txt", 4<<20)
	if err != nil {
		return ManagedRuntime{}, fmt.Errorf("load Node.js checksums: %w", err)
	}
	expected := checksumForFile(checksums, filename)
	if expected == "" {
		return ManagedRuntime{}, fmt.Errorf("official Node.js checksum not found for %s", filename)
	}
	archivePath, cleanup, err := downloadTemp(ctx, p.client, baseURL+"/"+filename, expected, true, reporter)
	if err != nil {
		return ManagedRuntime{}, err
	}
	defer cleanup()
	reporter.Stage("Extracting runtime")
	if err := atomicExtractTarGz(archivePath, targetRoot, "node-v"+version+"-linux-"+nodeArch()); err != nil {
		return ManagedRuntime{}, err
	}
	result := ManagedRuntime{
		ExecutablePath: filepath.Join(targetRoot, "bin", "node"),
		InstallationRoot: targetRoot,
		Tools: map[string]string{
			"node": filepath.Join(targetRoot, "bin", "node"),
			"npm": filepath.Join(targetRoot, "bin", "npm"),
			"npx": filepath.Join(targetRoot, "bin", "npx"),
			"corepack": filepath.Join(targetRoot, "bin", "corepack"),
		},
	}
	reporter.Stage("Validating executable")
	if err := validateExecutableVersion(ctx, result.ExecutablePath, version, "--version"); err != nil {
		_ = os.RemoveAll(targetRoot)
		return ManagedRuntime{}, err
	}
	return result, nil
}

func (p *nodeVersionProvider) ValidateInstallation(ctx context.Context, installation Installation) error {
	return validateExecutableVersion(ctx, installation.ExecutablePath, installation.Version, "--version")
}

func (p *nodeVersionProvider) Remove(ctx context.Context, installation Installation, reporter InstallReporter) error {
	return p.remove(ctx, installation, reporter)
}

type goVersionProvider struct {
	providerBase
	indexURL string
	distURL  string
}

type goRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
	Files   []struct {
		Filename string `json:"filename"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Version  string `json:"version"`
		SHA256   string `json:"sha256"`
		Kind     string `json:"kind"`
	} `json:"files"`
}

func (p *goVersionProvider) RuntimeType() string { return "go" }

func (p *goVersionProvider) releases(ctx context.Context) ([]goRelease, error) {
	var releases []goRelease
	if err := getJSON(ctx, p.client, p.indexURL, &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func (p *goVersionProvider) ListAvailableVersions(ctx context.Context) ([]AvailableVersion, error) {
	releases, err := p.releases(ctx)
	if err != nil {
		return nil, err
	}
	items := []AvailableVersion{}
	for _, release := range releases {
		version := strings.TrimPrefix(release.Version, "go")
		for _, file := range release.Files {
			if file.OS == "linux" && file.Arch == goruntime.GOARCH && file.Kind == "archive" {
				items = append(items, AvailableVersion{RuntimeType: p.RuntimeType(), Version: version, Platform: "linux", Architecture: goruntime.GOARCH, InstallationMethod: "official-archive", Installable: goruntime.GOOS == "linux", Source: "go.dev"})
				break
			}
		}
	}
	sortAvailable(items)
	return items, nil
}

func (p *goVersionProvider) DetectInstalled(ctx context.Context) ([]DetectedRuntime, error) {
	return detectLocalExecutables(ctx, "go", []string{"go"}, []string{"/usr/bin/go", "/usr/local/bin/go", "/usr/local/go/bin/go"}, []string{"version"})
}

func (p *goVersionProvider) Install(ctx context.Context, version, targetRoot string, reporter InstallReporter) (ManagedRuntime, error) {
	if goruntime.GOOS != "linux" {
		return ManagedRuntime{}, errors.New("managed Go installation is supported inside Linux/WSL")
	}
	if !validExactVersion(version) || targetRoot != p.finalRoot(version) {
		return ManagedRuntime{}, errors.New("invalid Go installation target")
	}
	releases, err := p.releases(ctx)
	if err != nil {
		return ManagedRuntime{}, err
	}
	var filename, checksum string
	for _, release := range releases {
		if strings.TrimPrefix(release.Version, "go") != version {
			continue
		}
		for _, file := range release.Files {
			if file.OS == "linux" && file.Arch == goruntime.GOARCH && file.Kind == "archive" {
				filename, checksum = file.Filename, file.SHA256
				break
			}
		}
	}
	if filename == "" || checksum == "" {
		return ManagedRuntime{}, fmt.Errorf("official Go archive for %s/%s was not found", goruntime.GOOS, goruntime.GOARCH)
	}
	reporter.Stage("Downloading runtime")
	archivePath, cleanup, err := downloadTemp(ctx, p.client, strings.TrimRight(p.distURL, "/")+"/"+filename, checksum, true, reporter)
	if err != nil {
		return ManagedRuntime{}, err
	}
	defer cleanup()
	reporter.Stage("Extracting runtime")
	if err := atomicExtractTarGz(archivePath, targetRoot, "go"); err != nil {
		return ManagedRuntime{}, err
	}
	result := ManagedRuntime{ExecutablePath: filepath.Join(targetRoot, "bin", "go"), InstallationRoot: targetRoot, Tools: map[string]string{"go": filepath.Join(targetRoot, "bin", "go"), "gofmt": filepath.Join(targetRoot, "bin", "gofmt")}}
	reporter.Stage("Validating executable")
	if err := validateExecutableVersion(ctx, result.ExecutablePath, version, "version"); err != nil {
		_ = os.RemoveAll(targetRoot)
		return ManagedRuntime{}, err
	}
	return result, nil
}

func (p *goVersionProvider) ValidateInstallation(ctx context.Context, installation Installation) error {
	return validateExecutableVersion(ctx, installation.ExecutablePath, installation.Version, "version")
}

func (p *goVersionProvider) Remove(ctx context.Context, installation Installation, reporter InstallReporter) error {
	return p.remove(ctx, installation, reporter)
}

type phpVersionProvider struct {
	providerBase
	releasesURL string
	distURL     string
}

type phpRelease struct {
	Version string `json:"version"`
	Source  []struct {
		Filename string `json:"filename"`
		Name     string `json:"name"`
		SHA256   string `json:"sha256"`
	} `json:"source"`
}

func (p *phpVersionProvider) RuntimeType() string { return "php" }

func (p *phpVersionProvider) releases(ctx context.Context) (map[string]phpRelease, error) {
	var releases map[string]phpRelease
	if err := getJSON(ctx, p.client, p.releasesURL, &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func (p *phpVersionProvider) ListAvailableVersions(ctx context.Context) ([]AvailableVersion, error) {
	releases, err := p.releases(ctx)
	if err != nil {
		return nil, err
	}
	items := []AvailableVersion{}
	for key, release := range releases {
		version := release.Version
		if version == "" {
			version = key
		}
		if !validExactVersion(version) {
			continue
		}
		items = append(items, AvailableVersion{RuntimeType: p.RuntimeType(), Version: version, Platform: "linux", Architecture: goruntime.GOARCH, InstallationMethod: "official-source-build", Installable: goruntime.GOOS == "linux", Source: "php.net"})
	}
	sortAvailable(items)
	return items, nil
}

func (p *phpVersionProvider) DetectInstalled(ctx context.Context) ([]DetectedRuntime, error) {
	glob, _ := filepath.Glob("/usr/bin/php[0-9]*")
	candidates := append([]string{"/usr/bin/php", "/usr/local/bin/php"}, glob...)
	return detectLocalExecutables(ctx, "php", []string{"php"}, candidates, []string{"-r", "echo PHP_VERSION;"})
}

func (p *phpVersionProvider) Install(ctx context.Context, version, targetRoot string, reporter InstallReporter) (ManagedRuntime, error) {
	if goruntime.GOOS != "linux" {
		return ManagedRuntime{}, errors.New("managed PHP installation is supported inside Linux/WSL")
	}
	if !validExactVersion(version) || targetRoot != p.finalRoot(version) {
		return ManagedRuntime{}, errors.New("invalid PHP installation target")
	}
	releases, err := p.releases(ctx)
	if err != nil {
		return ManagedRuntime{}, err
	}
	release, ok := releases[version]
	if !ok {
		for key, candidate := range releases {
			if candidate.Version == version || key == version {
				release, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return ManagedRuntime{}, fmt.Errorf("PHP version %s is not available from php.net", version)
	}
	var filename, checksum string
	for _, source := range release.Source {
		name := source.Filename
		if name == "" {
			name = source.Name
		}
		if strings.HasSuffix(name, ".tar.gz") && source.SHA256 != "" {
			filename, checksum = name, source.SHA256
			break
		}
	}
	if filename == "" {
		return ManagedRuntime{}, errors.New("php.net did not publish a SHA-256 verified tar.gz source archive")
	}
	archivePath, cleanup, err := downloadTemp(ctx, p.client, strings.TrimRight(p.distURL, "/")+"/"+filename, checksum, true, reporter)
	if err != nil {
		return ManagedRuntime{}, err
	}
	defer cleanup()
	return buildSourceRuntime(ctx, sourceBuildSpec{
		RuntimeType: "php", Version: version, ArchivePath: archivePath, SourcePrefix: "php-" + version, TargetRoot: targetRoot,
		ConfigureArgs: []string{"--enable-cli", "--enable-fpm", "--disable-cgi", "--without-pear"},
		PrimaryRelative: filepath.Join("bin", "php"),
		Tools: map[string]string{"php": filepath.Join("bin", "php"), "php-fpm": filepath.Join("sbin", "php-fpm")},
	}, reporter)
}

func (p *phpVersionProvider) ValidateInstallation(ctx context.Context, installation Installation) error {
	return validateExecutableVersion(ctx, installation.ExecutablePath, installation.Version, "-r", "echo PHP_VERSION;")
}

func (p *phpVersionProvider) Remove(ctx context.Context, installation Installation, reporter InstallReporter) error {
	return p.remove(ctx, installation, reporter)
}

type pythonVersionProvider struct {
	providerBase
	indexURL string
	distURL  string
}

func (p *pythonVersionProvider) RuntimeType() string { return "python" }

func (p *pythonVersionProvider) ListAvailableVersions(ctx context.Context) ([]AvailableVersion, error) {
	body, err := getText(ctx, p.client, p.indexURL, 8<<20)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	items := []AvailableVersion{}
	for _, token := range strings.FieldsFunc(body, func(r rune) bool {
		return !(r >= '0' && r <= '9') && r != '.'
	}) {
		token = strings.Trim(token, ".")
		if !strings.HasPrefix(token, "3.") || !validExactVersion(token) {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		items = append(items, AvailableVersion{RuntimeType: p.RuntimeType(), Version: token, Platform: "linux", Architecture: goruntime.GOARCH, InstallationMethod: "official-source-build", Installable: goruntime.GOOS == "linux", Source: "python.org"})
	}
	sortAvailable(items)
	return items, nil
}

func (p *pythonVersionProvider) DetectInstalled(ctx context.Context) ([]DetectedRuntime, error) {
	glob, _ := filepath.Glob("/usr/bin/python3.*")
	candidates := append([]string{"/usr/bin/python3", "/usr/local/bin/python3"}, glob...)
	return detectLocalExecutables(ctx, "python", []string{"python3", "python"}, candidates, []string{"--version"})
}

func (p *pythonVersionProvider) Install(ctx context.Context, version, targetRoot string, reporter InstallReporter) (ManagedRuntime, error) {
	if goruntime.GOOS != "linux" {
		return ManagedRuntime{}, errors.New("managed Python installation is supported inside Linux/WSL")
	}
	if !validExactVersion(version) || targetRoot != p.finalRoot(version) {
		return ManagedRuntime{}, errors.New("invalid Python installation target")
	}
	filename := "Python-" + version + ".tgz"
	sourceURL := strings.TrimRight(p.distURL, "/") + "/" + version + "/" + filename
	reporter.Log("Python source archives are fetched from python.org over verified HTTPS; binary archive installation is not used")
	archivePath, cleanup, err := downloadTemp(ctx, p.client, sourceURL, "", false, reporter)
	if err != nil {
		return ManagedRuntime{}, err
	}
	defer cleanup()
	result, err := buildSourceRuntime(ctx, sourceBuildSpec{
		RuntimeType: "python", Version: version, ArchivePath: archivePath, SourcePrefix: "Python-" + version, TargetRoot: targetRoot,
		ConfigureArgs: []string{"--with-ensurepip=install"},
		PrimaryRelative: filepath.Join("bin", "python3"),
		Tools: map[string]string{"python": filepath.Join("bin", "python3"), "python3": filepath.Join("bin", "python3"), "pip": filepath.Join("bin", "pip3"), "pip3": filepath.Join("bin", "pip3")},
	}, reporter)
	if err != nil {
		return ManagedRuntime{}, err
	}
	return result, nil
}

func (p *pythonVersionProvider) ValidateInstallation(ctx context.Context, installation Installation) error {
	return validateExecutableVersion(ctx, installation.ExecutablePath, installation.Version, "--version")
}

func (p *pythonVersionProvider) Remove(ctx context.Context, installation Installation, reporter InstallReporter) error {
	return p.remove(ctx, installation, reporter)
}

type sourceBuildSpec struct {
	RuntimeType      string
	Version          string
	ArchivePath      string
	SourcePrefix     string
	TargetRoot       string
	ConfigureArgs    []string
	PrimaryRelative string
	Tools            map[string]string
}

func buildSourceRuntime(ctx context.Context, spec sourceBuildSpec, reporter InstallReporter) (ManagedRuntime, error) {
	parent := filepath.Dir(spec.TargetRoot)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return ManagedRuntime{}, err
	}
	if existing, err := os.Stat(spec.TargetRoot); err == nil && existing.IsDir() {
		primary := filepath.Join(spec.TargetRoot, spec.PrimaryRelative)
		if err := validateExecutableVersion(ctx, primary, spec.Version, versionArgs(spec.RuntimeType)...); err == nil {
			return managedFromSpec(spec), nil
		}
		return ManagedRuntime{}, fmt.Errorf("managed runtime target already exists but is invalid: %s", spec.TargetRoot)
	}
	work, err := os.MkdirTemp(parent, "."+spec.RuntimeType+"-"+spec.Version+"-build-*")
	if err != nil {
		return ManagedRuntime{}, err
	}
	defer os.RemoveAll(work)
	sourceRoot := filepath.Join(work, "source")
	reporter.Stage("Extracting source")
	if err := extractTarGz(spec.ArchivePath, sourceRoot); err != nil {
		return ManagedRuntime{}, err
	}
	sourceDir := filepath.Join(sourceRoot, spec.SourcePrefix)
	if info, err := os.Stat(sourceDir); err != nil || !info.IsDir() {
		return ManagedRuntime{}, errors.New("source archive does not contain the expected top-level directory")
	}
	absoluteTarget, err := filepath.Abs(spec.TargetRoot)
	if err != nil {
		return ManagedRuntime{}, err
	}
	configureArgs := append([]string{"--prefix=" + absoluteTarget}, spec.ConfigureArgs...)
	reporter.Stage("Configuring source")
	if err := runBuildStep(ctx, reporter, sourceDir, "./configure", configureArgs...); err != nil {
		return ManagedRuntime{}, err
	}
	jobs := goruntime.NumCPU()
	if jobs < 1 {
		jobs = 1
	}
	if jobs > 8 {
		jobs = 8
	}
	reporter.Stage("Compiling runtime")
	if err := runBuildStep(ctx, reporter, sourceDir, "make", "-j"+strconv.Itoa(jobs)); err != nil {
		return ManagedRuntime{}, err
	}
	dest := filepath.Join(work, "dest")
	reporter.Stage("Installing into staging directory")
	if err := runBuildStep(ctx, reporter, sourceDir, "make", "DESTDIR="+dest, "install"); err != nil {
		return ManagedRuntime{}, err
	}
	staged := filepath.Join(dest, strings.TrimPrefix(filepath.Clean(absoluteTarget), string(filepath.Separator)))
	if _, err := os.Stat(staged); err != nil {
		return ManagedRuntime{}, fmt.Errorf("compiled runtime staging root is missing: %w", err)
	}
	reporter.Stage("Activating runtime atomically")
	if err := os.Rename(staged, spec.TargetRoot); err != nil {
		return ManagedRuntime{}, fmt.Errorf("activate runtime: %w", err)
	}
	result := managedFromSpec(spec)
	reporter.Stage("Validating executable")
	if err := validateExecutableVersion(ctx, result.ExecutablePath, spec.Version, versionArgs(spec.RuntimeType)...); err != nil {
		_ = os.RemoveAll(spec.TargetRoot)
		return ManagedRuntime{}, err
	}
	return result, nil
}

func managedFromSpec(spec sourceBuildSpec) ManagedRuntime {
	tools := map[string]string{}
	for key, relative := range spec.Tools {
		tools[key] = filepath.Join(spec.TargetRoot, relative)
	}
	return ManagedRuntime{ExecutablePath: filepath.Join(spec.TargetRoot, spec.PrimaryRelative), InstallationRoot: spec.TargetRoot, Tools: tools}
}

func versionArgs(runtimeType string) []string {
	switch runtimeType {
	case "php":
		return []string{"-r", "echo PHP_VERSION;"}
	case "go":
		return []string{"version"}
	default:
		return []string{"--version"}
	}
}

func runBuildStep(ctx context.Context, reporter InstallReporter, workDir, command string, args ...string) error {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if len(text) > 32*1024 {
		text = text[len(text)-32*1024:]
	}
	if text != "" {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				reporter.Log(line)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("%s failed: %w", filepath.Base(command), err)
	}
	return nil
}

func detectLocalExecutables(ctx context.Context, runtimeType string, pathNames, explicitPaths, versionArguments []string) ([]DetectedRuntime, error) {
	candidates := append([]string{}, explicitPaths...)
	for _, name := range pathNames {
		if path, err := exec.LookPath(name); err == nil {
			candidates = append(candidates, path)
		}
	}
	seen := map[string]struct{}{}
	items := []DetectedRuntime{}
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			continue
		}
		if _, exists := seen[resolved]; exists {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		version, err := commandVersion(ctx, resolved, versionArguments...)
		if err != nil {
			continue
		}
		seen[resolved] = struct{}{}
		items = append(items, DetectedRuntime{RuntimeType: runtimeType, Version: version, ExecutablePath: resolved, Source: "system", Tools: map[string]string{runtimeType: resolved}})
	}
	return items, nil
}

func commandVersion(ctx context.Context, executable string, args ...string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(callCtx, executable, args...).CombinedOutput()
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(output))
	if strings.HasPrefix(text, "go version go") {
		text = strings.TrimPrefix(strings.Fields(text)[2], "go")
	} else {
		fields := strings.Fields(text)
		for _, field := range fields {
			candidate := strings.TrimLeft(field, "vV")
			candidate = strings.Trim(candidate, ",;()")
			if validExactVersion(candidate) {
				return candidate, nil
			}
		}
	}
	if validExactVersion(text) {
		return text, nil
	}
	return "", fmt.Errorf("could not parse runtime version from %q", text)
}

func validateExecutableVersion(ctx context.Context, executable, expected string, args ...string) error {
	if !filepath.IsAbs(executable) {
		return errors.New("runtime executable path is not absolute")
	}
	info, err := os.Stat(executable)
	if err != nil {
		return fmt.Errorf("runtime executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return errors.New("runtime executable is not an executable regular file")
	}
	version, err := commandVersion(ctx, executable, args...)
	if err != nil {
		return err
	}
	if version != expected {
		return fmt.Errorf("runtime executable version is %s, expected %s", version, expected)
	}
	return nil
}

func getJSON(ctx context.Context, client *http.Client, rawURL string, target any) error {
	body, err := getBytes(ctx, client, rawURL, 32<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}

func getText(ctx context.Context, client *http.Client, rawURL string, max int64) (string, error) {
	body, err := getBytes(ctx, client, rawURL, max)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func getBytes(ctx context.Context, client *http.Client, rawURL string, max int64) ([]byte, error) {
	if err := validateDownloadURL(rawURL); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s returned %s", rawURL, response.Status)
	}
	limited := io.LimitReader(response.Body, max+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, errors.New("download exceeds size limit")
	}
	return body, nil
}

func validateDownloadURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return errors.New("invalid runtime download URL")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname()) {
		return nil
	}
	return errors.New("runtime downloads require HTTPS")
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func downloadTemp(ctx context.Context, client *http.Client, rawURL, expectedSHA string, requireChecksum bool, reporter InstallReporter) (string, func(), error) {
	if requireChecksum && len(expectedSHA) != 64 {
		return "", func() {}, errors.New("trusted SHA-256 checksum is required")
	}
	if err := validateDownloadURL(rawURL); err != nil {
		return "", func() {}, err
	}
	file, err := os.CreateTemp("", "devbox-runtime-download-*")
	if err != nil {
		return "", func() {}, err
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, err
	}
	reporter.Stage("Downloading runtime")
	response, err := client.Do(request)
	if err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("download returned %s", response.Status)
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, 768<<20))
	closeErr := file.Close()
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	if closeErr != nil {
		cleanup()
		return "", func() {}, closeErr
	}
	if written >= 768<<20 {
		cleanup()
		return "", func() {}, errors.New("runtime archive exceeds size limit")
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if expectedSHA != "" && !strings.EqualFold(actual, expectedSHA) {
		cleanup()
		return "", func() {}, fmt.Errorf("runtime archive checksum mismatch: got %s", actual)
	}
	reporter.Log("SHA-256: " + actual)
	return path, cleanup, nil
}

func atomicExtractTarGz(archivePath, targetRoot, expectedPrefix string) error {
	parent := filepath.Dir(targetRoot)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(targetRoot); err == nil {
		return fmt.Errorf("runtime target already exists: %s", targetRoot)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := os.MkdirTemp(parent, "."+filepath.Base(targetRoot)+"-extract-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	if err := extractTarGz(archivePath, temp); err != nil {
		return err
	}
	source := filepath.Join(temp, expectedPrefix)
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return errors.New("runtime archive has unexpected root")
	}
	if err := os.Rename(source, targetRoot); err != nil {
		return fmt.Errorf("activate runtime: %w", err)
	}
	return nil
}

func extractTarGz(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return errors.New("runtime archive contains unsafe path")
		}
		target := filepath.Join(destination, name)
		relative, err := filepath.Rel(destination, target)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("runtime archive path traversal detected")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > 512<<20 {
				return errors.New("runtime archive entry exceeds size limit")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode) & 0o777
			mode &^= 0o6000
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(out, reader, header.Size)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(header.Linkname) {
				return errors.New("runtime archive contains absolute symlink")
			}
			linkTarget := filepath.Clean(filepath.Join(filepath.Dir(target), filepath.FromSlash(header.Linkname)))
			relative, err := filepath.Rel(destination, linkTarget)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return errors.New("runtime archive symlink escapes extraction root")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(filepath.FromSlash(header.Linkname), target); err != nil {
				return err
			}
		case tar.TypeLink:
			linkName := filepath.Clean(filepath.FromSlash(header.Linkname))
			if filepath.IsAbs(linkName) || linkName == ".." || strings.HasPrefix(linkName, ".."+string(filepath.Separator)) {
				return errors.New("runtime archive contains unsafe hard link")
			}
			linkTarget := filepath.Join(destination, linkName)
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Link(linkTarget, target); err != nil {
				return err
			}
		default:
			continue
		}
	}
	return nil
}

func checksumForFile(body, filename string) string {
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[1] == filename && len(fields[0]) == 64 {
			return fields[0]
		}
	}
	return ""
}

func nodeArch() string {
	switch goruntime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	default:
		return goruntime.GOARCH
	}
}

func validExactVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}

func parseVersion(value string) [3]int {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	for index := 0; index < len(parts) && index < 3; index++ {
		out[index], _ = strconv.Atoi(parts[index])
	}
	return out
}

func compareVersions(left, right string) int {
	l := parseVersion(left)
	r := parseVersion(right)
	for index := range l {
		if l[index] < r[index] {
			return -1
		}
		if l[index] > r[index] {
			return 1
		}
	}
	return 0
}

func sortAvailable(items []AvailableVersion) {
	sort.Slice(items, func(i, j int) bool {
		return compareVersions(items[i].Version, items[j].Version) > 0
	})
}

func containsString(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}

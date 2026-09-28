package runtimes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
)

const (
	JobRuntimeInstall = "runtime.install"
	JobRuntimeRemove  = "runtime.remove"
)

type RuntimeTypeView struct {
	RuntimeType   string             `json:"runtime_type"`
	Installations []Installation     `json:"installations"`
	Available     []AvailableVersion `json:"available,omitempty"`
	Default       *RuntimeDefault    `json:"default,omitempty"`
}

type ProjectRuntimeTypeView struct {
	RuntimeType          string                    `json:"runtime_type"`
	Assignment           *ProjectRuntimeAssignment `json:"assignment,omitempty"`
	Requirement          string                    `json:"requirement,omitempty"`
	RequirementSatisfied *bool                     `json:"requirement_satisfied,omitempty"`
	CompatibleInstalled  []Installation            `json:"compatible_installed"`
}

type VersionService struct {
	repo        *VersionRepository
	providers   map[string]RuntimeVersionProvider
	jobs        jobs.JobRunner
	root        string
	registry    Registry
	resolver    ProjectResolver
	cacheTTL    time.Duration
	cacheMu     sync.Mutex
	available   map[string]cachedVersions
	discoveryMu sync.Mutex
}

type cachedVersions struct {
	Items     []AvailableVersion
	ExpiresAt time.Time
}

func NewVersionService(repo *VersionRepository, providers map[string]RuntimeVersionProvider, runner jobs.JobRunner, root string, registry Registry, resolver ProjectResolver) (*VersionService, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime root: %w", err)
	}
	service := &VersionService{
		repo: repo, providers: providers, jobs: runner, root: absolute, registry: registry, resolver: resolver,
		cacheTTL: 30 * time.Minute, available: map[string]cachedVersions{},
	}
	for _, runtimeType := range []string{"php", "node", "python", "go"} {
		if _, ok := providers[runtimeType]; !ok {
			return nil, fmt.Errorf("runtime version provider %s is not registered", runtimeType)
		}
	}
	return service, nil
}

func (s *VersionService) RefreshDiscovery(ctx context.Context) error {
	s.discoveryMu.Lock()
	defer s.discoveryMu.Unlock()
	for _, runtimeType := range []string{"php", "node", "python", "go"} {
		provider := s.providers[runtimeType]
		detected, err := provider.DetectInstalled(ctx)
		if err != nil {
			return fmt.Errorf("discover %s: %w", runtimeType, err)
		}
		for _, item := range detected {
			if item.Version == "" || item.ExecutablePath == "" {
				continue
			}
			if _, err := s.repo.UpsertSystem(ctx, item.RuntimeType, item.Version, item.ExecutablePath, item.Source, item.Tools); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *VersionService) ListAll(ctx context.Context, includeAvailable bool) ([]RuntimeTypeView, error) {
	if err := s.RefreshDiscovery(ctx); err != nil {
		return nil, err
	}
	defaults, err := s.repo.Defaults(ctx)
	if err != nil {
		return nil, err
	}
	defaultByType := map[string]RuntimeDefault{}
	for _, item := range defaults {
		defaultByType[item.RuntimeType] = item
	}
	result := make([]RuntimeTypeView, 0, 4)
	for _, runtimeType := range []string{"php", "node", "python", "go"} {
		installations, err := s.repo.ListInstallations(ctx, runtimeType)
		if err != nil {
			return nil, err
		}
		view := RuntimeTypeView{RuntimeType: runtimeType, Installations: installations}
		if item, ok := defaultByType[runtimeType]; ok {
			copy := item
			view.Default = &copy
		}
		if includeAvailable {
			available, err := s.Available(ctx, runtimeType, false)
			if err != nil {
				return nil, err
			}
			view.Available = available
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *VersionService) Runtime(ctx context.Context, runtimeType string, includeAvailable bool) (RuntimeTypeView, error) {
	runtimeType = normalizeRuntimeType(runtimeType)
	if !validRuntimeType(runtimeType) {
		return RuntimeTypeView{}, ErrInvalidRuntime
	}
	if err := s.RefreshDiscovery(ctx); err != nil {
		return RuntimeTypeView{}, err
	}
	items, err := s.repo.ListInstallations(ctx, runtimeType)
	if err != nil {
		return RuntimeTypeView{}, err
	}
	view := RuntimeTypeView{RuntimeType: runtimeType, Installations: items}
	defaults, err := s.repo.Defaults(ctx)
	if err != nil {
		return RuntimeTypeView{}, err
	}
	for _, item := range defaults {
		if item.RuntimeType == runtimeType {
			copy := item
			view.Default = &copy
			break
		}
	}
	if includeAvailable {
		view.Available, err = s.Available(ctx, runtimeType, false)
	}
	return view, err
}

func (s *VersionService) Available(ctx context.Context, runtimeType string, refresh bool) ([]AvailableVersion, error) {
	runtimeType = normalizeRuntimeType(runtimeType)
	provider, ok := s.providers[runtimeType]
	if !ok {
		return nil, ErrInvalidRuntime
	}
	s.cacheMu.Lock()
	if !refresh {
		if cached, ok := s.available[runtimeType]; ok && time.Now().Before(cached.ExpiresAt) {
			items := append([]AvailableVersion(nil), cached.Items...)
			s.cacheMu.Unlock()
			return items, nil
		}
	}
	s.cacheMu.Unlock()
	items, err := provider.ListAvailableVersions(ctx)
	if err != nil {
		return nil, err
	}
	s.cacheMu.Lock()
	s.available[runtimeType] = cachedVersions{Items: append([]AvailableVersion(nil), items...), ExpiresAt: time.Now().Add(s.cacheTTL)}
	s.cacheMu.Unlock()
	return items, nil
}

func (s *VersionService) EnqueueInstall(ctx context.Context, runtimeType, requested string, actor *string) (Installation, domain.Job, error) {
	runtimeType = normalizeRuntimeType(runtimeType)
	if !validRuntimeType(runtimeType) {
		return Installation{}, domain.Job{}, ErrInvalidRuntime
	}
	requested = normalizeRequestedVersion(requested)
	if requested == "" {
		return Installation{}, domain.Job{}, fmt.Errorf("%w: version is required", ErrInvalidRuntime)
	}
	available, err := s.Available(ctx, runtimeType, false)
	if err != nil {
		return Installation{}, domain.Job{}, err
	}
	resolved, method, err := resolveAvailableVersion(requested, available)
	if err != nil {
		return Installation{}, domain.Job{}, err
	}
	installation, err := s.repo.PrepareManagedInstall(ctx, runtimeType, requested, resolved, method)
	if err != nil {
		return Installation{}, domain.Job{}, err
	}
	job, err := s.jobs.Enqueue(ctx, jobs.Request{Type: JobRuntimeInstall, RequestedBy: actor, Payload: map[string]any{
		"installation_id": installation.ID,
		"runtime_type": runtimeType,
		"version": resolved,
	}})
	if err != nil {
		_ = s.repo.MarkFailed(context.Background(), installation.ID, err.Error())
		return Installation{}, domain.Job{}, err
	}
	return installation, job, nil
}

func (s *VersionService) EnqueueRemove(ctx context.Context, installationID string, actor *string) (domain.Job, error) {
	installation, err := s.repo.GetInstallation(ctx, installationID)
	if err != nil {
		return domain.Job{}, err
	}
	if !installation.ManagedByDevBox {
		return domain.Job{}, fmt.Errorf("%w: system runtimes are never removed by DevBox", ErrRuntimeConflict)
	}
	usage, err := s.repo.UsageProjects(ctx, installationID)
	if err != nil {
		return domain.Job{}, err
	}
	if len(usage) > 0 {
		names := make([]string, 0, len(usage))
		for _, project := range usage {
			names = append(names, project.Name)
		}
		return domain.Job{}, fmt.Errorf("%w: used by projects: %s", ErrRuntimeInUse, strings.Join(names, ", "))
	}
	if err := s.repo.MarkRemoving(ctx, installationID); err != nil {
		return domain.Job{}, err
	}
	job, err := s.jobs.Enqueue(ctx, jobs.Request{Type: JobRuntimeRemove, RequestedBy: actor, Payload: map[string]any{
		"installation_id": installation.ID,
		"runtime_type": installation.RuntimeType,
		"version": installation.Version,
	}})
	if err != nil {
		_ = s.repo.MarkValidation(context.Background(), installation.ID, InstallationInstalled, "")
		return domain.Job{}, err
	}
	return job, nil
}

func (s *VersionService) Validate(ctx context.Context, installationID string) (Installation, error) {
	installation, err := s.repo.GetInstallation(ctx, installationID)
	if err != nil {
		return Installation{}, err
	}
	provider, ok := s.providers[installation.RuntimeType]
	if !ok {
		return Installation{}, ErrInvalidRuntime
	}
	if err := provider.ValidateInstallation(ctx, installation); err != nil {
		_ = s.repo.MarkValidation(context.Background(), installation.ID, InstallationBroken, err.Error())
		return s.repo.GetInstallation(ctx, installation.ID)
	}
	if err := s.repo.MarkValidation(ctx, installation.ID, InstallationInstalled, ""); err != nil {
		return Installation{}, err
	}
	return s.repo.GetInstallation(ctx, installation.ID)
}

func (s *VersionService) Defaults(ctx context.Context) ([]RuntimeDefault, error) {
	return s.repo.Defaults(ctx)
}

func (s *VersionService) SetDefault(ctx context.Context, runtimeType, installationID string, actor *string) (RuntimeDefault, error) {
	runtimeType = normalizeRuntimeType(runtimeType)
	if !validRuntimeType(runtimeType) {
		return RuntimeDefault{}, ErrInvalidRuntime
	}
	installation, err := s.repo.GetInstallation(ctx, installationID)
	if err != nil {
		return RuntimeDefault{}, err
	}
	if err := s.repo.SetDefault(ctx, runtimeType, installationID, installation.Version, actor); err != nil {
		return RuntimeDefault{}, err
	}
	defaults, err := s.repo.Defaults(ctx)
	if err != nil {
		return RuntimeDefault{}, err
	}
	for _, item := range defaults {
		if item.RuntimeType == runtimeType {
			return item, nil
		}
	}
	return RuntimeDefault{}, errors.New("runtime default was not persisted")
}

func (s *VersionService) ApplyDefaults(ctx context.Context, projectID string) error {
	defaults, err := s.repo.Defaults(ctx)
	if err != nil {
		return err
	}
	for _, item := range defaults {
		if _, err := s.repo.SetAssignment(ctx, projectID, item.RuntimeType, item.InstallationID, item.RequestedVersion); err != nil {
			return err
		}
	}
	return nil
}

func (s *VersionService) ProjectRuntimes(ctx context.Context, projectID string) ([]ProjectRuntimeTypeView, error) {
	resolved, err := s.resolver.Resolve(ctx, projectID)
	if err != nil {
		return nil, err
	}
	assignments, err := s.repo.Assignments(ctx, projectID)
	if err != nil {
		return nil, err
	}
	assignmentByType := map[string]ProjectRuntimeAssignment{}
	for _, item := range assignments {
		assignmentByType[item.RuntimeType] = item
	}
	installations, err := s.repo.ListInstallations(ctx, "")
	if err != nil {
		return nil, err
	}
	installedByType := map[string][]Installation{}
	for _, item := range installations {
		if item.Status == InstallationInstalled {
			installedByType[item.RuntimeType] = append(installedByType[item.RuntimeType], item)
		}
	}
	result := make([]ProjectRuntimeTypeView, 0, 4)
	for _, runtimeType := range []string{"php", "node", "python", "go"} {
		view := ProjectRuntimeTypeView{RuntimeType: runtimeType}
		if assignment, ok := assignmentByType[runtimeType]; ok {
			copy := assignment
			view.Assignment = &copy
		}
		requirement := s.detectRequirement(ctx, runtimeType, resolved.Context)
		view.Requirement = requirement
		for _, candidate := range installedByType[runtimeType] {
			if requirement == "" || versionSatisfiesRequirement(candidate.Version, requirement) {
				view.CompatibleInstalled = append(view.CompatibleInstalled, candidate)
			}
		}
		if view.Assignment != nil && requirement != "" {
			ok := versionSatisfiesRequirement(view.Assignment.ResolvedVersion, requirement)
			view.RequirementSatisfied = &ok
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *VersionService) SetProjectRuntime(ctx context.Context, projectID, runtimeType, installationID string) (*ProjectRuntimeAssignment, error) {
	runtimeType = normalizeRuntimeType(runtimeType)
	if !validRuntimeType(runtimeType) {
		return nil, ErrInvalidRuntime
	}
	if strings.TrimSpace(installationID) == "" {
		if err := s.repo.ClearAssignment(ctx, projectID, runtimeType); err != nil {
			return nil, err
		}
		return nil, nil
	}
	installation, err := s.Validate(ctx, installationID)
	if err != nil {
		return nil, err
	}
	if installation.Status != InstallationInstalled {
		return nil, fmt.Errorf("%w: selected runtime failed validation", ErrInvalidRuntime)
	}
	assignment, err := s.repo.SetAssignment(ctx, projectID, runtimeType, installationID, installation.Version)
	if err != nil {
		return nil, err
	}
	return &assignment, nil
}

func (s *VersionService) ResolveExecution(ctx context.Context, projectID, runtimeType string) (ExecutionSelection, error) {
	return s.repo.Execution(ctx, projectID, normalizeRuntimeType(runtimeType))
}

func (s *VersionService) RecreatePythonVenv(ctx context.Context, projectID string) error {
	resolved, err := s.resolver.Resolve(ctx, projectID)
	if err != nil {
		return err
	}
	base, err := filepath.Abs(resolved.Context.WorkDir)
	if err != nil {
		return err
	}
	venv := filepath.Join(base, ".venv")
	relative, err := filepath.Rel(base, venv)
	if err != nil || relative != ".venv" {
		return errors.New("invalid project virtualenv path")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.RemoveAll(venv)
}

func (s *VersionService) detectRequirement(ctx context.Context, runtimeType string, project ProjectContext) string {
	if s.registry == nil {
		return ""
	}
	provider, ok := s.registry.Get(runtimeType)
	if !ok {
		return ""
	}
	detection, err := provider.Detect(ctx, project)
	if err != nil || !detection.Detected {
		return ""
	}
	return strings.TrimSpace(detection.Version)
}

func normalizeRuntimeType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeRequestedVersion(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "v"))
	parts := strings.Split(value, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return ""
	}
	for _, part := range parts {
		if part == "" {
			return ""
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return ""
			}
		}
	}
	return value
}

func resolveAvailableVersion(requested string, available []AvailableVersion) (string, string, error) {
	prefix := requested + "."
	exact := strings.Count(requested, ".") == 2
	candidates := []AvailableVersion{}
	for _, item := range available {
		if !item.Installable {
			continue
		}
		if (exact && item.Version == requested) || (!exact && (item.Version == requested || strings.HasPrefix(item.Version, prefix))) {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("%w: version %s is not available to install", ErrInvalidRuntime, requested)
	}
	sort.Slice(candidates, func(i, j int) bool { return compareVersions(candidates[i].Version, candidates[j].Version) > 0 })
	return candidates[0].Version, candidates[0].InstallationMethod, nil
}

func versionSatisfiesRequirement(version, requirement string) bool {
	requirement = strings.TrimSpace(requirement)
	if requirement == "" || version == "" {
		return true
	}
	requirement = strings.TrimSpace(strings.TrimPrefix(requirement, "v"))
	for _, separator := range []string{"||", ","} {
		if strings.Contains(requirement, separator) {
			parts := strings.Split(requirement, separator)
			for _, part := range parts {
				if versionSatisfiesRequirement(version, part) {
					return true
				}
			}
			return false
		}
	}
	fields := strings.Fields(requirement)
	if len(fields) > 1 {
		for _, field := range fields {
			if !versionSatisfiesRequirement(version, field) {
				return false
			}
		}
		return true
	}
	switch {
	case strings.HasPrefix(requirement, ">="):
		return compareRequirement(version, strings.TrimSpace(strings.TrimPrefix(requirement, ">="))) >= 0
	case strings.HasPrefix(requirement, ">"):
		return compareRequirement(version, strings.TrimSpace(strings.TrimPrefix(requirement, ">"))) > 0
	case strings.HasPrefix(requirement, "<="):
		return compareRequirement(version, strings.TrimSpace(strings.TrimPrefix(requirement, "<="))) <= 0
	case strings.HasPrefix(requirement, "<"):
		return compareRequirement(version, strings.TrimSpace(strings.TrimPrefix(requirement, "<"))) < 0
	case strings.HasPrefix(requirement, "^"):
		base := strings.TrimPrefix(requirement, "^")
		if compareRequirement(version, base) < 0 {
			return false
		}
		return parseVersion(version)[0] == parseVersion(base)[0]
	case strings.HasPrefix(requirement, "~"):
		base := strings.TrimPrefix(requirement, "~")
		if compareRequirement(version, base) < 0 {
			return false
		}
		v := parseVersion(version)
		b := parseVersion(base)
		return v[0] == b[0] && v[1] == b[1]
	default:
		clean := strings.Trim(requirement, "*xX")
		if clean == "" {
			return true
		}
		parts := strings.Split(clean, ".")
		versionParts := strings.Split(version, ".")
		for index, part := range parts {
			if index >= len(versionParts) || part != versionParts[index] {
				return false
			}
		}
		return true
	}
}

func compareRequirement(version, requirement string) int {
	normalized := requirement
	parts := strings.Split(requirement, ".")
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	if len(parts) >= 3 {
		normalized = strings.Join(parts[:3], ".")
	}
	return compareVersions(version, normalized)
}

type runtimeJobLogger interface {
	Log(ctx context.Context, jobID, level, message string, fields map[string]any) error
}

type RuntimeJobHandler struct {
	typeName string
	service  *VersionService
	logger   runtimeJobLogger
	audit    *audit.Service
}

func NewRuntimeJobHandler(typeName string, service *VersionService, logger runtimeJobLogger, auditService *audit.Service) *RuntimeJobHandler {
	return &RuntimeJobHandler{typeName: typeName, service: service, logger: logger, audit: auditService}
}

func (h *RuntimeJobHandler) Type() string { return h.typeName }

func (h *RuntimeJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	installationID, _ := job.Payload["installation_id"].(string)
	if installationID == "" {
		return nil, errors.New("runtime job payload missing installation_id")
	}
	installation, err := h.service.repo.GetInstallation(ctx, installationID)
	if err != nil {
		return nil, err
	}
	provider, ok := h.service.providers[installation.RuntimeType]
	if !ok {
		return nil, ErrInvalidRuntime
	}
	reporter := &jobRuntimeReporter{ctx: ctx, jobID: job.ID, logger: h.logger}
	switch h.typeName {
	case JobRuntimeInstall:
		targetRoot := filepath.Join(h.service.root, installation.RuntimeType, installation.Version)
		result, installErr := provider.Install(ctx, installation.Version, targetRoot, reporter)
		if installErr != nil {
			_ = h.service.repo.MarkFailed(context.Background(), installation.ID, installErr.Error())
			h.recordAudit(context.Background(), job.RequestedBy, "runtime.install.failed", installation, map[string]any{"error": installErr.Error()})
			return nil, installErr
		}
		if err := h.service.repo.MarkInstalled(ctx, installation.ID, result.ExecutablePath, result.InstallationRoot, result.Tools); err != nil {
			_ = h.service.repo.MarkFailed(context.Background(), installation.ID, err.Error())
			return nil, err
		}
		installed, err := h.service.repo.GetInstallation(ctx, installation.ID)
		if err != nil {
			return nil, err
		}
		if err := provider.ValidateInstallation(ctx, installed); err != nil {
			_ = h.service.repo.MarkValidation(context.Background(), installed.ID, InstallationBroken, err.Error())
			h.recordAudit(context.Background(), job.RequestedBy, "runtime.install.failed", installation, map[string]any{"error": err.Error()})
			return nil, err
		}
		reporter.Stage("Completed")
		h.recordAudit(context.Background(), job.RequestedBy, "runtime.install.completed", installed, nil)
		return map[string]any{"installation_id": installed.ID, "runtime_type": installed.RuntimeType, "version": installed.Version, "executable_path": installed.ExecutablePath}, nil
	case JobRuntimeRemove:
		if err := provider.Remove(ctx, installation, reporter); err != nil {
			_ = h.service.repo.MarkFailed(context.Background(), installation.ID, err.Error())
			return nil, err
		}
		if err := h.service.repo.DeleteInstallation(ctx, installation.ID); err != nil {
			return nil, err
		}
		h.recordAudit(context.Background(), job.RequestedBy, "runtime.remove", installation, nil)
		return map[string]any{"installation_id": installation.ID, "removed": true}, nil
	default:
		return nil, fmt.Errorf("unsupported runtime job %s", h.typeName)
	}
}

func (h *RuntimeJobHandler) recordAudit(ctx context.Context, actor *string, action string, installation Installation, metadata map[string]any) {
	if h.audit == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["runtime_type"] = installation.RuntimeType
	metadata["version"] = installation.Version
	id := installation.ID
	_ = h.audit.Record(ctx, actor, action, "runtime_installation", &id, metadata, nil)
}

type jobRuntimeReporter struct {
	ctx    context.Context
	jobID  string
	logger runtimeJobLogger
}

func (r *jobRuntimeReporter) Stage(message string) {
	if r.logger != nil {
		_ = r.logger.Log(r.ctx, r.jobID, "info", "runtime.install.stage", map[string]any{"stage": message})
	}
}

func (r *jobRuntimeReporter) Log(message string) {
	if r.logger != nil {
		_ = r.logger.Log(r.ctx, r.jobID, "info", message, nil)
	}
}

var _ jobs.Handler = (*RuntimeJobHandler)(nil)

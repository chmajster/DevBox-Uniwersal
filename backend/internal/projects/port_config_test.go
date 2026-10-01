package projects

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

// Extend the existing deployment fixture with the real allocator's optional
// contract; fixture ports remain deterministic independently of host sockets.
func (p *integrationPorts) ReserveFromOwned(ctx context.Context, projectID, purpose string, _ int) (providers.PortReservation, error) {
	lease, err := p.Reserve(ctx, projectID, purpose, nil)
	return providers.PortReservation{PortLease: lease}, err
}

type ownedPortFixture struct{ leases map[int]providers.PortLease }

func (p *ownedPortFixture) Reserve(ctx context.Context, projectID, purpose string, preferred *int) (providers.PortLease, error) {
	start := 8080
	if preferred != nil {
		start = *preferred
	}
	reservation, err := p.ReserveFromOwned(ctx, projectID, purpose, start)
	return reservation.PortLease, err
}
func (p *ownedPortFixture) ReserveFromOwned(_ context.Context, projectID, purpose string, start int) (providers.PortReservation, error) {
	for port := start; port <= 65535; port++ {
		if existing, ok := p.leases[port]; ok && existing.ProjectID == projectID && existing.Purpose == purpose {
			return providers.PortReservation{PortLease: existing, Reused: true}, nil
		}
		if _, used := p.leases[port]; !used {
			lease := providers.PortLease{Port: port, ProjectID: projectID, Purpose: purpose}
			p.leases[port] = lease
			return providers.PortReservation{PortLease: lease}, nil
		}
	}
	return providers.PortReservation{}, errors.New("no free ports")
}
func (p *ownedPortFixture) Release(_ context.Context, port int) error {
	delete(p.leases, port)
	return nil
}
func (p *ownedPortFixture) IsAvailable(_ context.Context, port int) (bool, error) {
	_, exists := p.leases[port]
	return !exists, nil
}
func (p *ownedPortFixture) Owns(_ context.Context, projectID, purpose string, port int) (bool, error) {
	lease, ok := p.leases[port]
	return ok && lease.ProjectID == projectID && lease.Purpose == purpose, nil
}
func (p *ownedPortFixture) ReleaseOwned(ctx context.Context, projectID, purpose string, port int) error {
	owned, _ := p.Owns(ctx, projectID, purpose, port)
	if owned {
		delete(p.leases, port)
	}
	return nil
}

func TestPortConfigurationDefaultsValidationAndDeploymentLock(t *testing.T) {
	repo, project, deploymentID := integrationProject(t, Project{Runtime: "static"})
	ctx := context.Background()
	service := NewService(repo, nil, nil, nil, t.TempDir())
	config, err := service.PortConfiguration(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if config.Settings.HostPort != 8080 || config.Settings.HTTPSHostPort != 8443 || config.Settings.HTTPSEnabled || config.Applied != nil {
		t.Fatalf("defaults: %+v", config)
	}
	if _, err := service.UpdatePortConfiguration(ctx, project.ID, DefaultPortSettings()); !errors.Is(err, ErrPortConfigurationBusy) {
		t.Fatalf("deployment lock = %v", err)
	}
	if err := repo.FinishDeployment(ctx, deploymentID, DeploymentSuccess, DeploymentSuccess, "", "", time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	settings := DefaultPortSettings()
	settings.ContainerPort = 80
	settings.HostPort = 9080
	saved, err := service.UpdatePortConfiguration(ctx, project.ID, settings)
	if err != nil || !saved.Configured || !reflect.DeepEqual(saved.Settings, settings) || saved.Applied != nil {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	for _, invalid := range []int{0, -1, 65536} {
		bad := settings
		bad.HostPort = invalid
		if _, err := service.UpdatePortConfiguration(ctx, project.ID, bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted host port %d: %v", invalid, err)
		}
	}
	settings.HTTPSEnabled = true
	if _, err := service.UpdatePortConfiguration(ctx, project.ID, settings); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("generated runtime pretended to support TLS: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.LocalPath, "Dockerfile"), []byte("FROM nginx:alpine\nEXPOSE 80 443\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdatePortConfiguration(ctx, project.ID, settings); err != nil {
		t.Fatalf("custom TLS publishing rejected: %v", err)
	}
	if _, err := service.PortConfiguration(ctx, "missing-project"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not found = %v", err)
	}
}

func TestPortConfigurationIgnoresCancelledQueuedDeploymentJob(t *testing.T) {
	repo, project, deploymentID := integrationProject(t, Project{Runtime: "static"})
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	jobID := NewID()
	if _, err := repo.db.ExecContext(ctx,
		`INSERT INTO jobs(id,type,status,project_id,payload_json,created_at,finished_at) VALUES(?,?,?,?,?,?,?)`,
		jobID, JobDeploy, "cancelled", project.ID, "{}", now, now,
	); err != nil {
		t.Fatal(err)
	}
	if err := repo.BindDeploymentJob(ctx, deploymentID, jobID); err != nil {
		t.Fatal(err)
	}

	service := NewService(repo, nil, nil, nil, t.TempDir())
	settings := DefaultPortSettings()
	settings.ContainerPort = 80
	settings.HostPort = 9080
	saved, err := service.UpdatePortConfiguration(ctx, project.ID, settings)
	if err != nil {
		t.Fatalf("cancelled queued deployment must not lock port settings: %v", err)
	}
	if !saved.Configured || saved.Settings.HostPort != 9080 {
		t.Fatalf("unexpected saved settings after cancelled job: %+v", saved)
	}
}

func TestPortPlanPersistsFallbackAndPreservesOldLeaseOnFailure(t *testing.T) {
	repo, project, _ := integrationProject(t, Project{Runtime: "static"})
	ctx := context.Background()
	allocator := &ownedPortFixture{leases: map[int]providers.PortLease{8080: {Port: 8080, ProjectID: "other", Purpose: httpPortPurpose}}}
	h := &DeploymentHandler{repo: repo, integrations: DeploymentIntegrations{Ports: allocator}}
	config := PortConfiguration{Settings: DefaultPortSettings()}
	first, err := h.preparePortPlan(ctx, project, config, 80)
	if err != nil || first.state.HTTP.HostPort != 8081 {
		t.Fatalf("fallback = %+v, %v", first, err)
	}
	if err := first.commit(); err != nil {
		t.Fatal(err)
	}
	stored, err := NewRepository(repo.db).PortConfiguration(ctx, project.ID)
	if err != nil || stored.Applied == nil || stored.Applied.HTTP.HostPort != 8081 || stored.Settings.HostPort != 8081 {
		t.Fatalf("not persisted = %+v, %v", stored, err)
	}
	restarted := &DeploymentHandler{repo: NewRepository(repo.db), integrations: DeploymentIntegrations{Ports: allocator}}
	redeploy, err := restarted.preparePortPlan(ctx, project, stored, 80)
	if err != nil || redeploy.state.HTTP.HostPort != 8081 || len(redeploy.newLeases) != 0 {
		t.Fatalf("redeploy changed owned port: %+v, %v", redeploy, err)
	}
	stored.Settings.HostPort = 9080
	failed, err := h.preparePortPlan(ctx, project, stored, 80)
	if err != nil {
		t.Fatal(err)
	}
	failed.rollback()
	if _, found := allocator.leases[9080]; found {
		t.Fatal("failed deployment leaked a new lease")
	}
	if _, found := allocator.leases[8081]; !found {
		t.Fatal("failed deployment released the previous running application's port")
	}
	stored, err = repo.PortConfiguration(ctx, project.ID)
	if err != nil || stored.Applied.HTTP.HostPort != 8081 {
		t.Fatalf("failure overwrote applied mapping: %+v, %v", stored, err)
	}
	stored.Settings.HostPort = 9080
	successful, err := h.preparePortPlan(ctx, project, stored, 80)
	if err != nil {
		t.Fatal(err)
	}
	if err := successful.commit(); err != nil {
		t.Fatal(err)
	}
	if _, found := allocator.leases[8081]; found {
		t.Fatal("old lease not released after successful replacement")
	}
	successful.rollback() // e.g. a later reverse-proxy error must not release an active port.
	if _, found := allocator.leases[9080]; !found {
		t.Fatal("active port released after replacement")
	}
}

func TestPortPlanIndependentHTTPSFallbackAndDisable(t *testing.T) {
	repo, project, _ := integrationProject(t, Project{Runtime: "static"})
	ctx := context.Background()
	allocator := &ownedPortFixture{leases: map[int]providers.PortLease{8443: {Port: 8443, ProjectID: "other", Purpose: httpsPortPurpose}}}
	h := &DeploymentHandler{repo: repo, integrations: DeploymentIntegrations{Ports: allocator}}
	settings := DefaultPortSettings()
	settings.HTTPSEnabled = true
	plan, err := h.preparePortPlan(ctx, project, PortConfiguration{Settings: settings}, 80)
	if err != nil || plan.state.HTTP.HostPort != 8080 || plan.state.HTTPS.HostPort != 8444 {
		t.Fatalf("HTTPS allocation = %+v, %v", plan, err)
	}
	if err := plan.commit(); err != nil {
		t.Fatal(err)
	}
	config, err := repo.PortConfiguration(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	config.Settings.HTTPSEnabled = false
	next, err := h.preparePortPlan(ctx, project, config, 80)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := allocator.leases[8444]; !found {
		t.Fatal("TLS lease released before replacement")
	}
	if err := next.commit(); err != nil {
		t.Fatal(err)
	}
	if _, found := allocator.leases[8444]; found {
		t.Fatal("TLS lease retained after disabling HTTPS")
	}
	if allocator.leases[8443].ProjectID != "other" {
		t.Fatal("foreign HTTPS lease changed")
	}
}

func TestPortConfigurationHTTPRejectsInvalidPayloadAndBusyDeployment(t *testing.T) {
	repo, project, _ := integrationProject(t, Project{Runtime: "static"})
	module := &Module{service: NewService(repo, nil, nil, nil, t.TempDir())}
	for _, body := range []string{`{"host_port":0}`, `{"host_port":65536}`, `{"host_port":8080.5}`, `{"host_port":8080,"applied":{"http":{"host_port":1}}}`} {
		req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
		req.SetPathValue("id", project.ID)
		response := httptest.NewRecorder()
		module.updatePortConfiguration(response, req)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s returned %d: %s", body, response.Code, response.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"host_port":8080}`))
	req.SetPathValue("id", project.ID)
	response := httptest.NewRecorder()
	module.updatePortConfiguration(response, req)
	if response.Code != http.StatusConflict {
		t.Fatalf("busy status = %d: %s", response.Code, response.Body.String())
	}
}

func TestComposeDiscoveryPersistsPublishedAndResolvedHostPort(t *testing.T) {
	repo, project, _ := integrationProject(t, Project{Runtime: "static"})
	ctx := context.Background()
	discovery := providers.ComposePortDiscovery{
		Selected: &providers.ComposePortCandidate{
			Service: "app", ContainerPort: 80, HostPort: 9080,
			Protocol: "http", Source: "docker-compose.yml",
		},
		Candidates: []providers.ComposePortCandidate{{
			Service: "app", ContainerPort: 80, HostPort: 9080,
			Protocol: "http", Source: "docker-compose.yml",
		}},
		Fingerprint: "compose-v1",
	}
	config, err := repo.saveComposePortDiscovery(ctx, project.ID, discovery)
	if err != nil {
		t.Fatal(err)
	}
	if config.Settings.ComposeService != "app" || config.Settings.ContainerPort != 80 ||
		config.Settings.HostPort != 9080 || config.Settings.Protocol != "http" ||
		config.Settings.DetectionMode != "automatic" || config.Settings.DetectionSource != "docker-compose.yml" {
		t.Fatalf("discovery not persisted completely: %+v", config.Settings)
	}
	if err := repo.saveResolvedComposeHostPort(ctx, project.ID, 9081); err != nil {
		t.Fatal(err)
	}
	config, err = repo.PortConfiguration(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if config.Settings.HostPort != 9081 {
		t.Fatalf("resolved host port = %d, want 9081", config.Settings.HostPort)
	}
}

func TestManualComposePortSelectionIsNotOverwrittenByAutomaticDiscovery(t *testing.T) {
	repo, project, deploymentID := integrationProject(t, Project{Runtime: "static"})
	ctx := context.Background()
	if err := repo.FinishDeployment(ctx, deploymentID, DeploymentSuccess, DeploymentSuccess, "", "", time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	service := NewService(repo, nil, nil, nil, t.TempDir())
	settings := DefaultPortSettings()
	settings.ReverseProxyMode = "manual"
	settings.ComposeService = "app"
	settings.ContainerPort = 8080
	saved, err := service.UpdatePortConfiguration(ctx, project.ID, settings)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Settings.DetectionMode != "manual" {
		t.Fatalf("manual selection mode = %q", saved.Settings.DetectionMode)
	}

	discovery := providers.ComposePortDiscovery{
		Selected: &providers.ComposePortCandidate{Service: "frontend", ContainerPort: 3000, Protocol: "http", Source: "docker-compose.yml"},
		Candidates: []providers.ComposePortCandidate{
			{Service: "frontend", ContainerPort: 3000, Protocol: "http", Source: "docker-compose.yml"},
		},
		Fingerprint: "new-compose-fingerprint",
	}
	after, err := repo.saveComposePortDiscovery(ctx, project.ID, discovery)
	if err != nil {
		t.Fatal(err)
	}
	if after.Settings.ComposeService != "app" || after.Settings.ContainerPort != 8080 {
		t.Fatalf("automatic discovery overwrote manual app:8080 selection: %+v", after.Settings)
	}
	if after.Settings.ComposeFingerprint != "new-compose-fingerprint" || len(after.Settings.Candidates) != 1 {
		t.Fatalf("discovery metadata was not refreshed: %+v", after.Settings)
	}
}

func TestProjectPrimaryPortDoesNotBecomeHTTPS(t *testing.T) {
	repo, project, _ := integrationProject(t, Project{Runtime: "static"})
	for i, purpose := range []string{httpPortPurpose, httpsPortPurpose} {
		port := 8080
		if i == 1 {
			port = 8443
		}
		_, err := repo.db.Exec(`INSERT INTO ports(id,project_id,port,purpose,state,created_at) VALUES(?,?,?,?,?,?)`, NewID(), project.ID, port, purpose, "reserved", time.Now().Add(time.Duration(i)*time.Second).UTC().Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.Get(context.Background(), project.ID)
	if err != nil || got.Port == nil || *got.Port != 8080 {
		t.Fatalf("primary port became HTTPS: %+v, %v", got.Port, err)
	}
}

func (p *ownedPortFixture) ReserveFrom(ctx context.Context, projectID, purpose string, start int) (providers.PortLease, error) {
	reservation, err := p.ReserveFromOwned(ctx, projectID, purpose, start)
	return reservation.PortLease, err
}

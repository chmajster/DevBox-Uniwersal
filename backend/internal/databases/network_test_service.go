package databases

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"net/http"
	"time"
)

func (s *Service) TestApplicationNetwork(ctx context.Context, projectID string) (providers.NetworkDiagnostic, error) {
	project, err := s.repo.ProjectByID(ctx, projectID)
	if err != nil {
		return providers.NetworkDiagnostic{}, err
	}
	b, err := s.repo.DatabaseBindingByProject(ctx, projectID)
	if err != nil {
		return providers.NetworkDiagnostic{}, err
	}
	// Intentionally avoid ResolveRuntimeDatabase: a network test does not decrypt
	// credentials, provision databases, start MySQL or issue a SQL query.
	connection, err := s.ResolveApplicationConnection(ctx, projectID)
	if err != nil {
		return providers.NetworkDiagnostic{}, err
	}
	if connection.Mode == DatabaseModeNone {
		return providers.NetworkDiagnostic{}, fmt.Errorf("database not selected")
	}
	tester, ok := s.compose.(providers.ApplicationNetworkTester)
	if !ok {
		return providers.NetworkDiagnostic{}, fmt.Errorf("container network diagnostics are unavailable")
	}
	work, err := databaseWorkingDirectory(project)
	if err != nil {
		return providers.NetworkDiagnostic{}, err
	}
	return tester.TestApplicationNetwork(ctx, providers.ApplicationNetworkTarget{ProjectID: projectID, Directory: work, ProjectName: project.Slug, ApplicationService: b.ApplicationService, Host: connection.Host, Port: connection.Port})
}

const JobDatabaseNetworkTest = "database.network-test"

type NetworkTestHandler struct{ service *Service }

func (h *NetworkTestHandler) Type() string { return JobDatabaseNetworkTest }
func (h *NetworkTestHandler) Run(parent context.Context, job domain.Job) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	if job.ProjectID == nil {
		return nil, fmt.Errorf("project ID is required")
	}
	report, err := h.service.TestApplicationNetwork(ctx, *job.ProjectID)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	h.service.recordAudit(ctx, job.RequestedBy, "database_binding.network_test", "project", job.ProjectID, map[string]any{"dns_resolved": report.DNSResolved, "tcp_reachable": report.TCPReachable}, nil)
	return result, nil
}
func (m *Module) testDatabaseNetwork(w http.ResponseWriter, r *http.Request) {
	s := m.service
	if s.jobs == nil {
		writeModuleError(w, fmt.Errorf("job engine is not configured"))
		return
	}
	id := r.PathValue("id")
	if _, err := s.repo.ProjectByID(r.Context(), id); err != nil {
		writeModuleError(w, err)
		return
	}
	actor, remote := requestIdentity(r)
	job, err := s.jobs.Enqueue(r.Context(), jobs.Request{Type: JobDatabaseNetworkTest, ProjectID: &id, RequestedBy: actor})
	if err != nil {
		writeModuleError(w, err)
		return
	}
	s.recordAudit(r.Context(), actor, "database_binding.network_test.queued", "job", &job.ID, map[string]any{"project_id": id}, remote)
	writeData(w, http.StatusAccepted, job)
}

package applications

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type RuntimeCleanup interface{ RemoveRuntimeFiles(string) error }

func (s *Service) WithRouting(routing providers.ReverseProxyProvider) *Service {
	s.routing = routing
	return s
}
func (s *Service) WithRuntimeCleanup(cleanup RuntimeCleanup) *Service {
	s.runtimeCleanup = cleanup
	return s
}

type savedRoute struct {
	ID, EndpointID, Domain, TLSMode string
	Port                            int
	Active                          bool
}

func (r *Repository) routes(ctx context.Context, id string) ([]savedRoute, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,endpoint_id,domain,target_port,tls_mode,active FROM application_routes WHERE application_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []savedRoute
	for rows.Next() {
		var item savedRoute
		if err := rows.Scan(&item.ID, &item.EndpointID, &item.Domain, &item.Port, &item.TLSMode, &item.Active); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Persist ownership before activating Nginx. A rejected candidate never reloads
// the proxy. Routes remain separately observable from container health.
func (s *Service) syncRoutes(ctx context.Context, id string) error {
	endpoints, err := s.repo.Endpoints(ctx, id)
	if err != nil {
		return err
	}
	old, err := s.repo.routes(ctx, id)
	if err != nil {
		return err
	}
	desired := map[string]bool{}
	for _, endpoint := range endpoints {
		if endpoint.Domain == nil || strings.TrimSpace(*endpoint.Domain) == "" {
			continue
		}
		if s.routing == nil {
			return fmt.Errorf("%w: reverse proxy is unavailable", ErrProviderUnavailable)
		}
		if endpoint.HostPort == nil || endpoint.Status != ObservedRunning {
			return fmt.Errorf("domain requires a running published endpoint")
		}
		domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(*endpoint.Domain), "."))
		desired[domain] = true
		var legacy int
		if err := s.repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains WHERE hostname=?`, domain).Scan(&legacy); err != nil {
			return err
		}
		if legacy > 0 {
			return fmt.Errorf("%w: domain is already owned", ErrConflict)
		}
		var previous *savedRoute
		for i := range old {
			if old[i].Domain == domain {
				previous = &old[i]
				break
			}
		}
		routeID := NewID()
		now := dbTime(time.Now())
		if previous != nil {
			routeID = previous.ID
		} else {
			_, err = s.repo.db.ExecContext(ctx, `INSERT INTO application_routes(id,application_id,endpoint_id,domain,target_port,tls_mode,active,created_at,updated_at) VALUES(?,?,?,?,?,?,0,?,?)`, routeID, id, endpoint.ID, domain, *endpoint.HostPort, endpoint.TLSMode, now, now)
			if err != nil {
				return classifyDBError("reserve domain", err)
			}
		}
		route := providers.ProxyRoute{Domain: domain, Upstream: fmt.Sprintf("http://127.0.0.1:%d", *endpoint.HostPort), TLS: endpoint.TLSMode == "existing"}
		if err = s.routing.Apply(ctx, route); err != nil {
			if previous == nil {
				_, _ = s.repo.db.ExecContext(context.Background(), `DELETE FROM application_routes WHERE id=?`, routeID)
			}
			return err
		}
		_, err = s.repo.db.ExecContext(ctx, `UPDATE application_routes SET endpoint_id=?,target_port=?,tls_mode=?,active=1,updated_at=? WHERE id=?`, endpoint.ID, *endpoint.HostPort, endpoint.TLSMode, now, routeID)
		if err != nil {
			return err
		}
	}
	for _, route := range old {
		if !desired[route.Domain] {
			if s.routing == nil {
				return ErrProviderUnavailable
			}
			if err := s.routing.Remove(ctx, route.Domain); err != nil {
				return err
			}
			if _, err := s.repo.db.ExecContext(ctx, `DELETE FROM application_routes WHERE id=?`, route.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) removeRoutes(ctx context.Context, id string) error {
	routes, err := s.repo.routes(ctx, id)
	if err != nil {
		return err
	}
	for _, route := range routes {
		if s.routing == nil {
			return ErrProviderUnavailable
		}
		if err := s.routing.Remove(ctx, route.Domain); err != nil {
			return err
		}
		if _, err := s.repo.db.ExecContext(ctx, `DELETE FROM application_routes WHERE id=?`, route.ID); err != nil {
			return err
		}
	}
	return nil
}

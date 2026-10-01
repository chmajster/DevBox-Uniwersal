package docker

import "testing"

func TestComposeApplicationPortDiscovery(t *testing.T) {
	tests := []struct {
		name          string
		config        string
		healthcheck   string
		dockerfilePort int
		service       string
		port          int
		ambiguous     bool
		candidates    int
		infrastructure int
	}{
		{
			name: "A app plus mysql and redis",
			config: `{"services":{
				"app":{"ports":[{"target":80,"published":"8080","protocol":"tcp"}]},
				"mysql":{"image":"mysql:8.4","ports":[{"target":3306,"published":"3306","protocol":"tcp"}]},
				"redis":{"image":"redis:7","ports":[{"target":6379,"published":"6379","protocol":"tcp"}]}
			}}`,
			service: "app", port: 80, candidates: 1, infrastructure: 2,
		},
		{
			name: "B web plus postgres",
			config: `{"services":{
				"web":{"ports":[{"target":3000,"published":"3000","protocol":"tcp"}]},
				"postgres":{"image":"postgres:17","ports":[{"target":5432,"published":"5432","protocol":"tcp"}]}
			}}`,
			service: "web", port: 3000, candidates: 1, infrastructure: 1,
		},
		{
			name: "C api and frontend remain candidates but mysql does not",
			config: `{"services":{
				"api":{"ports":[{"target":8000,"published":"8000","protocol":"tcp"}]},
				"frontend":{"ports":[{"target":3000,"published":"3000","protocol":"tcp"}]},
				"mysql":{"image":"mysql:8","ports":[{"target":3306,"published":"3306","protocol":"tcp"}]}
			}}`,
			ambiguous: true, candidates: 2, infrastructure: 1,
		},
		{
			name: "D infrastructure only has no application port",
			config: `{"services":{
				"mysql":{"image":"mysql:8","ports":[{"target":3306,"published":"3306","protocol":"tcp"}]},
				"redis":{"image":"redis:7","ports":[{"target":6379,"published":"6379","protocol":"tcp"}]}
			}}`,
			ambiguous: true, candidates: 0, infrastructure: 2,
		},
		{
			name: "F healthcheck selects application listener",
			config: `{"services":{
				"app":{"image":"example/app"},
				"mysql":{"image":"mysql:8","ports":[{"target":3306,"published":"3306","protocol":"tcp"}]}
			}}`,
			healthcheck: "http://app:8000/health",
			service: "app", port: 8000, candidates: 1, infrastructure: 1,
		},
		{
			name: "G expose only is detected",
			config: `{"services":{"app":{"image":"example/app","expose":["80"]}}}`,
			service: "app", port: 80, candidates: 1,
		},
		{
			name: "H Dockerfile EXPOSE is fallback",
			config: `{"services":{"app":{"image":"example/app"}}}`,
			dockerfilePort: 8080,
			service: "app", port: 8080, candidates: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := discoverComposeApplicationPorts([]byte(test.config), test.healthcheck, test.dockerfilePort)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Candidates) != test.candidates {
				t.Fatalf("candidates = %+v, want %d", got.Candidates, test.candidates)
			}
			if len(got.Infrastructure) != test.infrastructure {
				t.Fatalf("infrastructure = %+v, want %d", got.Infrastructure, test.infrastructure)
			}
			if test.ambiguous {
				if got.Selected != nil {
					t.Fatalf("selected infrastructure or ambiguous candidate: %+v", got.Selected)
				}
				for _, candidate := range got.Candidates {
					if candidate.Service == "mysql" || candidate.Service == "redis" || candidate.Service == "postgres" {
						t.Fatalf("infrastructure leaked into application candidates: %+v", candidate)
					}
				}
				return
			}
			if got.Selected == nil {
				t.Fatalf("no selected application port: %+v", got)
			}
			if got.Selected.Service != test.service || got.Selected.ContainerPort != test.port {
				t.Fatalf("selected = %+v, want %s:%d", got.Selected, test.service, test.port)
			}
		})
	}
}

func TestSelectComposePortBindingNeverSelectsInfrastructure(t *testing.T) {
	data := []byte(`{"services":{
		"mysql":{"image":"mysql:8","ports":[{"target":3306,"published":"3306","protocol":"tcp"}]},
		"redis":{"image":"redis:7","ports":[{"target":6379,"published":"6379","protocol":"tcp"}]}
	}}`)
	if _, err := selectComposePortBinding(data); err == nil {
		t.Fatal("infrastructure-only Compose unexpectedly produced reverse proxy binding")
	}
}

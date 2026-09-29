package docker

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type replacementRunner struct {
	calls                             []string
	old, oldRunning, renamed, created bool
	failStart, panicStart             bool
}

func (f *replacementRunner) Stream(context.Context, ...string) (io.ReadCloser, error) {
	return nil, errors.New("not used")
}
func (f *replacementRunner) Run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	call := strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if strings.HasPrefix(call, "container inspect --format") {
		return []byte("true"), nil, nil
	}
	if strings.HasPrefix(call, "container inspect ") {
		if !f.created && (!f.old || f.renamed) {
			return nil, nil, ErrNotFound
		}
		image := "old:1"
		if f.created {
			image = "new:1"
		}
		b, _ := json.Marshal([]map[string]any{{"Name": "/audit-container", "Config": map[string]any{"Image": image, "Labels": map[string]string{"io.devbox.project": "audit"}}, "State": map[string]any{"Running": f.oldRunning, "Status": "running"}}})
		return b, nil, nil
	}
	if strings.HasPrefix(call, "container rename audit-container ") {
		f.renamed = true
	}
	if strings.HasPrefix(call, "container rename audit-container-previous-") {
		f.renamed = false
	}
	if strings.HasPrefix(call, "container create ") {
		f.created = true
	}
	if strings.HasPrefix(call, "container start ") && f.created {
		if f.panicStart {
			panic("private-token")
		}
		if f.failStart {
			return nil, nil, errors.New("injected start failure")
		}
	}
	if strings.HasPrefix(call, "container rm -f -v audit-container") && !strings.Contains(call, "previous-") {
		f.created = false
	}
	return nil, nil, nil
}
func replacementSpec(t *testing.T) (containerspec.DeploymentSpec, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ready" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	}))
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	return containerspec.DeploymentSpec{Image: "new:1", Runtime: "custom", ContainerName: "audit-container", HostPort: port, ContainerPort: 8080, Labels: map[string]string{"io.devbox.project": "audit"}, Healthcheck: containerspec.Healthcheck{Target: "/ready", ExpectedStatuses: []int{204}, StartupSeconds: 1}}, server.Close
}
func TestReplacementAcceptsZeroOneAndMultipleNetworks(t *testing.T) {
	for _, networks := range [][]string{nil, {}, {"apps"}, {"apps", "database"}} {
		t.Run(strings.Join(networks, "-"), func(t *testing.T) {
			spec, close := replacementSpec(t)
			defer close()
			spec.Networks = networks
			runner := &replacementRunner{}
			p := newCLIProviderWithRunner(runner)
			if err := p.ReplaceManagedPorts(context.Background(), spec, nil); err != nil {
				t.Fatal(err)
			}
			calls := strings.Join(runner.calls, "\n")
			if len(networks) == 2 && !strings.Contains(calls, "network connect database audit-container") {
				t.Fatal("missing secondary network")
			}
		})
	}
}
func TestReplacementRestoresOnlyPreviouslyRunningContainer(t *testing.T) {
	for _, running := range []bool{false, true} {
		for _, panicking := range []bool{false, true} {
			t.Run(strconv.FormatBool(running)+strconv.FormatBool(panicking), func(t *testing.T) {
				spec, close := replacementSpec(t)
				defer close()
				runner := &replacementRunner{old: true, oldRunning: running, failStart: !panicking, panicStart: panicking}
				p := newCLIProviderWithRunner(runner)
				err := p.ReplaceManagedPorts(context.Background(), spec, nil)
				if err == nil || strings.Contains(err.Error(), "private-token") {
					t.Fatalf("unsafe failure: %v", err)
				}
				if runner.renamed || runner.created {
					t.Fatal("previous container not restored")
				}
				starts := 0
				for _, call := range runner.calls {
					if call == "container start audit-container" {
						starts++
					}
					if strings.HasPrefix(call, "container rm -f -v audit-container-previous-") {
						t.Fatal("deleted recovery backup on failure")
					}
				}
				want := 1
				if running {
					want = 2
				}
				if starts != want {
					t.Fatalf("wrong prior running state, starts=%d want=%d", starts, want)
				}
			})
		}
	}
}
func TestInvalidReplacementDoesNotStopPreviousContainer(t *testing.T) {
	spec, close := replacementSpec(t)
	defer close()
	spec.Networks = []string{"invalid network"}
	runner := &replacementRunner{old: true, oldRunning: true}
	err := newCLIProviderWithRunner(runner).ReplaceManagedPorts(context.Background(), spec, nil)
	if err == nil {
		t.Fatal("accepted invalid network")
	}
	for _, call := range runner.calls {
		if strings.Contains(call, " stop ") || strings.Contains(call, " rename ") {
			t.Fatal("invalid request interrupted previous app")
		}
	}
}

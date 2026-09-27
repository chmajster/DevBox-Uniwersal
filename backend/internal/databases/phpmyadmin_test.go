package databases

import "testing"

func TestDockerMySQLTarget(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantHost    string
		wantGateway bool
	}{
		{name: "ipv4 loopback", input: "127.0.0.1", wantHost: "host.docker.internal", wantGateway: true},
		{name: "localhost", input: "localhost", wantHost: "host.docker.internal", wantGateway: true},
		{name: "ipv6 loopback", input: "::1", wantHost: "host.docker.internal", wantGateway: true},
		{name: "remote mysql", input: "mysql.internal", wantHost: "mysql.internal", wantGateway: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, gateway := dockerMySQLTarget(tt.input)
			if host != tt.wantHost || gateway != tt.wantGateway {
				t.Fatalf("dockerMySQLTarget(%q) = (%q, %v), want (%q, %v)", tt.input, host, gateway, tt.wantHost, tt.wantGateway)
			}
		})
	}
}

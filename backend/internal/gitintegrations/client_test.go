package gitintegrations

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type testTokenReader struct{ issued []byte }

func (r *testTokenReader) ReadToken(context.Context, string) ([]byte, error) {
	r.issued = []byte("sensitive-provider-token")
	return r.issued, nil
}

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestProfileValidationAndTokenOrigin(t *testing.T) {
	for _, address := range []string{"http://gitlab.com", "https://user:pass@gitlab.com", "https://gitlab.com/?token=x", "https://gitlab.com/a/../b", "https://gitlab.com/%2e%2e/b"} {
		if _, err := normalize(Integration{Name: "test", Provider: "gitlab", BaseURL: address, CredentialID: "c"}); err == nil {
			t.Errorf("unsafe profile %s", address)
		}
	}
	i, err := normalize(Integration{Name: "test", Provider: "gitlab", BaseURL: "https://gitlab.example/subpath/api/v4/", CredentialID: "c"})
	if err != nil || i.apiBase() != "https://gitlab.example/subpath/api/v4" {
		t.Fatalf("GitLab self-hosted path: %#v %v", i, err)
	}
	if _, err := normalize(Integration{Name: "test", Provider: "github", BaseURL: "https://other.example", CredentialID: "c"}); err == nil {
		t.Fatal("GitHub token could target another server")
	}
	if safeWebURL("https://evil.example/repo", Integration{Provider: "github", BaseURL: "https://api.github.com"}) != "" {
		t.Fatal("unsafe returned URL")
	}
}
func TestProviderNeverFollowsRedirectOrEchoesSecrets(t *testing.T) {
	credentials := &testTokenReader{}
	s := NewService(nil, credentials)
	calls := 0
	s.client.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer sensitive-provider-token" {
			t.Fatal("incorrect token destination")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.example"}}, Body: io.NopCloser(strings.NewReader("sensitive-provider-token"))}, nil
	})
	var target any
	_, err := s.get(context.Background(), Integration{Provider: "github", BaseURL: "https://api.github.com", CredentialID: "c"}, "/user", &target)
	if err == nil || calls != 1 || strings.Contains(err.Error(), "sensitive-provider-token") {
		t.Fatalf("unsafe redirect/error: %d %v", calls, err)
	}
	for _, b := range credentials.issued {
		if b != 0 {
			t.Fatal("decrypted token not cleared")
		}
	}
}

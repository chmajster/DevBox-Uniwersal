package runtimeimages

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type auditTransport func(*http.Request) (*http.Response, error)

func (f auditTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func jsonResponse(body string, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body))}
}
func TestCatalogPaginationAndTokenAudience(t *testing.T) {
	for _, badLink := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "reject-cross-origin"}[badLink], func(t *testing.T) {
			c := NewCatalog()
			calls := 0
			c.client.Transport = auditTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				switch calls {
				case 1:
					if r.URL.Host != "auth.docker.io" || r.Header.Get("Authorization") != "" {
						t.Fatal("incorrect anonymous auth")
					}
					return jsonResponse(`{"token":"public-read-token"}`, nil), nil
				case 2:
					if r.URL.Host != "registry-1.docker.io" || r.Header.Get("Authorization") != "Bearer public-read-token" {
						t.Fatal("incorrect registry audience")
					}
					link := `</v2/library/node/tags/list?n=2&last=20-bookworm-slim>; rel="next"`
					if badLink {
						link = `<https://evil.example/tags>; rel="next"`
					}
					return jsonResponse(`{"tags":["20-bookworm-slim","22-rc-bookworm-slim","latest"]}`, http.Header{"Link": []string{link}}), nil
				case 3:
					if r.URL.Host != "registry-1.docker.io" {
						t.Fatal("leaked registry token")
					}
					return jsonResponse(`{"tags":["22-bookworm-slim","22.1.0-bookworm-slim"]}`, nil), nil
				default:
					t.Fatal("unexpected request")
				}
				return nil, nil
			})
			versions, err := c.Versions(context.Background(), "node")
			if badLink {
				if err == nil || calls != 2 {
					t.Fatal("unsafe pagination accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(versions, ",") != "22.1.0,22,20" {
				t.Fatal(versions)
			}
			versions[0] = "mutated"
			again, err := c.Versions(context.Background(), "node")
			if err != nil || again[0] != "22.1.0" || calls != 3 {
				t.Fatal("cache was mutable or not reused")
			}
		})
	}
}

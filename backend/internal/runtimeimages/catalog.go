package runtimeimages

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type catalogEntry struct {
	versions []string
	expires  time.Time
}
type Catalog struct {
	client  *http.Client
	mu      sync.Mutex
	entries map[string]catalogEntry
}

func NewCatalog() *Catalog {
	return &Catalog{client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, entries: map[string]catalogEntry{}}
}

// Only fixed public image repositories are contacted. Registry tokens are
// anonymous, read-only, ephemeral and never persisted or returned to the UI.
func (c *Catalog) Versions(ctx context.Context, runtime string) ([]string, error) {
	repository, suffix, err := containerspec.RuntimeImageCoordinates(runtime)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	cached, ok := c.entries[runtime]
	c.mu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return append([]string{}, cached.versions...), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var token struct {
		Token string `json:"token"`
	}
	authURL := "https://auth.docker.io/token?service=registry.docker.io&scope=" + url.QueryEscape("repository:"+repository+":pull")
	if _, err = c.get(ctx, authURL, "", &token); err != nil {
		return nil, err
	}
	if token.Token == "" {
		return nil, fmt.Errorf("registry did not issue an anonymous read token")
	}
	versions := map[string]bool{}
	endpoint := "https://registry-1.docker.io/v2/" + repository + "/tags/list?n=10000"
	visited := map[string]bool{}
	for page := 0; page < 100; page++ {
		if visited[endpoint] {
			return nil, fmt.Errorf("registry pagination did not advance")
		}
		visited[endpoint] = true
		var payload struct {
			Tags []string `json:"tags"`
		}
		headers, err := c.get(ctx, endpoint, token.Token, &payload)
		if err != nil {
			return nil, err
		}
		for _, tag := range payload.Tags {
			if strings.HasSuffix(tag, suffix) {
				v := strings.TrimSuffix(tag, suffix)
				if stableVersion.MatchString(v) {
					versions[v] = true
				}
			}
		}
		link := headers.Get("Link")
		if link == "" {
			break
		}
		match := nextRegistryLink.FindStringSubmatch(link)
		if len(match) != 2 {
			return nil, fmt.Errorf("unsupported registry pagination header")
		}
		base, _ := url.Parse(endpoint)
		reference, err := url.Parse(match[1])
		if err != nil {
			return nil, fmt.Errorf("invalid registry pagination")
		}
		next := base.ResolveReference(reference)
		if next.Scheme != "https" || next.Host != "registry-1.docker.io" || next.Path != "/v2/"+repository+"/tags/list" || next.User != nil || next.Fragment != "" {
			return nil, fmt.Errorf("registry pagination left the configured repository")
		}
		endpoint = next.String()
		if page == 99 {
			return nil, fmt.Errorf("registry tag catalog exceeds pagination limit")
		}
	}

	result := make([]string, 0, len(versions))
	for v := range versions {
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool { return versionGreater(result[i], result[j]) })
	c.mu.Lock()
	c.entries[runtime] = catalogEntry{append([]string{}, result...), time.Now().Add(10 * time.Minute)}
	c.mu.Unlock()
	return result, nil
}

var nextRegistryLink = regexp.MustCompile(`^\s*<([^>]+)>;\s*rel="?next"?\s*$`)

var stableVersion = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){0,2}$`)

func versionGreater(a, b string) bool {
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		av, bv := 0, 0
		if i < len(aa) {
			av, _ = strconv.Atoi(aa[i])
		}
		if i < len(bb) {
			bv, _ = strconv.Atoi(bb[i])
		}
		if av != bv {
			return av > bv
		}
	}
	return len(aa) > len(bb)
}
func (c *Catalog) get(ctx context.Context, endpoint, token string, target any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("runtime registry request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runtime registry returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 8*1024*1024 {
		return nil, fmt.Errorf("registry response exceeds limit")
	}
	if err = json.Unmarshal(data, target); err != nil {
		return nil, fmt.Errorf("invalid registry response")
	}
	return resp.Header, nil
}

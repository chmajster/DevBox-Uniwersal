package updater

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/updatebundle"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func signedReleaseInfo(parent context.Context, base string, key ed25519.PublicKey) (updatebundle.Manifest, error) {
	var empty updatebundle.Manifest
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return empty, fmt.Errorf("invalid signed release HTTPS URL")
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	client := http.Client{Timeout: 8 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || r.URL.Scheme != "https" || r.URL.User != nil {
			return fmt.Errorf("invalid release redirect")
		}
		return nil
	}}
	fetch := func(name string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/"+name, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("release metadata download failed")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("release metadata returned HTTP %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 16385))
		if err != nil || len(b) > 16384 {
			return nil, fmt.Errorf("invalid release metadata size")
		}
		return b, nil
	}
	manifest, err := fetch("devbox-manifest.json")
	if err != nil {
		return empty, err
	}
	encoded, err := fetch("devbox-manifest.sig")
	if err != nil {
		return empty, err
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		return empty, fmt.Errorf("invalid release signature encoding")
	}
	return updatebundle.VerifyManifest(manifest, signature, key)
}

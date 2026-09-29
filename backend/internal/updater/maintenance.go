package updater

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/updatebundle"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// Maintenance commands are used by the root-owned updater, not HTTP endpoints.
func Maintenance(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("maintenance command is required")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "verify-update":
		manifest := fs.String("manifest", "", "manifest path")
		signature := fs.String("signature", "", "signature path")
		key := fs.String("public-key", "", "trusted public key path")
		archive := fs.String("archive", "", "archive path")
		destination := fs.String("destination", "", "new extraction directory")
		minimum := fs.String("minimum-time", "", "earliest accepted signed release date")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		pub, err := updatebundle.PublicKey(*key)
		if err != nil {
			return err
		}
		m, err := updatebundle.ReadLimited(*manifest, 16<<10)
		if err != nil {
			return err
		}
		sigText, err := updatebundle.ReadLimited(*signature, 4096)
		if err != nil {
			return err
		}
		sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
		if err != nil {
			return fmt.Errorf("invalid release signature encoding")
		}
		archiveBytes, err := updatebundle.ReadLimited(*archive, updatebundle.MaxArchive)
		if err != nil {
			return err
		}
		verified, err := updatebundle.Verify(m, sig, archiveBytes, pub)
		if err != nil {
			return err
		}
		if *minimum != "" {
			oldest, err := time.Parse(time.RFC3339, *minimum)
			if err != nil {
				return fmt.Errorf("invalid minimum release date")
			}
			released, _ := time.Parse(time.RFC3339, verified.CreatedAt)
			if released.Before(oldest) {
				return fmt.Errorf("signed release is older than the last accepted release")
			}
		}
		if err = updatebundle.Extract(bytes.NewReader(archiveBytes), *destination); err != nil {
			return err
		}
		fmt.Println(verified.Version)
		fmt.Println(verified.CreatedAt)
		return nil
	case "update-snapshot", "update-restore":
		file := fs.String("file", "", "encrypted snapshot filename")
		paths := fs.String("paths", "", "JSON list of absolute paths to snapshot")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		raw, err := base64.StdEncoding.DecodeString(os.Getenv("DEVBOX_UPDATE_ROLLBACK_KEY"))
		if err != nil || len(raw) != 32 {
			return fmt.Errorf("rollback requires the existing 32-byte master key")
		}
		defer clear(raw)
		if args[0] == "update-restore" {
			return restoreUpdateSnapshot(*file, raw)
		}
		var roots []string
		if *paths == "" {
			roots = fs.Args()
		} else if err = json.Unmarshal([]byte(*paths), &roots); err != nil {
			return fmt.Errorf("invalid snapshot path list")
		}
		return createUpdateSnapshot(*file, roots, raw)
	case "update-check-idle":
		database := fs.String("database", "", "SQLite path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return checkUpdateIdle(*database)
	case "update-health":
		address := fs.String("address", "127.0.0.1:8787", "local API address")
		version := fs.String("version", "", "expected version")
		timeout := fs.Int("timeout", 45, "timeout seconds")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return verifyUpdateHealth(*address, *version, *timeout)
	default:
		return fmt.Errorf("unsupported maintenance command")
	}
}
func verifyUpdateHealth(address, version string, seconds int) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid healthcheck bind address")
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("update healthcheck requires a local API address")
	}
	if seconds < 1 || seconds > 120 {
		return fmt.Errorf("health timeout must be 1..120 seconds")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	defer cancel()
	client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/api/v1/health", nil)
		resp, err := client.Do(req)
		if err == nil {
			var result struct {
				Data struct {
					Status   string `json:"status"`
					Database string `json:"database"`
					Version  string `json:"version"`
				} `json:"data"`
			}
			decode := json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&result)
			_ = resp.Body.Close()
			if resp.StatusCode == 200 && decode == nil && result.Data.Status == "ok" && result.Data.Database == "ok" && result.Data.Version == version {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("new API did not report a healthy database and the expected installed version")
		case <-time.After(500 * time.Millisecond):
		}
	}
}

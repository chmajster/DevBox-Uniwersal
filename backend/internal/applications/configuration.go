package applications

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
)

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Configuration contains only non-secret data. Runtime secrets belong in the
// encrypted SecretStore or in a source-owned Compose env_file, not snapshots.
func validateConfiguration(config map[string]any) error {
	if err := rejectPlaintextSecrets(config); err != nil {
		return err
	}
	for key, value := range config {
		switch key {
		case "container_port", "host_port":
			var number float64
			switch n := value.(type) {
			case float64:
				number = n
			case int:
				number = float64(n)
			case json.Number:
				var err error
				number, err = n.Float64()
				if err != nil {
					return fmt.Errorf("%w: %s must be numeric", ErrInvalidInput, key)
				}
			default:
				return fmt.Errorf("%w: %s must be an integer", ErrInvalidInput, key)
			}
			if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < 0 || number > 65535 {
				return fmt.Errorf("%w: %s must be an integer from 0 to 65535", ErrInvalidInput, key)
			}
		case "protocol":
			v, ok := value.(string)
			if !ok || (v != "http" && v != "https" && v != "tcp") {
				return fmt.Errorf("%w: protocol must be http, https or tcp", ErrInvalidInput)
			}
		case "health_path":
			v, ok := value.(string)
			if !ok || (v != "" && (!strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || strings.ContainsAny(v, "\r\n"))) {
				return fmt.Errorf("%w: health_path must be an absolute URL path", ErrInvalidInput)
			}
		case "runtime", "runtime_version", "compose_service", "restart_policy":
			v, ok := value.(string)
			if !ok || strings.ContainsAny(v, "\x00\r\n") {
				return fmt.Errorf("%w: invalid %s", ErrInvalidInput, key)
			}
			if key == "restart_policy" && v != "" && v != "no" && v != "always" && v != "unless-stopped" && v != "on-failure" {
				return fmt.Errorf("%w: invalid restart_policy", ErrInvalidInput)
			}
		case "environment":
			raw, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("%w: environment", ErrInvalidInput)
			}
			var env map[string]string
			if err := json.Unmarshal(raw, &env); err != nil {
				return fmt.Errorf("%w: environment must be a string map", ErrInvalidInput)
			}
			for name, v := range env {
				if !environmentName.MatchString(name) || strings.ContainsRune(v, '\x00') {
					return fmt.Errorf("%w: invalid environment variable name or value", ErrInvalidInput)
				}
			}
		case "modules", "command":
			raw, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("%w: %s", ErrInvalidInput, key)
			}
			var values []string
			if json.Unmarshal(raw, &values) != nil {
				return fmt.Errorf("%w: %s must be an array of strings", ErrInvalidInput, key)
			}
			for _, v := range values {
				if strings.ContainsRune(v, '\x00') {
					return fmt.Errorf("%w: invalid %s", ErrInvalidInput, key)
				}
			}
		default:
			return fmt.Errorf("%w: unsupported configuration field %q", ErrInvalidInput, key)
		}
	}
	return nil
}

func rejectPlaintextSecrets(value any) error {
	// Marshal first so programmatic and JSON callers receive identical validation.
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("%w: configuration is not JSON serializable", ErrInvalidInput)
	}
	var normalized any
	if json.Unmarshal(raw, &normalized) != nil {
		return fmt.Errorf("%w: invalid configuration", ErrInvalidInput)
	}
	var walk func(any) error
	walk = func(node any) error {
		switch x := node.(type) {
		case map[string]any:
			for key, v := range x {
				if secretKeyPattern.MatchString(key) && v != nil && fmt.Sprint(v) != "" {
					return fmt.Errorf("%w: plaintext secret-like field %q is forbidden; use encrypted secrets", ErrInvalidInput, key)
				}
				if err := walk(v); err != nil {
					return err
				}
			}
		case []any:
			for _, v := range x {
				if err := walk(v); err != nil {
					return err
				}
			}
		case string:
			if u, err := url.Parse(x); err == nil && u.User != nil {
				if _, set := u.User.Password(); set {
					return fmt.Errorf("%w: credentials in URLs are forbidden", ErrInvalidInput)
				}
			}
		}
		return nil
	}
	return walk(normalized)
}

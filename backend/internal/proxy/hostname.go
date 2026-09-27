package proxy

import (
	"fmt"
	"net"
	"strings"
)

func NormalizeHostname(value string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(value))
	host = strings.TrimSuffix(host, ".")
	if host == "" || len(host) > 253 {
		return "", fmt.Errorf("%w: hostname length is invalid", ErrInvalidInput)
	}
	if net.ParseIP(host) != nil {
		return "", fmt.Errorf("%w: hostname cannot be an IP address", ErrInvalidInput)
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("%w: hostname must contain at least two labels", ErrInvalidInput)
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return "", fmt.Errorf("%w: hostname label length is invalid", ErrInvalidInput)
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("%w: hostname labels cannot start or end with '-'", ErrInvalidInput)
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return "", fmt.Errorf("%w: hostname contains an invalid character", ErrInvalidInput)
		}
	}
	return host, nil
}

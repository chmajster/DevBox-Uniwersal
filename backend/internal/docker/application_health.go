package docker

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (p *CLIProvider) CheckApplicationHTTP(ctx context.Context, port int, protocol, path string) error {
	if port < 1 || port > 65535 || (protocol != "http" && protocol != "https") {
		return ErrInvalidInput
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\r\n") {
		return ErrInvalidInput
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, protocol+"://127.0.0.1:"+strconv.Itoa(port)+path, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("HTTP readiness: %w", err)
	}
	response.Body.Close()
	if response.StatusCode >= 500 {
		return fmt.Errorf("HTTP readiness returned %d", response.StatusCode)
	}
	return nil
}

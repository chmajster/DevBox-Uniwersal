package proxy

import "testing"

func TestNormalizeHostname(t *testing.T) {
	got, err := NormalizeHostname(" CloudPortal.DevBox.Local. ")
	if err != nil {
		t.Fatalf("NormalizeHostname() error = %v", err)
	}
	if got != "cloudportal.devbox.local" {
		t.Fatalf("NormalizeHostname() = %q", got)
	}

	invalid := []string{
		"localhost",
		"127.0.0.1",
		"-bad.devbox.local",
		"bad-.devbox.local",
		"bad_name.devbox.local",
		"bad..devbox.local",
	}
	for _, value := range invalid {
		if _, err := NormalizeHostname(value); err == nil {
			t.Fatalf("NormalizeHostname(%q) expected error", value)
		}
	}
}

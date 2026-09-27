package system

import "testing"

func TestReportHealthy(t *testing.T) {
	if !reportHealthy([]CheckResult{{Status: CheckOK}, {Status: CheckWarn}}) {
		t.Fatal("warnings should not make report unhealthy")
	}
	if reportHealthy([]CheckResult{{Status: CheckOK}, {Status: CheckFail}}) {
		t.Fatal("failures must make report unhealthy")
	}
}

func TestDatabaseDataDir(t *testing.T) {
	if got := DatabaseDataDir("/var/lib/devbox/devbox.db"); got != "/var/lib/devbox" {
		t.Fatalf("unexpected dir %q", got)
	}
}

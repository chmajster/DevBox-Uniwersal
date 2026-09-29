package plugins

import "testing"

func TestParsePostgreSQLClusters(t *testing.T) {
	items := parsePostgreSQLClusters("16 main 5432 online postgres /var/lib/postgresql/16/main /var/log/postgresql/postgresql-16-main.log\n15 legacy 5544 down postgres /var/lib/postgresql/15/legacy /var/log/postgresql/postgresql-15-legacy.log\n", "psql (PostgreSQL) 16.4")
	if len(items) != 2 {
		t.Fatalf("expected 2 clusters, got %d: %#v", len(items), items)
	}
	if items[0].Engine != "postgresql" || items[0].Port != 5432 || !items[0].Running {
		t.Fatalf("unexpected first cluster: %#v", items[0])
	}
	if items[0].Host != "host.docker.internal" || items[0].Label != "PostgreSQL 16 / main" {
		t.Fatalf("unexpected first cluster endpoint: %#v", items[0])
	}
	if items[1].Port != 5544 {
		t.Fatalf("custom PostgreSQL port was not preserved: %#v", items[1])
	}
}

func TestParsePostgreSQLClustersRejectsInvalidPort(t *testing.T) {
	items := parsePostgreSQLClusters("16 main invalid online postgres /data /log\n16 other 70000 online postgres /data /log\n", "psql")
	if len(items) != 0 {
		t.Fatalf("expected invalid clusters to be ignored, got %#v", items)
	}
}

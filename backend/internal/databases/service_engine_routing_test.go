package databases

import "testing"

func TestDatabaseEngineOverrideKeepsPrimaryProviderSeparate(t *testing.T) {
	primary := &MySQLProvider{}
	managed := &MySQLProvider{}
	service := &Service{
		engine: primary,
		engines: map[string]databaseEngine{
			"mysql":   primary,
			"mariadb": primary,
		},
	}

	WithDatabaseEngine("mysql", managed)(service)
	WithDatabaseEngine("mariadb", managed)(service)

	mysqlEngine, err := service.engineFor("mysql")
	if err != nil {
		t.Fatal(err)
	}
	if mysqlEngine != managed {
		t.Fatal("database console MySQL engine did not use managed plugin provider")
	}

	mariaEngine, err := service.engineFor("mariadb")
	if err != nil {
		t.Fatal(err)
	}
	if mariaEngine != managed {
		t.Fatal("database console MariaDB engine did not use managed plugin provider")
	}

	if service.engine != primary {
		t.Fatal("primary project provisioning provider must remain unchanged")
	}
}

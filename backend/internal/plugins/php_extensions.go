package plugins

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

type PHPExtension struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Package     string   `json:"package"`
	Installed   bool     `json:"installed"`
	Modules     []string `json:"modules,omitempty"`
}

type phpExtensionDefinition struct {
	ID          string
	Name        string
	Description string
	Category    string
	HelperKey   string
	Package     string
	Modules     []string
}

var phpExtensionCatalog = []phpExtensionDefinition{
	{ID: "curl", Name: "cURL", Description: "HTTP/HTTPS, REST API i pobieranie danych z sieci.", Category: "Sieć", HelperKey: "php-ext-curl", Package: "php-curl", Modules: []string{"curl"}},
	{ID: "mbstring", Name: "Mbstring", Description: "Obsługa wielobajtowych kodowań znaków, wymagana przez wiele frameworków.", Category: "Podstawowe", HelperKey: "php-ext-mbstring", Package: "php-mbstring", Modules: []string{"mbstring"}},
	{ID: "xml", Name: "XML", Description: "DOM, SimpleXML, XMLReader/XMLWriter i obsługa dokumentów XML.", Category: "Podstawowe", HelperKey: "php-ext-xml", Package: "php-xml", Modules: []string{"xml", "dom", "simplexml"}},
	{ID: "zip", Name: "ZIP", Description: "Obsługa archiwów ZIP używana m.in. przez Composer i aplikacje webowe.", Category: "Pliki", HelperKey: "php-ext-zip", Package: "php-zip", Modules: []string{"zip"}},
	{ID: "gd", Name: "GD", Description: "Przetwarzanie obrazów PNG/JPEG/WebP i generowanie grafik.", Category: "Multimedia", HelperKey: "php-ext-gd", Package: "php-gd", Modules: []string{"gd"}},
	{ID: "intl", Name: "Intl", Description: "ICU, lokalizacja, formatowanie dat, liczb i danych językowych.", Category: "Podstawowe", HelperKey: "php-ext-intl", Package: "php-intl", Modules: []string{"intl"}},
	{ID: "mysql", Name: "MySQL", Description: "mysqli i PDO MySQL dla MySQL/MariaDB.", Category: "Bazy danych", HelperKey: "php-ext-mysql", Package: "php-mysql", Modules: []string{"mysqli", "pdo_mysql"}},
	{ID: "pgsql", Name: "PostgreSQL", Description: "Sterownik PostgreSQL i PDO PostgreSQL.", Category: "Bazy danych", HelperKey: "php-ext-pgsql", Package: "php-pgsql", Modules: []string{"pgsql", "pdo_pgsql"}},
	{ID: "sqlite3", Name: "SQLite3", Description: "SQLite3 oraz PDO SQLite.", Category: "Bazy danych", HelperKey: "php-ext-sqlite3", Package: "php-sqlite3", Modules: []string{"sqlite3", "pdo_sqlite"}},
	{ID: "bcmath", Name: "BCMath", Description: "Precyzyjna arytmetyka dziesiętna dla finansów i dużych liczb.", Category: "Matematyka", HelperKey: "php-ext-bcmath", Package: "php-bcmath", Modules: []string{"bcmath"}},
	{ID: "soap", Name: "SOAP", Description: "Klient i serwer SOAP dla starszych integracji enterprise.", Category: "Integracje", HelperKey: "php-ext-soap", Package: "php-soap", Modules: []string{"soap"}},
	{ID: "ldap", Name: "LDAP", Description: "Integracja z LDAP i Active Directory.", Category: "Integracje", HelperKey: "php-ext-ldap", Package: "php-ldap", Modules: []string{"ldap"}},
	{ID: "gmp", Name: "GMP", Description: "Arytmetyka dużych liczb i operacje kryptograficzne.", Category: "Matematyka", HelperKey: "php-ext-gmp", Package: "php-gmp", Modules: []string{"gmp"}},
	{ID: "imagick", Name: "Imagick", Description: "Zaawansowane przetwarzanie obrazów przez ImageMagick.", Category: "Multimedia", HelperKey: "php-ext-imagick", Package: "php-imagick", Modules: []string{"imagick"}},
	{ID: "redis", Name: "Redis", Description: "Natywny klient Redis dla cache, sesji i kolejek.", Category: "Cache", HelperKey: "php-ext-redis", Package: "php-redis", Modules: []string{"redis"}},
	{ID: "memcached", Name: "Memcached", Description: "Natywny klient Memcached.", Category: "Cache", HelperKey: "php-ext-memcached", Package: "php-memcached", Modules: []string{"memcached"}},
	{ID: "opcache", Name: "OPcache", Description: "Cache skompilowanego bytecode PHP poprawiający wydajność.", Category: "Wydajność", HelperKey: "php-ext-opcache", Package: "php-opcache", Modules: []string{"zend opcache", "opcache"}},
	{ID: "xdebug", Name: "Xdebug", Description: "Debugger, profiler i rozszerzone informacje diagnostyczne dla developmentu.", Category: "Development", HelperKey: "php-ext-xdebug", Package: "php-xdebug", Modules: []string{"xdebug"}},
}

func (s *Service) PHPExtensions(ctx context.Context) ([]PHPExtension, error) {
	installed, err := loadedPHPModules(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]PHPExtension, 0, len(phpExtensionCatalog))
	for _, def := range phpExtensionCatalog {
		item := PHPExtension{
			ID: def.ID, Name: def.Name, Description: def.Description, Category: def.Category,
			Package: def.Package, Modules: def.Modules,
		}
		for _, module := range def.Modules {
			if installed[strings.ToLower(module)] {
				item.Installed = true
				break
			}
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Category == items[j].Category {
			return items[i].Name < items[j].Name
		}
		return items[i].Category < items[j].Category
	})
	return items, nil
}

func (s *Service) InstallPHPExtensions(ctx context.Context, ids []string) ([]PHPExtension, error) {
	if s.helperBinary == "" {
		return nil, errors.New("privileged helper is not configured")
	}
	if len(ids) == 0 {
		return nil, errors.New("select at least one PHP extension")
	}
	definitions := make(map[string]phpExtensionDefinition, len(phpExtensionCatalog))
	for _, def := range phpExtensionCatalog {
		definitions[def.ID] = def
	}

	seen := map[string]bool{}
	for _, rawID := range ids {
		id := strings.ToLower(strings.TrimSpace(rawID))
		if id == "" || seen[id] {
			continue
		}
		def, ok := definitions[id]
		if !ok {
			return nil, fmt.Errorf("unsupported PHP extension: %s", id)
		}
		seen[id] = true
		if err := s.installPackage(ctx, def.HelperKey); err != nil {
			return nil, fmt.Errorf("install %s (%s): %w", def.Name, def.Package, err)
		}
	}
	return s.PHPExtensions(ctx)
}

func (s *Service) installPackage(ctx context.Context, helperKey string) error {
	sudo := s.sudoBinary
	if sudo == "" {
		sudo = "sudo"
	}
	cmd := exec.CommandContext(ctx, sudo, s.helperBinary, "install-package", helperKey)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return errors.New(message)
	}
	return nil
}

func loadedPHPModules(ctx context.Context) (map[string]bool, error) {
	php, err := exec.LookPath("php")
	if err != nil {
		return nil, errors.New("PHP CLI is not installed")
	}
	out, err := exec.CommandContext(ctx, php, "-m").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("query PHP modules: %s", strings.TrimSpace(string(out)))
	}
	result := make(map[string]bool)
	for _, line := range strings.Split(strings.ToLower(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		result[line] = true
	}
	return result, nil
}

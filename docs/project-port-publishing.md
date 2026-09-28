# Porty kontenera i dostęp do aplikacji

W konfiguracji runtime projektu znajduje się sekcja **Porty Docker i dostęp do aplikacji**. Wybierz port HTTP wewnątrz kontenera oraz port publikowany na hoście. Puste pole portu wewnętrznego oznacza automatyczne wykrycie z Dockerfile lub obrazu generowanego przez DevBox.

Nowe aplikacje zarządzane przez DevBox zaczynają od portu hosta **8080**. Dodatkowe mapowanie HTTPS, jeśli je włączysz, zaczyna od **8443** i wskazuje domyślnie port kontenera **443**. Na przykład kontener nasłuchujący na 80 może być dostępny przez `http://HOST:8080`, a jego skonfigurowany serwer TLS na 443 przez `https://HOST:8443`.

Jeżeli 8080 jest zajęte, allocator sprawdza 8081, 8082 itd. Uwzględnia rezerwacje innych projektów, gniazda IPv4/IPv6 i równoległe wdrożenia DevBox. Po udanym wdrożeniu rzeczywiście przydzielony numer zostaje zapisany jako port projektu. Zwykły redeploy/restart nie zmienia go tylko dlatego, że poprzednio zajęty port stał się wolny. Późniejsze jawne ustawienie innego numeru jest stosowane dopiero przy wdrożeniu.

**Zapisz porty** zapisuje konfigurację bez zatrzymywania aplikacji. **Zapisz i wdroż** dodatkowo uruchamia istniejący proces wdrożenia; nie wymusza przebudowy niezmienionego obrazu. Panel pokazuje ostatnio zastosowane mapowanie i odsyła do statusu/logów zadania. Nie zmieniaj konfiguracji podczas aktywnego wdrożenia — API zwraca wtedy 409.

## Port wewnętrzny

Dla obrazów generowanych DevBox dostosowuje znane polecenia serwerów PHP/Python, konfigurację nginx dla treści statycznych oraz zmienne PORT/APP_PORT. Własny kod Node.js/Go/Python musi respektować swój skonfigurowany port; DevBox nie przepisuje kodu aplikacji. Zmiana portu wewnętrznego może wymagać przebudowy obrazu. Zmiana samego portu hosta nie zmienia fingerprintu obrazu. Montowania kodu live pozostają zachowane.

Przy własnym Dockerfile wskazany port musi odpowiadać rzeczywistemu nasłuchiwaniu aplikacji. Sama instrukcja EXPOSE ani publikacja portu nie uruchamia serwera.

## HTTPS

Mapowanie HTTPS nie generuje certyfikatu i nie uruchamia TLS. Jest dostępne dla projektu z własnym Dockerfile lub Compose z serwerem HTTPS i certyfikatem. Generowane obrazy runtime dostarczają HTTP. Certyfikaty, nazwa hosta i konfiguracja aplikacji pozostają po stronie projektu. Gotowość publikacji aplikacji jest sprawdzana przez HTTP; dodanie mapowania HTTPS nie oznacza weryfikacji jej certyfikatu.

## Docker Compose

Zapisanie konfiguracji portów włącza zarządzanie publikowanymi portami wskazanej usługi. Można podać nazwę, np. `web`; przy jednoznacznej usłudze web DevBox wykryje ją automatycznie. Gdy portu kontenera nie można wykryć z definicji usługi, wpisz go jawnie. Pozostałe usługi nie są przepisywane. Publikowane porty wybranej usługi zostaną zastąpione wybranym HTTP i opcjonalnym HTTPS.

Ta opcja wymaga **Docker Compose 2.24.4 lub nowszego** z obsługą `!override`. Dotychczasowe projekty Compose, w których nie zapisano nowych ustawień, zachowują oryginalną konfigurację i zgodność z dotychczas obsługiwanym `docker-compose`. Wybrana usługa nie może używać host/container network mode ani wymagać profilu Compose.

Plik override jest przechowywany poza repozytorium aplikacji. Można wskazać trwały katalog zmienną `DEVBOX_COMPOSE_PORTS_DIR` w środowisku usługi DevBox. Musi być ścieżką bezwzględną, zapisywalną dla konta usługi. Domyślnie używany jest katalog konfiguracji tego konta, podkatalog `devbox/compose-ports`. Zachowuj go w kopii zapasowej razem z bazą DevBox; nie umieszczaj go w nietrwałym systemie plików kontenera panelu. Generowany plik nie zawiera interpolowanych zmiennych środowiskowych ani poświadczeń. Polecenia up/down/restart/ps/logs używają tego samego override.

## API

`GET /api/v1/projects/{id}/ports/config` jest dostępny co najmniej dla Viewer. `PUT` wymaga Operator/Admin oraz dotychczasowego uwierzytelnienia i ochrony CSRF. Przykładowa treść PUT dla aplikacji HTTP nasłuchującej na 80:

```json
{
  "container_port": 80,
  "host_port": 8080,
  "https_enabled": false,
  "https_container_port": 443,
  "https_host_port": 8443,
  "compose_service": ""
}
```

Odpowiedź w istniejącej kopercie `data` zawiera `settings`, `configured` i opcjonalne `applied` z rzeczywistymi portami. Stan `applied` jest zapisywany przez job wdrożenia, nie przez klienta. Nieznane pola, ułamkowe numery i wartości spoza zakresu 1–65535 są odrzucane; 0 jest dozwolone wyłącznie dla automatycznie wykrywanego portu HTTP kontenera.

## Dostęp spoza hosta

Publikowanie zachowuje standardową politykę wiązania interfejsów Dockera. Potrzebne mogą być dodatkowe reguły firewalla i przekierowanie routera/NAT lub WSL. DevBox nie zmienia automatycznie tych zabezpieczeń. Adresy w panelu wykorzystują nazwę hosta, przez którą otwarto DevBox; przy niestandardowym reverse proxy użyj rzeczywistego adresu hosta Dockera. Aplikacja powinna mieć odpowiednie uwierzytelnienie przed udostępnieniem publicznym.

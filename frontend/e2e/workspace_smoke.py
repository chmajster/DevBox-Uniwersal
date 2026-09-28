"""Browser smoke checks against the built UI with explicitly synthetic API fixtures.

Run from frontend after `npm ci && npm run build`:
  python -m pip install playwright==1.55.0
  python -m playwright install chromium
  python e2e/workspace_smoke.py
No production API, credentials or infrastructure are contacted.
"""
import json
import threading
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse

from playwright.sync_api import expect, sync_playwright

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "ui-artifacts"
BASE = "http://127.0.0.1:4173"


class SPAHandler(SimpleHTTPRequestHandler):
    def do_GET(self):
        path = urlparse(self.path).path
        if not Path(self.translate_path(path)).is_file():
            self.path = "/index.html"
        super().do_GET()

    def log_message(self, *_args):
        pass


def project(identifier, name, runtime, status, domain):
    return {
        "id": identifier, "name": name, "slug": identifier,
        "description": "Przykładowa aplikacja — dane testowe", "status": status,
        "source_type": "git", "runtime": runtime, "branch": "main", "port": 8080,
        "domain": domain, "deployment_mode": "native", "local_path": "/test/app",
        "working_directory": ".", "build_command": "", "start_command": "",
        "healthcheck": "", "auto_start": False, "current_commit": "a1b2c3d4e5f6",
        "created_at": "2026-01-01T10:00:00Z", "updated_at": "2026-01-01T10:00:00Z",
    }


def install_api(context, role="admin", authenticated=True):
    state = {
        "projects": [project("portal", "Portal zespołu", "PHP", "running", "portal.test"),
                     project("api", "API projektu", "Go", "running", "api.test"),
                     project("worker", "Worker", "Python", "stopped", "")],
        "fail_projects": False, "posts": [], "authenticated": authenticated,
    }
    user = {"id": "test-user", "username": "developer", "role": role, "active": True}
    snapshot = {
        "collected_at": "2026-01-01T10:00:00Z", "host_uptime_seconds": 193800,
        "cpu": {"available": True, "usage_percent": 24.6, "cores": 8},
        "memory": {"available": True, "usage_percent": 37.5, "used_bytes": 6 * 1024**3, "total_bytes": 16 * 1024**3, "free_bytes": 10 * 1024**3},
        "disk": {"available": True, "usage_percent": 42, "used_bytes": 210 * 1024**3, "total_bytes": 500 * 1024**3, "free_bytes": 290 * 1024**3, "path": "/"},
        "process": {"host_process_count": 148, "pid": 100, "goroutines": 18, "heap_allocated_bytes": 1024, "runtime_reserved_bytes": 2048, "uptime_seconds": 5000},
    }
    responses = {
        "/monitoring/snapshot": snapshot, "/docker/containers": [{"id": str(index)} for index in range(6)],
        "/databases": [{"id": "db1"}, {"id": "db2"}], "/ports": [{"port": 8080}, {"port": 8081}],
        "/health": {"status": "ok"}, "/docker/status": {"available": True, "server_version": "Docker Engine — test"},
        "/mysql/status": {"running": True, "version": "MySQL — test"},
        "/proxy/status": {"detected": True, "config_valid": True, "version": "Nginx — test"},
    }

    def route_api(route):
        endpoint = urlparse(route.request.url).path.removeprefix("/api/v1")
        method = route.request.method
        if endpoint == "/auth/me":
            if not state["authenticated"]:
                route.fulfill(status=401, json={"error": {"code": "unauthorized", "message": "Zaloguj się"}})
                return
            data = user
        elif endpoint == "/auth/login":
            state["authenticated"] = True
            data = user
        elif endpoint == "/projects" and state["fail_projects"]:
            route.fulfill(status=503, json={"error": {"code": "unavailable", "message": "Test: API projektów niedostępne"}})
            return
        elif endpoint == "/projects":
            data = state["projects"]
        elif method == "POST" and endpoint.endswith(("/deploy", "/archive")):
            assert route.request.headers.get("x-csrf-token") == "smoke-csrf", "Existing CSRF header must be preserved"
            state["posts"].append(endpoint)
            data = {"id": "job-test", "status": "queued", "type": "deploy", "created_at": "2026-01-01T10:00:00Z"}
        elif endpoint in responses:
            data = responses[endpoint]
        else:
            raise AssertionError(f"Unexpected API request in smoke test: {method} {endpoint}")
        route.fulfill(json={"data": data})

    context.route("**/api/v1/**", route_api)
    context.add_cookies([{"name": "devbox_csrf", "value": "smoke-csrf", "url": BASE}])
    return state


def no_overflow(page):
    assert page.evaluate("document.documentElement.scrollWidth <= window.innerWidth"), "Page overflows its viewport"


def main():
    if not (ROOT / "dist" / "index.html").is_file():
        raise SystemExit("Build the frontend first: npm ci && npm run build")
    OUTPUT.mkdir(exist_ok=True)
    server = ThreadingHTTPServer(("127.0.0.1", 4173), partial(SPAHandler, directory=str(ROOT / "dist")))
    threading.Thread(target=server.serve_forever, daemon=True).start()
    errors = []
    try:
        with sync_playwright() as playwright:
            browser = playwright.chromium.launch()
            context = browser.new_context(viewport={"width": 1440, "height": 1000}, color_scheme="light")
            state = install_api(context)
            page = context.new_page()
            page.on("pageerror", lambda error: errors.append(str(error)))
            page.goto(BASE)
            expect(page.get_by_role("heading", name="Przegląd", exact=True)).to_be_visible()
            expect(page.get_by_text("Ostatni odczyt:", exact=False)).to_be_visible()
            no_overflow(page)
            page.screenshot(path=str(OUTPUT / "dashboard-light.png"), full_page=True)
            page.get_by_role("button", name="Włącz ciemny motyw").click()
            expect(page.locator("html")).to_have_attribute("data-theme", "dark")
            page.reload()
            expect(page.locator("html")).to_have_attribute("data-theme", "dark")
            expect(page.get_by_text("Ostatni odczyt:", exact=False)).to_be_visible()
            page.screenshot(path=str(OUTPUT / "dashboard-dark.png"), full_page=True)
            page.get_by_role("button", name="Włącz jasny motyw").click()
            page.get_by_role("button", name="Zwiń menu").click()
            expect(page.locator(".app-shell")).to_have_class("app-shell is-collapsed")
            page.get_by_role("button", name="Rozwiń menu").click()

            page.keyboard.press("Control+k")
            search = page.get_by_role("textbox", name="Szukaj w nawigacji")
            expect(search).to_be_focused()
            search.fill("Aplikacje")
            search.press("Enter")
            expect(page.get_by_role("heading", name="Aplikacje", exact=True)).to_be_visible()
            expect(page.locator(".project-card")).to_have_count(3)
            page.screenshot(path=str(OUTPUT / "applications-light.png"), full_page=True)
            page.get_by_role("textbox", name="Szukaj aplikacji").fill("PYTHON")
            expect(page.locator(".project-card")).to_have_count(1)
            page.get_by_role("button", name="Wyczyść filtry", exact=True).click()
            page.get_by_role("combobox", name="Filtr statusu").select_option("RUNNING")
            expect(page.locator(".project-card")).to_have_count(2)
            page.get_by_role("combobox", name="Filtr statusu").select_option("")
            page.get_by_role("button", name="Widok tabeli").click()
            expect(page.get_by_role("table")).to_be_visible()
            page.reload()
            expect(page.get_by_role("table")).to_be_visible()
            page.get_by_role("button", name="Widok kafelków").click()
            page.get_by_role("button", name="Archiwizuj Portal zespołu", exact=True).click()
            expect(page.get_by_role("dialog", name="Archiwizować aplikację?")).to_be_visible()
            page.get_by_role("button", name="Anuluj", exact=True).click()
            assert not state["posts"], "Cancel must never mutate a project"
            page.get_by_role("button", name="Wdróż Portal zespołu", exact=True).click()
            expect(page.get_by_text("Wdrożenie „Portal zespołu” dodano do kolejki zadań.", exact=False)).to_be_visible()
            assert state["posts"] == ["/projects/portal/deploy"]

            for width in (390, 320):
                page.set_viewport_size({"width": width, "height": 844})
                no_overflow(page)
                page.get_by_role("button", name="Otwórz menu").click()
                drawer = page.get_by_role("dialog", name="Menu DevBox")
                expect(drawer).to_be_visible()
                drawer.get_by_role("link", name="Przegląd", exact=True).click()
                expect(drawer).not_to_be_visible()
                expect(page.get_by_text("Ostatni odczyt:", exact=False)).to_be_visible()
                no_overflow(page)
                page.screenshot(path=str(OUTPUT / f"dashboard-mobile-{width}.png"), full_page=True)
                page.goto(BASE + "/apps")
                expect(page.locator(".project-card")).to_have_count(3)
            state["fail_projects"] = True
            page.goto(BASE)
            expect(page.get_by_text("Aplikacje: Test: API projektów niedostępne", exact=True)).to_be_visible()
            expect(page.locator(".overview-kpi").first.locator("strong")).to_have_text("—")
            state["fail_projects"] = False
            state["projects"] = None
            page.goto(BASE + "/apps")
            expect(page.get_by_role("heading", name="Miejsce na Twoją pierwszą aplikację")).to_be_visible()
            context.close()

            viewer = browser.new_context(viewport={"width": 1280, "height": 900}, color_scheme="light")
            install_api(viewer, role="viewer")
            viewer.add_init_script("Storage.prototype.getItem = () => { throw new Error('blocked'); }; Storage.prototype.setItem = () => { throw new Error('blocked'); };")
            view_page = viewer.new_page()
            view_page.on("pageerror", lambda error: errors.append(str(error)))
            view_page.goto(BASE + "/apps")
            expect(view_page.locator(".project-card")).to_have_count(3)
            expect(view_page.get_by_role("link", name="Dodaj aplikację")).to_have_count(0)
            expect(view_page.get_by_role("button", name="Wdróż Portal zespołu")).to_have_count(0)
            expect(view_page.get_by_role("link", name="Kopie zapasowe", exact=True)).to_have_count(0)
            expect(view_page.get_by_role("link", name="Audyt", exact=True)).to_have_count(0)
            view_page.get_by_role("button", name="Włącz ciemny motyw").click()
            expect(view_page.locator("html")).to_have_attribute("data-theme", "dark")
            view_page.keyboard.press("Control+k")
            view_page.get_by_role("textbox", name="Szukaj w nawigacji").fill("Audyt")
            expect(view_page.get_by_text("Brak pasujących widoków. Spróbuj innej nazwy.")).to_be_visible()
            view_page.keyboard.press("Escape")
            expect(view_page.get_by_role("dialog")).to_have_count(0)
            viewer.close()

            login = browser.new_context(viewport={"width": 1440, "height": 960}, color_scheme="light")
            install_api(login, authenticated=False)
            login_page = login.new_page()
            login_page.on("pageerror", lambda error: errors.append(str(error)))
            login_page.goto(BASE + "/login")
            expect(login_page.get_by_role("heading", name="Zaloguj się do DevBox")).to_be_visible()
            login_page.screenshot(path=str(OUTPUT / "login-light.png"), full_page=True)
            login_page.get_by_label("Nazwa użytkownika").fill("developer")
            login_page.locator('input[autocomplete="current-password"]').fill("synthetic-test-password")
            login_page.get_by_role("button", name="Pokaż hasło").click()
            expect(login_page.locator('input[autocomplete="current-password"]')).to_have_attribute("type", "text")
            login_page.get_by_role("button", name="Zaloguj się", exact=True).click()
            expect(login_page.get_by_role("heading", name="Przegląd", exact=True)).to_be_visible()
            login.close()
            browser.close()
        assert not errors, f"Uncaught browser errors: {errors}"
        report = {"status": "passed", "fixtures": "Synthetic API responses; no real infrastructure", "checks": ["dashboard", "themes/persistence", "sidebar", "keyboard search", "project filters", "table persistence", "archive cancellation", "deployment and CSRF", "mobile 390/320px", "API errors/null collections", "viewer visibility", "blocked storage", "login"]}
        (OUTPUT / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print(json.dumps(report, ensure_ascii=False))
    finally:
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    main()

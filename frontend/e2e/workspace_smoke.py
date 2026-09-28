"""Selected control-room UI: real production bundle, synthetic API fixtures only.
Run in frontend after npm ci && npm run build, using Python Playwright.
No real credentials, Docker mutations, databases or managed hosts are contacted.
"""
import json
import re
import threading
import time
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse
from playwright.sync_api import expect, sync_playwright

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'ui-artifacts'
BASE = 'http://127.0.0.1:4173'

class SPAHandler(SimpleHTTPRequestHandler):
    def do_GET(self):
        if not Path(self.translate_path(urlparse(self.path).path)).is_file():
            self.path = '/index.html'
        super().do_GET()
    def log_message(self, *_args):
        pass

def project(identifier, name, runtime, status, domain):
    return dict(id=identifier, name=name, slug=identifier, description='Aplikacja testowa',
                status=status, source_type='git', runtime=runtime, branch='main', port=8080,
                domain=domain, runtime_version='', container_policy='auto', local_path='/test/app', working_directory='.',
                build_command='', start_command='', healthcheck='', auto_start=False,
                current_commit='a1b2c3d4e5', created_at='2026-09-28T08:00:00Z', updated_at='2026-09-28T08:00:00Z')

def install_api(context, role='admin', authenticated=True):
    state = dict(authenticated=authenticated, posts=[], calls=[], failures=set(), tick=0, deployment_stage='PREPARING',
                 projects=[project('portal', 'Portal zespołu', 'PHP', 'running', 'portal.test'),
                           project('api', 'API projektu', 'Go', 'running', 'api.test'),
                           project('worker', 'Worker', 'Python', 'stopped', '')])
    user = dict(id='test-user', username='developer', role=role, active=True)
    jobs = [dict(id='job-482', type='Kopia zapasowa bazy danych', status='queued', created_at='2026-09-28T08:41:00Z'),
            dict(id='job-481', type='Budowa obrazu Docker', status='succeeded', created_at='2026-09-28T08:12:00Z'),
            dict(id='job-480', type='Aktualizacja aplikacji', status='succeeded', created_at='2026-09-28T08:03:00Z'),
            dict(id='job-479', type='Czyszczenie logów', status='failed', created_at='2026-09-28T07:03:00Z'),
            dict(id='job-478', type='Kopia zapasowa plików', status='succeeded', created_at='2026-09-28T06:03:00Z')]
    logs = [dict(id=f'log-{i}', cursor=i, source=source, level=level, message=message, created_at=f'2026-09-28T08:42:{i:02d}Z') for i, (source, level, message) in enumerate([
        ('devbox', 'info', 'GET /api/health 200'), ('docker', 'info', 'Container api started'),
        ('job', 'info', 'Backup job queued'), ('devbox', 'warn', 'Slow query detected (320ms)'),
        ('project', 'error', 'Worker process exited with code 1'), ('docker', 'info', 'Image pulled: php:8.2-fpm'),
        ('devbox', 'info', 'System health check completed'), ('job', 'info', 'Deployment completed')])]
    responses = {'/docker/containers': [dict(id=str(i), state='running') for i in range(6)],
                 '/databases': [dict(id='db1'), dict(id='db2')], '/ports': [dict(port=8080), dict(port=8081)],
                 '/health': {'status': 'ok'}, '/docker/status': {'available': True, 'server_version': 'Docker Engine'},
                 '/mysql/status': {'running': True, 'version': 'MySQL'},
                 '/proxy/status': {'detected': True, 'config_valid': True, 'version': 'Nginx'},
                 '/system/info': dict(hostname='devbox-test', version='test-build', os='linux', arch='amd64'),
                 '/logs/sources': ['all', 'devbox', 'docker', 'job', 'project'], '/jobs': jobs,
                 '/jobs/job-481/logs': []}
    epoch = time.time()
    def route_api(route):
        parsed = urlparse(route.request.url)
        endpoint = parsed.path.removeprefix('/api/v1')
        method = route.request.method
        state['calls'].append((method, endpoint, parsed.query))
        if endpoint in state['failures']:
            route.fulfill(status=503, json={'error': {'code': 'unavailable', 'message': 'Test: API niedostępne'}})
            return
        if endpoint == '/auth/me':
            if not state['authenticated']:
                route.fulfill(status=401, json={'error': {'code': 'unauthorized', 'message': 'Zaloguj się'}})
                return
            data = user
        elif endpoint == '/auth/login':
            state['authenticated'] = True
            data = user
        elif endpoint == '/auth/logout':
            state['authenticated'] = False
            data = {'status': 'ok'}
        elif endpoint == '/monitoring/snapshot':
            from datetime import datetime, timezone
            state['tick'] += 1
            data = dict(collected_at=datetime.fromtimestamp(epoch + state['tick'] * 10, timezone.utc).isoformat(), host_uptime_seconds=193800,
                        cpu=dict(available=True, usage_percent=20 + state['tick'] % 12, cores=8),
                        memory=dict(available=True, usage_percent=37.5, used_bytes=6*1024**3, total_bytes=16*1024**3, free_bytes=10*1024**3),
                        disk=dict(available=True, usage_percent=42, used_bytes=210*1024**3, total_bytes=500*1024**3, free_bytes=290*1024**3, path='/'),
                        process=dict(host_process_count=148, pid=100, goroutines=18, heap_allocated_bytes=1024, runtime_reserved_bytes=2048, uptime_seconds=5000))
        elif endpoint == '/projects':
            data = state['projects']
        elif method == 'GET' and endpoint == '/projects/portal':
            data = state['projects'][0]
        elif method == 'GET' and endpoint == '/projects/portal/deployments':
            data = [dict(id='deployment-test', project_id='portal', job_id='job-test',
                         status=state['deployment_stage'], stage=state['deployment_stage'],
                         commit_before='a1b2c3d4e5', commit_after='', duration_ms=0,
                         started_at='2026-09-28T08:00:00Z', created_at='2026-09-28T08:00:00Z')]
        elif endpoint == '/logs':
            source = parse_qs(parsed.query).get('source', ['all'])[0]
            data = [entry for entry in logs if source == 'all' or entry['source'] == source]
        elif method == 'POST' and endpoint.endswith(('/deploy', '/archive')):
            assert route.request.headers.get('x-csrf-token') == 'smoke-csrf'
            state['posts'].append(endpoint)
            data = {'id': 'job-test', 'status': 'queued', 'type': 'deploy', 'created_at': '2026-09-28T08:00:00Z'}
        elif endpoint in responses:
            data = responses[endpoint]
        else:
            raise AssertionError(f'Unexpected request: {method} {endpoint}')
        route.fulfill(json={'data': data})
    context.route('**/api/v1/**', route_api)
    context.add_cookies([{'name': 'devbox_csrf', 'value': 'smoke-csrf', 'url': BASE}])
    return state

def no_overflow(page):
    if not page.evaluate('document.documentElement.scrollWidth <= window.innerWidth'):
        debug = page.evaluate("""() => ({
            innerWidth: window.innerWidth,
            scrollWidth: document.documentElement.scrollWidth,
            offenders: [...document.querySelectorAll('body *')].map((el) => {
                const rect = el.getBoundingClientRect()
                const style = getComputedStyle(el)
                return { tag: el.tagName, className: String(el.className || ''), text: (el.textContent || '').trim().slice(0, 100), left: rect.left, right: rect.right, width: rect.width, whiteSpace: style.whiteSpace }
            }).filter((item) => item.right > window.innerWidth + 0.5 || item.left < -0.5).sort((a, b) => b.right - a.right).slice(0, 20)
        })""")
        print('OVERFLOW_DEBUG=' + json.dumps(debug, ensure_ascii=False))
    assert page.evaluate('document.documentElement.scrollWidth <= window.innerWidth'), 'Horizontal page overflow'

def main():
    if not (ROOT / 'dist' / 'index.html').exists():
        raise SystemExit('Build the frontend first: npm ci && npm run build')
    OUT.mkdir(exist_ok=True)
    server = ThreadingHTTPServer(('127.0.0.1', 4173), partial(SPAHandler, directory=str(ROOT / 'dist')))
    threading.Thread(target=server.serve_forever, daemon=True).start()
    errors = []
    try:
        with sync_playwright() as p:
            browser = p.chromium.launch()
            context = browser.new_context(viewport={'width': 1440, 'height': 1086}, color_scheme='light')
            state = install_api(context)
            page = context.new_page()
            page.on('pageerror', lambda error: errors.append(str(error)))
            # Install before the application registers any timer; otherwise those
            # native timers cannot be advanced by the Playwright clock.
            page.clock.install()
            page.goto(BASE)
            expect(page.locator('html')).to_have_attribute('data-theme', 'dark')
            expect(page.get_by_role('heading', name='Przegląd', exact=True)).to_be_visible()
            expect(page.get_by_text('System OK', exact=True)).to_be_visible()
            expect(page.locator('.console-kpi').nth(3).locator('strong')).to_have_text('1')
            expect(page.locator('.console-log-line')).to_have_count(8)
            expect(page.locator('.gauge-value')).to_have_count(3)
            expect(page.get_by_text('Temperatura CPU', exact=True).locator('..').get_by_text('Brak danych')).to_be_visible()
            no_overflow(page)
            page.screenshot(path=str(OUT / 'control-room-first-read.png'), full_page=True)
            for _ in range(4):
                with page.expect_response(lambda response: '/monitoring/snapshot' in response.url):
                    page.clock.fast_forward(11000)
                expect(page.get_by_role('button', name='Odśwież', exact=True)).to_be_enabled()
            expect(page.locator('.chart-line')).to_have_count(1)
            page.screenshot(path=str(OUT / 'control-room-dark.png'), full_page=True)
            page.get_by_role('button', name='Pamięć', exact=True).click()
            expect(page.locator('.metric-history')).to_have_class(re.compile('history-memory'))
            page.get_by_role('combobox', name='Zakres wykresu').select_option('10')
            expect(page.get_by_role('img', name=re.compile('Pamięć:.*10 minut'))).to_be_visible()
            page.get_by_role('button', name='Włącz jasny motyw').click()
            expect(page.locator('html')).to_have_attribute('data-theme', 'light')
            page.screenshot(path=str(OUT / 'control-room-light.png'), full_page=True)
            page.reload()
            expect(page.locator('html')).to_have_attribute('data-theme', 'light')
            page.get_by_role('button', name='Włącz ciemny motyw').click()
            page.get_by_role('button', name='Zwiń menu').click()
            expect(page.locator('.app-shell')).to_have_class(re.compile('is-collapsed'))
            page.get_by_role('button', name='Rozwiń menu').click()

            page.get_by_role('combobox', name='Źródło logów', exact=True).select_option('docker')
            expect(page.locator('.console-log-line')).to_have_count(2)
            page.get_by_role('combobox', name='Liczba linii').select_option('20')
            page.locator('.log-preview').get_by_role('link', name='Pełny widok').click()
            expect(page.get_by_role('heading', name='Logi', exact=True)).to_be_visible()
            expect(page.get_by_role('combobox', name='Źródło', exact=True)).to_have_value('docker')
            page.goto(BASE)
            page.locator('.console-kpi').nth(3).click()
            expect(page.get_by_role('combobox', name='Filtr zadań')).to_have_value('active')
            expect(page.locator('tbody tr')).to_have_count(1)
            page.goto(BASE + '/jobs?job=job-481')
            expect(page.locator('.job-details')).to_be_visible()

            page.keyboard.press('Control+k')
            search = page.get_by_role('textbox', name='Szukaj w nawigacji')
            expect(search).to_be_focused()
            search.fill('Aplikacje')
            search.press('Enter')
            expect(page.locator('.project-card')).to_have_count(3)
            page.screenshot(path=str(OUT / 'applications-dark.png'), full_page=True)
            page.get_by_role('textbox', name='Szukaj aplikacji').fill('PYTHON')
            expect(page.locator('.project-card')).to_have_count(1)
            page.get_by_role('button', name='Wyczyść filtry', exact=True).click()
            page.get_by_role('combobox', name='Filtr statusu').select_option('RUNNING')
            expect(page.locator('.project-card')).to_have_count(2)
            page.get_by_role('combobox', name='Filtr statusu').select_option('')
            page.get_by_role('button', name='Widok tabeli').click()
            expect(page.get_by_role('table')).to_be_visible()
            page.reload()
            expect(page.get_by_role('table')).to_be_visible()
            page.get_by_role('button', name='Widok kafelków').click()
            page.get_by_role('button', name='Archiwizuj Portal zespołu', exact=True).click()
            expect(page.get_by_role('dialog', name='Archiwizować aplikację?')).to_be_visible()
            page.get_by_role('button', name='Anuluj', exact=True).click()
            assert not state['posts']
            page.get_by_role('button', name='Wdróż Portal zespołu', exact=True).click()
            expect(page).to_have_url(re.compile(r'/apps/portal[?]tab=deployments$'))
            expect(page.get_by_text('AKTUALNY DEPLOYMENT', exact=True)).to_be_visible()
            expect(page.get_by_role('heading', name='Przygotowanie', exact=True)).to_be_visible()
            assert state['posts'] == ['/projects/portal/deploy']

            for width in (900, 390, 320):
                page.set_viewport_size({'width': width, 'height': 844})
                no_overflow(page)
                page.get_by_role('button', name='Otwórz menu').click()
                drawer = page.get_by_role('dialog', name='Menu DevBox')
                expect(drawer).to_be_visible()
                drawer.get_by_role('link', name='Przegląd', exact=True).click()
                expect(drawer).not_to_be_visible()
                expect(page.locator('.console-service')).to_have_count(4)
                no_overflow(page)
                page.screenshot(path=str(OUT / f'control-room-mobile-{width}.png'), full_page=True)
                page.goto(BASE + '/apps')
                expect(page.locator('.project-card')).to_have_count(3)
            state['failures'].update(['/projects', '/monitoring/snapshot', '/docker/status'])
            page.set_viewport_size({'width': 1440, 'height': 1086})
            page.goto(BASE)
            expect(page.locator('.console-kpi').first.locator('strong')).to_have_text('—')
            expect(page.locator('.gauge-value')).to_have_count(0)
            expect(page.get_by_text('Sprawdź usługi', exact=True)).to_be_visible()
            expect(page.locator('.console-services').get_by_text('NIEZNANY', exact=True)).to_be_visible()
            expect(page.get_by_text('Aplikacje: Test: API niedostępne', exact=True)).to_be_visible()
            page.screenshot(path=str(OUT / 'control-room-api-errors.png'), full_page=True)
            state['failures'].clear()
            state['projects'] = None
            page.goto(BASE + '/apps')
            expect(page.get_by_role('heading', name='Miejsce na Twoją pierwszą aplikację')).to_be_visible()
            context.close()

            viewer = browser.new_context(viewport={'width': 1280, 'height': 900})
            install_api(viewer, role='viewer')
            viewer.add_init_script("Storage.prototype.getItem = () => { throw new Error('blocked') }; Storage.prototype.setItem = () => { throw new Error('blocked') };")
            v = viewer.new_page()
            v.on('pageerror', lambda error: errors.append(str(error)))
            v.goto(BASE + '/apps')
            expect(v.locator('.project-card')).to_have_count(3)
            expect(v.get_by_role('link', name='Dodaj aplikację')).to_have_count(0)
            expect(v.get_by_role('button', name='Wdróż Portal zespołu')).to_have_count(0)
            expect(v.get_by_role('link', name='Kopie zapasowe', exact=True)).to_have_count(0)
            expect(v.get_by_role('link', name='Audyt', exact=True)).to_have_count(0)
            v.get_by_role('button', name='Włącz jasny motyw').click()
            expect(v.locator('html')).to_have_attribute('data-theme', 'light')
            v.keyboard.press('Control+k')
            v.get_by_role('textbox', name='Szukaj w nawigacji').fill('Audyt')
            expect(v.get_by_text('Brak pasujących widoków. Spróbuj innej nazwy.')).to_be_visible()
            v.keyboard.press('Escape')
            expect(v.get_by_role('dialog')).to_have_count(0)
            viewer.close()

            login = browser.new_context(viewport={'width': 1440, 'height': 960})
            install_api(login, authenticated=False)
            l = login.new_page()
            l.on('pageerror', lambda error: errors.append(str(error)))
            l.goto(BASE + '/login')
            expect(l.get_by_role('heading', name='Zaloguj się do DevBox')).to_be_visible()
            l.screenshot(path=str(OUT / 'login-dark.png'), full_page=True)
            l.get_by_label('Nazwa użytkownika').fill('developer')
            l.locator('input[autocomplete="current-password"]').fill('synthetic-password')
            l.get_by_role('button', name='Pokaż hasło').click()
            expect(l.locator('input[autocomplete="current-password"]')).to_have_attribute('type', 'text')
            l.get_by_role('button', name='Zaloguj się', exact=True).click()
            expect(l.get_by_role('heading', name='Przegląd', exact=True)).to_be_visible()
            login.close()
            browser.close()
        assert not errors, errors
        report = {'status': 'passed', 'fixtures': 'Synthetic API only; production bundle', 'checks': ['dark default/light persistence', 'real-source gauges', 'chart sample accumulation and metric/range switching', 'service failures', 'logs filters/deep link', 'job queue and details deep links', 'search', 'project CRUD UI and CSRF', '900/390/320px no overflow', 'API failures/null collections', 'viewer RBAC visibility', 'blocked storage', 'login']}
        (OUT / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        print(json.dumps(report, ensure_ascii=False))
    finally:
        server.shutdown()
        server.server_close()

if __name__ == '__main__':
    main()

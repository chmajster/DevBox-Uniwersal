"""Selected control-room UI: real production bundle, synthetic API fixtures only.
Run in frontend after npm ci && npm run build, using Python Playwright.
No real credentials, Docker mutations, databases or managed hosts are contacted.
"""
import json
import os
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

def application(identifier, name, runtime, status, port):
    workload = dict(id=identifier+'-web', name='web', role='web', primary=True,
                    image='nginx:alpine', driver_resource_id=identifier+'-container',
                    observed_state=status, health_state='unknown')
    endpoint = dict(id=identifier+'-http', name='primary', workload_id=workload['id'],
                    protocol='http', container_port=80, host_port=port,
                    primary=True, public=True, status=status)
    return dict(id=identifier, name=name, slug=identifier, description='Aplikacja testowa',
                status=status, source_type='git', driver='managed', runtime=dict(name=runtime),
                desired_state=status, observed_state=status, health_state='unknown',
                source=dict(repository_url='https://github.com/example/'+identifier, reference='main'),
                source_config=dict(container_port=80, deployment_mode='auto', runtime=runtime,
                                   runtime_version={'php': '8.4', 'python': '3.13', 'node': '22', 'go': '1.26', 'static': '1.28'}[runtime]), auto_start=False,
                workloads=[workload], endpoints=[endpoint], deployments=[],
                workload_count=1, primary_endpoint=endpoint,
                created_at='2026-09-28T08:00:00Z', updated_at='2026-09-28T08:00:00Z')

def install_api(context, role='admin', authenticated=True):
    state = dict(authenticated=authenticated, posts=[], calls=[], failures=set(), tick=0, deployment_stage='PREPARE', secrets=[],
                 applications=[application('portal', 'Portal zespołu', 'php', 'running', 8080),
                               application('api', 'API projektu', 'go', 'running', 8081),
                               application('worker', 'Worker', 'python', 'stopped', 8082)])
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
    responses = {'/docker/containers': [
                     dict(id='portal-container', name='devbox-app-portal', image='portal:test', state='running', status='Up', project_id='portal'),
                     dict(id='api-container', name='devbox-app-api', image='api:test', state='running', status='Up', project_id='api'),
                     dict(id='worker-container', name='devbox-app-worker', image='worker:test', state='exited', status='Exited (0)', project_id='worker'),
                     dict(id='mysql-container', name='devbox-mysql', image='mysql:8.4', state='running', status='Up'),
                     dict(id='proxy-container', name='devbox-proxy', image='nginx:latest', state='running', status='Up'),
                     dict(id='helper-container', name='devbox-helper', image='helper:test', state='running', status='Up'),
                 ],
                 '/projects': [],
                 '/databases': [dict(id='db1', name='one', engine='mysql', status='ready'), dict(id='db2', name='two', engine='postgresql', status='ready')],
                 '/database-users': [],
                 '/database-servers': [dict(engine=engine, container='devbox-'+engine, running=True, installed=True) for engine in ('mysql','mariadb','postgresql')],
                 '/runtimes/catalog': [dict(name=name, label=name, versions=[version], default_version=version, container_port=port) for name, version, port in [('php','8.4',8080),('python','3.13',8000),('node','22',3000),('go','1.26',8080),('static','1.28',8080)]],
                 '/runtimes/php/modules': [dict(name='pdo_mysql', label='pdo_mysql', description='SQL')],
                 '/runtimes/static/modules': [],
                 '/jobs/job-test/logs': [],
                 '/ports': [dict(port=8080), dict(port=8081)],
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
        elif endpoint == '/project-directories':
            root = '/opt/devbox/projects'
            selected = root + '/aplikacja'
            path = parse_qs(parsed.query).get('path', [''])[0]
            if method == 'POST':
                body = route.request.post_data_json
                assert route.request.headers.get('x-csrf-token') == 'smoke-csrf'
                data = dict(name=body['name'], path=body['parent'].rstrip('/') + '/' + body['name'])
            elif path == '':
                data = dict(path='', directories=[dict(name=root, path=root)])
            elif path == root:
                data = dict(path=root, directories=[dict(name='aplikacja', path=selected)])
            elif path == selected:
                data = dict(path=selected, parent=root, directories=[])
            else:
                route.fulfill(status=400, json={'error': {'code': 'invalid_request', 'message': 'Test: katalog nie istnieje'}})
                return
        elif endpoint == '/applications/detect':
            assert method == 'POST'
            assert route.request.headers.get('x-csrf-token') == 'smoke-csrf'
            data = dict(detection=dict(driver='managed', runtime='static', version='1.28', confidence='high', requires_configuration=False,
                                       services=[dict(name='app', suggested_role='web', primary=True)],
                                       endpoints=[dict(service='app', protocol='http', container_port=80, primary=True)]))
        elif endpoint == '/applications':
            if method == 'POST':
                assert route.request.headers.get('x-csrf-token') == 'smoke-csrf'
                body = route.request.post_data_json
                data = application('new-app', body['name'], 'static', 'stopped', 8090)
                data.update(source_type=body['source_type'], source=body['source'], source_config=body['configuration'])
                state['applications'].append(data)
                state['posts'].append(endpoint)
            else:
                data = state['applications']
        elif endpoint.startswith('/applications/'):
            parts = endpoint.split('/')
            app = next(item for item in state['applications'] if item['id'] == parts[2])
            suffix = '/'.join(parts[3:])
            if method in ('POST', 'PATCH', 'PUT', 'DELETE'):
                assert route.request.headers.get('x-csrf-token') == 'smoke-csrf'
                state['posts'].append(endpoint)
            if method == 'GET' and suffix == 'state':
                data = dict(application_id=app['id'], status=app['status'], observed_state=app['observed_state'],
                            desired_state=app['desired_state'], health_state=app['health_state'], workloads=app['workloads'])
            elif method == 'GET' and suffix == '':
                data = app
            elif method == 'PATCH' and suffix == '':
                body = route.request.post_data_json
                app.update(name=body['name'], description=body['description'], source_config=body['configuration'])
                data = app
            elif method == 'GET' and suffix == 'logs':
                data = [dict(workload='web', line='GET /health 200')]
            elif method == 'GET' and suffix == 'events':
                data = [dict(type='deployment.completed', stage='SUCCESS')]
            elif method == 'GET' and suffix == 'secrets':
                data = state['secrets']
            elif method == 'GET' and suffix == 'stats':
                data = [dict(ID=app['id'], Name='devbox-app-'+app['id'], CPUPerc='1%', MemUsage='10 MiB / 1 GiB', MemPerc='1%', NetIO='1 KiB / 1 KiB')]
            elif method == 'GET' and suffix == 'php-modules':
                data = dict(available=True, modules=['pdo_mysql'])
            elif method == 'GET' and suffix == 'database-binding':
                data = dict(configured=False, binding=dict(application_id=app['id']))
            elif method == 'GET' and suffix == 'jobs':
                data = [app['active_operation']] if app.get('active_operation') else []
            elif method == 'PUT' and suffix.startswith('secrets/'):
                state['secrets'].append(parts[4])
                data = dict(name=parts[4])
            elif method == 'POST' and suffix in ('deploy', 'start', 'stop', 'restart', 'reconcile'):
                job = dict(id='job-test', status='queued', type='application.'+suffix,
                           application_id=app['id'], created_at='2026-09-28T08:00:00Z')
                app['active_operation'] = job
                if suffix == 'deploy':
                    deployment = dict(id='deployment-test', job_id=job['id'], status='running',
                                      driver=app['driver'], stage=state['deployment_stage'],
                                      created_at='2026-09-28T08:00:00Z')
                    app['deployments'] = [deployment]
                    data = dict(job=job, deployment=deployment)
                else:
                    data = job
            else:
                raise AssertionError(f'Unexpected application request: {method} {endpoint}')
        elif endpoint == '/logs':
            source = parse_qs(parsed.query).get('source', ['all'])[0]
            data = [entry for entry in logs if source == 'all' or entry['source'] == source]
        elif endpoint in responses:
            data = responses[endpoint]
        else:
            raise AssertionError(f'Unexpected request: {method} {endpoint}')
        route.fulfill(json={'data': data})
    context.route('**/api/v1/**', route_api)
    context.add_cookies([{'name': 'devbox_csrf', 'value': 'smoke-csrf', 'url': BASE}])
    return state

def no_overflow(page):
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
            browser = p.chromium.launch(executable_path=os.getenv('PLAYWRIGHT_CHROMIUM_EXECUTABLE') or None)
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
            expect(page.locator('.acp-table tbody tr')).to_have_count(3)
            page.screenshot(path=str(OUT / 'applications-dark.png'), full_page=True)
            search_app = page.get_by_role('searchbox', name='Szukaj aplikacji')
            search_app.fill('WORKER')
            expect(page.locator('.acp-table tbody tr')).to_have_count(1)
            search_app.fill('')
            expect(page.locator('.acp-table tbody tr')).to_have_count(3)
            # Source wizard creates configuration, not a running deployment.
            page.get_by_role('link', name='Dodaj aplikację', exact=True).click()
            page.get_by_label('Nazwa', exact=True).fill('Nowa aplikacja')
            source_select = page.get_by_label('Źródło', exact=True)
            source_select.select_option('local')
            local_path = page.get_by_label('Katalog z kodem', exact=True)
            local_path.fill('/opt/devbox/projects/aplikacja')
            page.get_by_role('button', name='Przeglądaj', exact=True).click()
            expect(page.get_by_role('dialog', name='Wybierz katalog')).to_be_visible()
            expect(page.get_by_text('/opt/devbox/projects/aplikacja', exact=True).last).to_be_visible()
            page.get_by_role('button', name='Wybierz katalog', exact=True).click()
            expect(local_path).to_have_value('/opt/devbox/projects/aplikacja')
            # Start the existing Docker-image wizard flow from a clean form so the
            # directory-browser regression does not leave asynchronous UI state behind.
            page.reload()
            expect(page.get_by_role('heading', name='Dodaj aplikację', exact=True)).to_be_visible()
            page.get_by_label('Nazwa', exact=True).fill('Nowa aplikacja')
            page.get_by_label('Źródło', exact=True).select_option('docker_image')
            expect(page.get_by_label('Obraz OCI', exact=True)).to_be_visible()
            page.get_by_label('Obraz OCI', exact=True).fill('nginx:alpine')
            page.get_by_role('button', name='Dalej', exact=True).click()
            expect(page.get_by_role('heading', name='Technologia', exact=True)).to_be_visible()
            page.get_by_role('button', name='Dalej', exact=True).click()
            expect(page.get_by_role('heading', name='Uruchamianie', exact=True)).to_be_visible()
            page.get_by_label('Port wewnętrzny', exact=True).fill('80')
            page.get_by_role('button', name='Dalej', exact=True).click()
            expect(page.get_by_role('heading', name='Baza danych', exact=True)).to_be_visible()
            page.get_by_role('button', name='Dalej', exact=True).click()
            expect(page.get_by_role('heading', name='Podsumowanie', exact=True)).to_be_visible()
            expect(page.get_by_text('Bez katalogu źródłowego', exact=True)).to_be_visible()
            page.screenshot(path=str(OUT / 'application-wizard.png'), full_page=True)
            page.get_by_role('button', name='Zapisz', exact=True).click()
            expect(page.get_by_role('heading', name='Nowa aplikacja', exact=True)).to_be_visible()
            assert state['posts'] == ['/applications']
            state['applications'].pop()
            state['posts'].clear()
            page.goto(BASE + '/apps/portal')
            expect(page.get_by_role('heading', name='Portal zespołu', exact=True)).to_be_visible()
            page.get_by_role('button', name='Usuń', exact=True).click()
            dialog = page.get_by_role('dialog', name='Usuń aplikację')
            expect(dialog).to_be_visible()
            expect(dialog.get_by_role('button', name='Usuń aplikację')).to_be_disabled()
            dialog.get_by_role('button', name='Anuluj', exact=True).click()
            assert not state['posts']
            page.get_by_role('button', name='Settings', exact=True).click()
            page.get_by_label('Port wewnętrzny', exact=True).fill('8080')
            page.get_by_role('button', name='Zapisz konfigurację').click()
            expect(page.get_by_text('Konfiguracja zapisana.', exact=False)).to_be_visible()
            assert state['applications'][0]['source_config']['container_port'] == 8080
            page.get_by_role('button', name='Environment', exact=True).click()
            page.get_by_label('Nazwa', exact=True).fill('API_TOKEN')
            page.get_by_label('Nowa wartość', exact=True).fill('synthetic-secret')
            page.get_by_role('button', name='Zapisz sekret').click()
            expect(page.locator('.acp-secret-list code')).to_have_text('API_TOKEN')
            expect(page.get_by_label('Nowa wartość', exact=True)).to_have_value('')
            page.get_by_role('button', name='Logs', exact=True).click()
            expect(page.get_by_label('Logi aplikacji')).to_contain_text('GET /health 200')
            page.get_by_role('button', name='Deploy', exact=True).click()
            expect(page.get_by_role('button', name='Deploy', exact=True)).to_be_disabled()
            expect(page.get_by_role('link', name='Postęp i anulowanie')).to_be_visible()
            page.get_by_role('button', name='Jobs', exact=True).click()
            expect(page.get_by_role('heading', name='Historia wdrożeń')).to_be_visible()
            expect(page.get_by_role('cell', name='PREPARE', exact=True)).to_be_visible()
            assert state['posts'][-1] == '/applications/portal/deploy'
            # A successful job and real runtime state are independent reads.
            portal = state['applications'][0]
            portal.pop('active_operation')
            portal['deployments'][0].update(status='success', stage='SUCCESS')
            portal.update(status='stopped', observed_state='stopped', desired_state='stopped')
            with page.expect_response(lambda response: '/applications/portal/state' in response.url):
                page.clock.fast_forward(5100)
            expect(page.locator('.acp-header .acp-status')).to_have_text('STOPPED')
            expect(page.get_by_role('button', name='Deploy', exact=True)).to_be_enabled()
            page.screenshot(path=str(OUT / 'application-detail.png'), full_page=True)
            # Failed inspection must not retain a stale green runtime badge.
            state['failures'].add('/applications/portal/state')
            page.get_by_role('button', name='Odśwież stan', exact=True).click()
            expect(page.locator('.acp-header .acp-status')).to_have_text('UNKNOWN')
            state['failures'].clear()
            portal.update(status='running', observed_state='running', desired_state='running')
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
                expect(page.locator('.acp-table tbody tr')).to_have_count(3)
                no_overflow(page)
            state['failures'].update(['/applications', '/monitoring/snapshot', '/docker/status'])
            page.set_viewport_size({'width': 1440, 'height': 1086})
            page.goto(BASE)
            expect(page.locator('.console-kpi').first.locator('strong')).to_have_text('—')
            expect(page.locator('.gauge-value')).to_have_count(0)
            expect(page.get_by_text('Sprawdź usługi', exact=True)).to_be_visible()
            expect(page.locator('.console-services').get_by_text('NIEZNANY', exact=True)).to_be_visible()
            expect(page.get_by_text('Aplikacje: Test: API niedostępne', exact=True)).to_be_visible()
            page.screenshot(path=str(OUT / 'control-room-api-errors.png'), full_page=True)
            state['failures'].clear()
            state['applications'] = []
            page.goto(BASE + '/apps')
            expect(page.get_by_role('heading', name='Brak aplikacji')).to_be_visible()
            context.close()
            viewer = browser.new_context(viewport={'width': 1280, 'height': 900})
            install_api(viewer, role='viewer')
            viewer.add_init_script("Storage.prototype.getItem = () => { throw new Error('blocked') }; Storage.prototype.setItem = () => { throw new Error('blocked') };")
            v = viewer.new_page()
            v.on('pageerror', lambda error: errors.append(str(error)))
            v.goto(BASE + '/apps')
            expect(v.locator('.acp-table tbody tr')).to_have_count(3)
            expect(v.get_by_role('link', name='Dodaj aplikację')).to_have_count(0)
            v.get_by_role('link', name='Portal zespołu', exact=True).click()
            expect(v.get_by_role('button', name='Deploy', exact=True)).to_be_disabled()
            expect(v.get_by_role('button', name='Usuń', exact=True)).to_have_count(0)
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
        report = {'status': 'passed', 'fixtures': 'Synthetic API only; production bundle', 'checks': ['dark default/light persistence', 'real-source gauges', 'chart sample accumulation and metric/range switching', 'service failures', 'logs filters/deep link', 'job queue and details deep links', 'search', 'application wizard/configuration/secrets/deployment/CSRF', 'live runtime state independent of deployment', 'failed provider inspection', 'preserve data confirmation', '900/390/320px no overflow', 'API failures/null collections', 'viewer RBAC visibility', 'blocked storage', 'login']}
        (OUT / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        print(json.dumps(report, ensure_ascii=False))
    finally:
        server.shutdown()
        server.server_close()

if __name__ == '__main__':
    main()

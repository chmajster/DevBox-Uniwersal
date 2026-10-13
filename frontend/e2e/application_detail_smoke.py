"""Application details against the production bundle and isolated synthetic API.
Run after npm run build, using Python Playwright. No live DevBox mutations.
"""
import threading
from functools import partial
from playwright.sync_api import expect, sync_playwright
from workspace_smoke import BASE, OUT, ROOT, SPAHandler, ThreadingHTTPServer, install_api, no_overflow


def main():
    OUT.mkdir(exist_ok=True)
    server = ThreadingHTTPServer(('127.0.0.1', 4173), partial(SPAHandler, directory=str(ROOT / 'dist')))
    threading.Thread(target=server.serve_forever, daemon=True).start()
    errors = []
    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(headless=True)
            context = browser.new_context(viewport=dict(width=1440, height=1080), permissions=['clipboard-read', 'clipboard-write'])
            state = install_api(context)
            page = context.new_page()
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.clock.install()
            page.goto(BASE + '/apps/portal')
            expect(page.get_by_role('heading', name='Portal zespołu', exact=True)).to_be_visible()
            overview = page.get_by_role('region', name='Przegląd', exact=True)
            expect(overview.get_by_role('heading', name='Adres aplikacji')).to_be_visible()
            expect(overview.get_by_text('Dostępna', exact=True)).to_be_visible()
            expect(overview.get_by_role('table')).to_have_count(0)
            expect(page.get_by_role('button', name='Zatrzymaj', exact=True)).to_be_enabled()
            expect(page.get_by_role('button', name='Uruchom', exact=True)).to_have_count(0)
            expect(page.get_by_role('button', name='Przebuduj', exact=True)).not_to_be_visible()
            page.get_by_role('button', name='Kopiuj adres', exact=True).click()
            expect(overview.get_by_role('status')).to_have_text('Skopiowano')
            assert page.evaluate('navigator.clipboard.readText()') == 'http://127.0.0.1:8080/'
            page.get_by_text('Więcej działań', exact=True).click()
            expect(page.get_by_role('button', name='Przebuduj', exact=True)).to_be_visible()
            page.keyboard.press('Escape')
            expect(page.get_by_role('button', name='Przebuduj', exact=True)).not_to_be_visible()
            page.get_by_text('Więcej działań', exact=True).click()
            page.get_by_role('heading', name='Portal zespołu', exact=True).click()
            expect(page.get_by_role('button', name='Przebuduj', exact=True)).not_to_be_visible()
            assert not state['posts']
            no_overflow(page)
            page.screenshot(path=str(OUT / 'details-overview-dark.png'), full_page=True)

            page.get_by_role('button', name='Porty', exact=True).click()
            expect(page).to_have_url(BASE + '/apps/portal?tab=Ports')
            expect(page.get_by_role('heading', name='Porty i adresy')).to_be_visible()
            page.reload()
            expect(page.get_by_role('heading', name='Porty i adresy')).to_be_visible()
            page.go_back()
            expect(page.get_by_role('heading', name='Adres aplikacji')).to_be_visible()
            page.go_forward()
            expect(page.get_by_role('heading', name='Porty i adresy')).to_be_visible()
            page.screenshot(path=str(OUT / 'details-ports.png'), full_page=True)

            for tab, heading, filename in [
                ('Środowisko', 'Środowisko uruchomieniowe', 'runtime'),
                ('Baza danych', 'Baza danych aplikacji', 'database'),
                ('Domeny', 'Domeny i dostęp HTTPS', 'domains'),
                ('Zmienne i sekrety', 'Zmienne środowiskowe', 'environment'),
                ('Kontenery', 'Usługi aplikacji', 'containers'),
                ('Logi', 'Logi kontenera', 'logs'),
                ('Zadania', 'Historia wdrożeń', 'jobs'),
            ]:
                page.get_by_role('button', name=tab, exact=True).click()
                expect(page.get_by_role('heading', name=heading, exact=True)).to_be_visible()
                no_overflow(page)
                page.screenshot(path=str(OUT / f'details-{filename}.png'), full_page=True)
            page.get_by_role('button', name='Logi', exact=True).click()
            page.get_by_role('searchbox', name='Szukaj w logach').fill('no-matching-message')
            expect(page.get_by_label('Logi aplikacji')).to_have_text('Brak wpisów dla wybranego filtra.')
            page.get_by_role('searchbox', name='Szukaj w logach').fill('health')
            expect(page.get_by_label('Logi aplikacji')).to_contain_text('GET /health 200')

            page.get_by_role('button', name='Zmień nazwę', exact=True).click()
            expect(page).to_have_url(BASE + '/apps/portal?tab=Settings')
            expect(page.get_by_label('Nazwa', exact=True)).to_have_value('Portal zespołu')
            expect(page.get_by_role('button', name='Zapisz nazwę i opis')).to_be_enabled()
            page.screenshot(path=str(OUT / 'details-settings.png'), full_page=True)

            # A failed deployment is separate from the currently serving app.
            portal = state['applications'][0]
            portal['deployments'] = [dict(id='failed', status='failed', stage='HEALTH', driver='managed', created_at='2026-10-13T10:00:00Z')]
            page.get_by_role('button', name='Przegląd', exact=True).click()
            with page.expect_response(lambda response: '/applications/portal/state' in response.url):
                page.clock.fast_forward(5100)
            expect(page.locator('.acp-header .acp-status')).to_have_text('Działa')
            expect(page.locator('.app-summary-card').last.locator('.acp-status')).to_have_text('Błąd')
            page.get_by_role('button', name='Włącz jasny motyw').click()
            expect(page.locator('html')).to_have_attribute('data-theme', 'light')
            page.screenshot(path=str(OUT / 'details-overview-light.png'), full_page=True, animations='disabled')
            page.get_by_role('button', name='Włącz ciemny motyw').click()

            for width in (900, 390, 320):
                page.set_viewport_size(dict(width=width, height=844))
                for tab in ('Przegląd', 'Środowisko', 'Baza danych', 'Porty', 'Logi', 'Ustawienia'):
                    page.get_by_role('button', name=tab, exact=True).click()
                    no_overflow(page)
                    page.screenshot(path=str(OUT / f'details-{width}-{tab}.png'), full_page=True)
                page.get_by_text('Więcej działań', exact=True).click()
                expect(page.get_by_role('button', name='Przebuduj', exact=True)).to_be_visible()
                no_overflow(page)
                page.screenshot(path=str(OUT / f'details-{width}-actions.png'), full_page=True)
                page.keyboard.press('Escape')
            assert not state['posts'], 'Viewing and copying unexpectedly mutated application state'
            context.close()
            browser.close()
        assert not errors, errors
        print('Application details UI: PASS (sections, copy, menus, URL history, runtime states, dark/light, mobile 900/390/320). Synthetic API only.')
    finally:
        server.shutdown()
        server.server_close()


if __name__ == '__main__':
    main()

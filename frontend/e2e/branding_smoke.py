"""Decode real production brand assets under a same-origin-only image CSP.

Run from frontend after npm run build: python e2e/branding_smoke.py
Only the existing synthetic API fixtures are mocked, never the image requests.
"""
import json
import threading
from functools import partial
from http.server import ThreadingHTTPServer
from urllib.parse import urlparse

from playwright.sync_api import expect, sync_playwright
from workspace_smoke import BASE, OUT, ROOT, SPAHandler, install_api


class BrandingHandler(SPAHandler):
    def end_headers(self):
        self.send_header('Content-Security-Policy', "default-src 'self'; img-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'")
        super().end_headers()


def check_branding(page, name, minimum_images=1):
    images = page.locator('img.brand-logo:visible')
    expect(images.first).to_be_visible()
    assert images.count() >= minimum_images, f'{name}: missing brand images'
    decoded = []
    for image in images.all():
        dimensions = image.evaluate('''async (image) => {
            await image.decode();
            return {src: image.currentSrc, width: image.naturalWidth, height: image.naturalHeight};
        }''')
        assert dimensions['width'] > 0 and dimensions['height'] > 0, f'{name}: broken logo'
        assert dimensions['src'].startswith(BASE + '/assets/'), f'{name}: logo is not a bundled local asset'
        decoded.append(dimensions)
    favicon = page.evaluate('''async () => {
        const link = document.querySelector('link[rel="icon"]');
        if (!link) throw new Error('Missing favicon');
        const image = new Image();
        image.src = link.href;
        await image.decode();
        return {src: image.src, width: image.naturalWidth, height: image.naturalHeight};
    }''')
    assert favicon['src'].startswith(BASE + '/assets/') and urlparse(favicon['src']).path.endswith('.ico')
    assert favicon['width'] > 0 and favicon['height'] > 0
    page.screenshot(path=str(OUT / f'branding-{name}.png'), full_page=True)
    return dict(case=name, logos=decoded, favicon=favicon)


def main():
    if not (ROOT / 'dist' / 'index.html').exists():
        raise SystemExit('Build the frontend first: npm ci && npm run build')
    OUT.mkdir(exist_ok=True)
    server = ThreadingHTTPServer(('127.0.0.1', 4173), partial(BrandingHandler, directory=str(ROOT / 'dist')))
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    report, errors = [], []
    try:
        with sync_playwright() as playwright:
            browser = playwright.chromium.launch()
            for authenticated, route, prefix in [(False, '/login', 'login'), (True, '/', 'sidebar')]:
                context = browser.new_context(viewport={'width': 1280, 'height': 900})
                install_api(context, authenticated=authenticated)
                page = context.new_page()
                page.on('pageerror', lambda error: errors.append(str(error)))
                page.goto(BASE + route)
                expect(page.locator('img.brand-logo:visible').first).to_be_visible()
                for theme in ('light', 'dark'):
                    page.evaluate('(theme) => { document.documentElement.dataset.theme = theme }', theme)
                    report.append(check_branding(page, f'{prefix}-{theme}', 2 if authenticated else 1))
                if authenticated:
                    page.get_by_role('button', name='Zwiń menu', exact=True).click()
                    report.append(check_branding(page, 'sidebar-collapsed'))
                    page.set_viewport_size({'width': 390, 'height': 844})
                    page.get_by_role('button', name='Otwórz menu', exact=True).click()
                    expect(page.get_by_role('dialog', name='Menu DevBox')).to_be_visible()
                    report.append(check_branding(page, 'mobile-drawer'))
                context.close()
            browser.close()
        assert not errors, f'Browser errors: {errors}'
        (OUT / 'branding-report.json').write_text(json.dumps(report, indent=2), encoding='utf-8')
        print(f'Branding smoke: {len(report)} cases passed; production PNG/ICO decoded successfully.')
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


if __name__ == '__main__':
    main()

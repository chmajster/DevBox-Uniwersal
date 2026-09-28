# DevBox brand assets

The approved cube artwork is stored as binary assets in `frontend/src/assets/`.
`devboxBrand.ts` resolves `devbox-logo.png` through Vite's static asset pipeline.
`frontend/index.html` references `devbox-favicon.ico` through the same pipeline.
Both references use `?no-inline`, so production builds emit same-origin files
with content-hashed names instead of handwritten base64 strings. Existing sidebar,
login and host-summary consumers retain their layout and shared logo constant.

## Recovery provenance

The PNG string previously committed in `devboxBrand.ts` was incomplete: its first
IDAT chunk declared 43,673 bytes, while the entire 22,368-byte TypeScript file
could contain fewer than 16,776 decoded bytes. It was not a CSS spacing defect.
The existing ICO in `frontend/index.html` contained valid 16x16 and 32x32 PNG
frames. The larger frame was extracted byte-for-byte as `devbox-logo.png`; the
ICO itself is also preserved byte-for-byte. This preserves the approved design,
not a replacement drawing. The recovered logo has 32x32 native resolution; it
is not a recovery of the missing 192x192 source or an artificially upscaled asset.

## Verification

From `frontend/`:

```sh
python -m unittest discover -s e2e -p 'test_branding_assets.py' -v
npm ci
npm run lint
npm run typecheck
npm test
npm run build
python e2e/branding_smoke.py
```

The Python unit checks use only the standard library and verify PNG chunk bounds,
checksums, zlib completeness, scanline length/filter bytes, ICO frame bounds and
artwork identity. Negative tests reject truncated PNGs/ICOs and corrupted chunks.
The Chromium regression test requires the existing Playwright tooling. It serves
the actual production bundle, mocks only API fixtures, and calls `image.decode()`
on the real logo and favicon. It covers login and sidebar/host summary in both
themes, collapsed navigation and a 390px mobile drawer, under `img-src 'self'`.
Screenshots and a JSON report are uploaded by the Workspace UI workflow. These
checks do not exercise a user's installed host or real infrastructure providers.

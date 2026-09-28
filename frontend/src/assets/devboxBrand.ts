// Keep the approved artwork in a real image file: the previous hand-copied
// base64 PNG was truncated. Vite emits a hashed, same-origin asset in builds.
export const DEVBOX_LOGO = new URL('./devbox-logo.png?no-inline', import.meta.url).href

// SPA mode: the dashboard is a client-rendered app embedded in the Go binary.
// adapter-static emits a single index.html fallback; SSR and prerender are off
// so the binary only serves static assets and a single HTML shell.
export const ssr = false;
export const prerender = false;
export const trailingSlash = 'ignore';

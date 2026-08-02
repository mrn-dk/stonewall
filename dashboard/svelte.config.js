import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
  preprocess: vitePreprocess(),
  kit: {
    // Static SPA: a single index.html + assets, embeddable into the Go binary.
    adapter: adapter({
      pages: 'build',
      assets: 'build',
      fallback: 'index.html',
      precompress: false,
      strict: false
    }),
    // The API and dashboard are served from the same origin; the dashboard
    // talks to the control plane at /v1. No separate dev proxy needed for the
    // embedded deployment, but enable one for `npm run dev`.
    csrf: { checkOrigin: false }
  }
};

export default config;

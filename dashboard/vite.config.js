import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  // Tailwind v4 is configured in CSS (src/app.css), not a JS config file; the
  // plugin only wires the compiler into the build.
  plugins: [tailwindcss(), sveltekit()],
  server: {
    proxy: {
      '/v1': 'http://localhost:8080'
    }
  }
});

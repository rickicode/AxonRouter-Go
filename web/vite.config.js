import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [tailwindcss(), sveltekit()],
  server: {
    port: 5173,
    host: '0.0.0.0',
    allowedHosts: true,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:3777',
        changeOrigin: true
      },
      '/v1': {
        target: 'http://127.0.0.1:3777',
        changeOrigin: true
      }
    }
  },
  build: {
    sourcemap: true,
    minify: 'esbuild',
    target: 'esnext'
  }
});

import { svelte } from '@sveltejs/vite-plugin-svelte';
import { defineConfig } from 'vite';
import tailwindcss from '@tailwindcss/vite';
import { fileURLToPath } from 'url';

export default defineConfig({
  logLevel: 'error',
  plugins: [tailwindcss(), svelte()],
  publicDir: 'static',
  resolve: {
    alias: {
      '$lib': fileURLToPath(new URL('./src/lib', import.meta.url))
    }
  },
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
    outDir: 'build',
    sourcemap: true,
    target: 'esnext',
    chunkSizeWarningLimit: 750
  }
});

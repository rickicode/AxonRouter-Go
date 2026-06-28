import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    port: 5173,
    host: '0.0.0.0'
  },
  build: {
    // Output directory matches adapter-static
    outDir: 'build',
    // Generate sourcemaps for debugging
    sourcemap: true,
    // Minify for production (use esbuild instead of terser)
    minify: 'esbuild',
    // Target modern browsers
    target: 'esnext'
  }
});

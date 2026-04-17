/// <reference types="vitest" />
import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import { fileURLToPath, URL } from 'node:url';

// Vite build output is placed in frontend/dist/, which the Go backend embeds
// via //go:embed all:dist after a copy step (see architecture §4).
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    target: 'es2020',
  },
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      // Forward API + file-serving calls to the Gin backend during development
      // so the frontend can share the same origin and avoid CORS surface.
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: false },
      '/files': { target: 'http://127.0.0.1:8080', changeOrigin: false },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['tests/unit/**/*.test.ts'],
  },
});

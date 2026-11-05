import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // Proxy through the nginx front door during development. Auth is enforced
    // at nginx for /api, /stream-api, /download-api, and /stream.
    proxy: {
      '/api': 'http://localhost:8080',
      '/stream-api': 'http://localhost:8080',
      '/download-api': 'http://localhost:8080',
      '/stream': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.js',
  },
});

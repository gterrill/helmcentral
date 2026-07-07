import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitest/config'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const apiProxyTarget = process.env.VITE_API_PROXY_TARGET || 'http://localhost:8080'

export default defineConfig(({ mode }) => ({
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  // react-grid-layout's bundled react-draggable reads `process.env.NODE_ENV` (a Node/webpack
  // convention) for a dev-only debug log. Vite doesn't polyfill `process` in the browser, so
  // without this define the reference throws `ReferenceError: process is not defined` on every
  // drag-start mousedown, aborting the drag before it engages.
  define: {
    'process.env.NODE_ENV': JSON.stringify(mode),
  },
  server: {
    port: 5173,
    host: '0.0.0.0',
    watch: {
      usePolling: true,
      interval: 300,
      ignored: ['**/settings.yaml'],
    },
    fs: {
      allow: ['..'],
    },
    proxy: {
      '/api': {
        target: apiProxyTarget,
        changeOrigin: true,
      }
    }
  },
  build: {
    target: 'ES2020',
    outDir: 'dist',
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
  },
}))

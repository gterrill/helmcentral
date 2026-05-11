import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    host: '0.0.0.0',
    watch: {
      usePolling: true,
      interval: 300,
    },
    fs: {
      allow: ['..'],
    },
    proxy: {
      '/api': {
        target: 'http://backend-dev:8080',
        changeOrigin: true,
      }
    }
  },
  build: {
    target: 'ES2020',
    outDir: 'dist',
  }
})

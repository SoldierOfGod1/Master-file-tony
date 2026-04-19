import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
        timeout: 10 * 60 * 1000,
        proxyTimeout: 10 * 60 * 1000,
      },
      '/ws': {
        target: 'ws://127.0.0.1:8080',
        ws: true,
      },
      '/health': 'http://127.0.0.1:8080',
    },
  },
  build: {
    outDir: '../frontend',
    emptyOutDir: true,
  },
})

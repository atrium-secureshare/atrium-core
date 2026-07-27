import path from 'node:path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    // Emit into the Go module so the binary can embed the bundle.
    outDir: path.resolve(__dirname, '../internal/webui/dist'),
    emptyOutDir: false,
  },
  server: {
    // Proxy the gateway routes to a locally running core so `npm run dev` hits the real backend.
    proxy: {
      '/api': 'http://localhost:8080',
      '/auth': 'http://localhost:8080',
      '/branding': 'http://localhost:8080',
    },
  },
})

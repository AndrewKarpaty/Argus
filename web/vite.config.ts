import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The dev server proxies the API to a locally running argusd, so the UI runs
// with hot reload against the real backend. In production the built assets are
// embedded into argusd itself (see web/embed.go) and served from the same
// origin, which is why no base URL is configurable here.
const target = process.env.ARGUS_API_URL ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target, changeOrigin: true },
      '/readyz': { target, changeOrigin: true },
      '/healthz': { target, changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
  },
})

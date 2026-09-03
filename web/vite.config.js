import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const backendProxy = () => ({ target: 'http://localhost:17654', changeOrigin: false })
const frontendPort = Number(process.env.VITE_DEV_PORT || 5173)

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: frontendPort,
    // Keep the development URL stable for VS Code remote forwarding. Vite's
    // default fallback to 5174/5175 makes it easy to open a second, stale
    // frontend by accident when 5173 is already occupied.
    strictPort: true,
    proxy: {
      '/healthz': backendProxy(),
      '/auth': backendProxy(),
      '/videos': backendProxy(),
      '/tags': backendProxy(),
      '/sync': backendProxy(),
      '/directories': backendProxy(),
      '/jav': backendProxy(),
      '/config': backendProxy(),
      '/tools': backendProxy(),
    },
  },
})

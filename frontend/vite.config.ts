import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const proxy = {
    '/console-api': { target: env.SICP_WORKER_TARGET || 'http://127.0.0.1:18080' },
    '/admin': { target: env.SICP_WORKER_TARGET || 'http://127.0.0.1:18080' },
  }
  return { plugins: [vue()], base: '/console/', server: { port: 5173, strictPort: true, proxy }, preview: { proxy } }
})

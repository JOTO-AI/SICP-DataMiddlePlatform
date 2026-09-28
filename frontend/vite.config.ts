import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const proxy = {
    '/gateway/query': { target: env.SICP_QUERY_TARGET || 'http://127.0.0.1:18080', rewrite: (p: string) => p.replace(/^\/gateway\/query/, '') },
    '/gateway/worker': { target: env.SICP_WORKER_TARGET || 'http://127.0.0.1:18081', rewrite: (p: string) => p.replace(/^\/gateway\/worker/, '') },
  }
  return { plugins: [vue()], base: '/console/', server: { port: 5173, strictPort: true, proxy }, preview: { proxy } }
})

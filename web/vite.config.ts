import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发期同源策略：所有 /api 与 /.well-known 请求代理到本地 astral-server，
// 前端代码永远用相对路径请求，生产（同源部署 / 静态托管）无需改代码。
// TODO(phase-6): 生产模式 server 直接托管 web/dist，删除对 CORS 的依赖。
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: false },
      '/.well-known': { target: 'http://127.0.0.1:8080', changeOrigin: false },
    },
  },
})

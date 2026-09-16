import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// 开发期同源策略：所有 /api 与 /.well-known 请求代理到本地 astral-server，
// 前端代码永远用相对路径请求，生产（同源部署 / 静态托管）无需改代码。
// S6-1 已落地：`make web-dist`（server/Makefile）把本目录构建产物拷入
// server/webdist/dist 并经 go:embed 随二进制发布，生产同源零 CORS；
// 跨域白名单仅开发期需要（服务端 ASTRAL_DEV_CORS_ORIGINS）。
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: false },
      '/.well-known': { target: 'http://127.0.0.1:8080', changeOrigin: false },
    },
  },
})

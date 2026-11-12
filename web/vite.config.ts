import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 开发期把 API 与 WebSocket 代理到本地 Go 服务，避免 CORS 配置。
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
      '/clusters': {
        target: 'ws://127.0.0.1:8080',
        ws: true,
      },
    },
  },
  build: {
    // 产物直接写入 Go 的 embed 目录，避免 node_modules 被 Go 工具链扫描。
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
})

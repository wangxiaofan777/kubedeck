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
        ws: true,
      },
    },
  },
  worker: {
    format: 'es',
  },
  build: {
    // 产物直接写入 Go 的 embed 目录，避免 node_modules 被 Go 工具链扫描。
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // 把体量稳定的框架依赖拆开：Monaco 仍由路由懒加载，主包保持可缓存的小体积。
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined
          if (id.includes('monaco')) return 'monaco'
          if (
            id.includes('@ant-design') ||
            id.includes('/antd/') ||
            id.includes('/rc-') ||
            id.includes('/@rc-component/')
          ) {
            return 'antd'
          }
          if (id.includes('react-router')) return 'router'
          if (id.includes('react') || id.includes('scheduler')) return 'react'
          if (id.includes('@tanstack')) return 'query'
          return undefined
        },
      },
    },
  },
})

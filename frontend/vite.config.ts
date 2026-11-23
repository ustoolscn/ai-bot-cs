import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: {
    port: 5173,
    // 状态图模板放在后端目录，前端预览直接读取同一份文件
    fs: { allow: ['..'] },
    proxy: {
      '/api': {
        target: process.env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:8080',
        ws: true,
        // 本地后端没启动时返回 502，让开发模式走示例数据，而不是报 HTTP 500。
        configure: (proxy) => {
          proxy.on('error', (_error, _req, res) => {
            if ('writeHead' in res && !res.headersSent) { res.writeHead(502); res.end() }
          })
        },
      },
      '/callbacks': process.env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:8080',
    },
  },
  test: { environment: 'jsdom' },
})

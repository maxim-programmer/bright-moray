import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// В dev-режиме запросы к API и картинкам уходят на бэкенд (см. docker-compose.dev.yml),
// чтобы cookie сессии работала с того же origin, что и фронтенд.
export default defineConfig({
  plugins: [vue()],
  server: {
    proxy: {
      // ws: true — иначе апгрейд до WebSocket на /api/ws в dev не пройдёт.
      // changeOrigin не включаем: бэкенд сверяет Origin с Host запроса.
      '/api': { target: 'http://localhost:8080', ws: true },
      '/uploads': 'http://localhost:8080'
    }
  }
})

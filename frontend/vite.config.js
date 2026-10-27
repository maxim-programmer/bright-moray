import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// В dev-режиме запросы к API и картинкам уходят на бэкенд (см. docker-compose.dev.yml),
// чтобы cookie сессии работала с того же origin, что и фронтенд.
export default defineConfig({
  plugins: [vue()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/uploads': 'http://localhost:8080'
    }
  }
})

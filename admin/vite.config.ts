import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// Админка раздаётся сервисом по /admin/ (ADMIN_UI_DIR), API — по /admin/v1 на том же адресе.
// В разработке Vite проксирует API на запущенный `make run`.
export default defineConfig({
  base: '/admin/',
  plugins: [vue()],
  // Element Plus подключён целиком: для внутренней админки это проще, чем дробить сборку.
  build: { chunkSizeWarningLimit: 1500 },
  server: {
    port: 5173,
    proxy: {
      '/admin/v1': 'http://localhost:8080',
    },
  },
})

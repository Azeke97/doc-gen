import { createRouter, createWebHashHistory } from 'vue-router'

import { api, setUnauthorizedHandler } from './api'
import { currentAdmin, loadAdmin } from './session'
import LoginView from './views/LoginView.vue'
import ProjectsView from './views/ProjectsView.vue'

// Хэш-история: сервису достаточно отдавать index.html и файлы, без перенаправлений на него.
export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/projects' },
    { path: '/login', component: LoginView, meta: { public: true } },
    { path: '/projects', component: ProjectsView },
    { path: '/:pathMatch(.*)*', redirect: '/projects' },
  ],
})

router.beforeEach(async (to) => {
  if (to.meta.public) return true
  if (!api.isLoggedIn()) return { path: '/login', query: { next: to.fullPath } }
  if (!currentAdmin.value) {
    try {
      await loadAdmin()
    } catch {
      return { path: '/login', query: { next: to.fullPath } }
    }
  }
  return true
})

setUnauthorizedHandler(() => {
  currentAdmin.value = null
  const next = router.currentRoute.value.fullPath
  if (router.currentRoute.value.path !== '/login') void router.push({ path: '/login', query: { next } })
})

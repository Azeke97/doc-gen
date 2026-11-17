import { createRouter, createWebHashHistory } from 'vue-router'

import { api, setUnauthorizedHandler } from './api'
import { authRequired, currentAdmin, loadAdmin, loadLoginInfo } from './session'
import JournalEntryView from './views/JournalEntryView.vue'
import JournalView from './views/JournalView.vue'
import LoginView from './views/LoginView.vue'
import ProjectsView from './views/ProjectsView.vue'
import ReportsView from './views/ReportsView.vue'

// Хэш-история: сервису достаточно отдавать index.html и файлы, без перенаправлений на него.
export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/journal' },
    { path: '/login', component: LoginView, meta: { public: true } },
    { path: '/journal', component: JournalView },
    { path: '/journal/:id', component: JournalEntryView, props: true },
    { path: '/reports', component: ReportsView },
    { path: '/projects', component: ProjectsView },
    { path: '/:pathMatch(.*)*', redirect: '/journal' },
  ],
})

router.beforeEach(async (to) => {
  try {
    await loadLoginInfo()
  } catch {
    // сервис недоступен — дальше покажет ошибку сама страница
  }
  const needLogin = authRequired.value !== false

  if (to.meta.public) return needLogin ? true : '/'
  if (needLogin && !api.isLoggedIn()) return { path: '/login', query: { next: to.fullPath } }
  if (!currentAdmin.value) {
    try {
      await loadAdmin()
    } catch {
      return needLogin ? { path: '/login', query: { next: to.fullPath } } : true
    }
  }
  return true
})

setUnauthorizedHandler(() => {
  currentAdmin.value = null
  const next = router.currentRoute.value.fullPath
  if (router.currentRoute.value.path !== '/login') void router.push({ path: '/login', query: { next } })
})

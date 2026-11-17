import { createRouter, createWebHashHistory } from 'vue-router'

import { api, setUnauthorizedHandler } from './api'
import { currentAdmin, loadAdmin } from './session'
import LoginView from './views/LoginView.vue'
import ProjectsView from './views/ProjectsView.vue'
import TemplatesView from './views/TemplatesView.vue'
import TemplateView from './views/TemplateView.vue'

// Хэш-история: сервису достаточно отдавать index.html и файлы, без перенаправлений на него.
export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/templates' },
    { path: '/login', component: LoginView, meta: { public: true } },
    { path: '/templates', component: TemplatesView },
    { path: '/templates/:id', component: TemplateView, props: true },
    // Редактор тянет CodeMirror — грузим его отдельным файлом, только когда он нужен.
    { path: '/templates/:id/edit', component: () => import('./views/TemplateEditorView.vue'), props: true },
    { path: '/projects', component: ProjectsView },
    { path: '/:pathMatch(.*)*', redirect: '/templates' },
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

import { ref } from 'vue'

import { api, type Admin } from './api'

/** Текущий администратор; null — не загружен или не вошёл. */
export const currentAdmin = ref<Admin | null>(null)

export async function loadAdmin() {
  currentAdmin.value = await api.me()
}

export async function logout() {
  await api.logout().catch(() => {})
  currentAdmin.value = null
}

import { ref } from 'vue'

import { api, type Admin } from './api'

/** Текущий администратор; null — не загружен или не вошёл. */
export const currentAdmin = ref<Admin | null>(null)

/** Нужен ли вход. На демо-стенде (ADMIN_AUTH_DISABLED) админка открыта без него. */
export const authRequired = ref<boolean | null>(null)

export async function loadLoginInfo() {
  if (authRequired.value === null) authRequired.value = (await api.loginInfo()).auth_required
}

export async function loadAdmin() {
  currentAdmin.value = await api.me()
}

export async function logout() {
  await api.logout().catch(() => {})
  currentAdmin.value = null
}

// Клиент админского API (/admin/v1). Токен сессии хранится в localStorage.

const BASE = '/admin/v1'
const TOKEN_KEY = 'docgen.token'

export interface Admin {
  id: string
  email: string
  name: string
}

export interface Project {
  code: string
  name: string
  created_at: string
}

export interface FieldError {
  field: string
  message: string
}

/** Ошибка API в формате {"error": {code, message, details}}. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly details?: unknown,
  ) {
    super(message)
  }

  get fieldErrors(): FieldError[] {
    return Array.isArray(this.details) ? (this.details as FieldError[]) : []
  }

}

let onUnauthorized: () => void = () => {}

/** Что делать, когда сессия истекла: роутер отправляет на страницу входа. */
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY)
  } catch {
    return null
  }
}

function setToken(token: string | null) {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token)
    else localStorage.removeItem(TOKEN_KEY)
  } catch {
    // без localStorage сессия просто не переживёт перезагрузку
  }
}

async function request(method: string, path: string, body?: BodyInit, json = true): Promise<Response> {
  const headers: Record<string, string> = {}
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  if (body !== undefined && json) headers['Content-Type'] = 'application/json'

  const res = await fetch(BASE + path, { method, headers, body })
  if (res.ok) return res

  let err = new ApiError(res.status, 'http_' + res.status, `Ошибка сервера (${res.status})`)
  try {
    const data = await res.json()
    if (data?.error) err = new ApiError(res.status, data.error.code, data.error.message, data.error.details)
  } catch {
    // тело не JSON — оставляем общее сообщение
  }
  if (res.status === 401 && path !== '/login') {
    setToken(null)
    onUnauthorized()
  }
  throw err
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await request(method, path, body === undefined ? undefined : JSON.stringify(body))
  return res.status === 204 ? (undefined as T) : ((await res.json()) as T)
}

/** Скачивает ответ как файл; имя берётся из Content-Disposition. */
async function blob(path: string): Promise<{ blob: Blob; filename: string }> {
  const res = await request('GET', path)
  const match = /filename="([^"]+)"/.exec(res.headers.get('Content-Disposition') ?? '')
  return { blob: await res.blob(), filename: match?.[1] ?? 'file' }
}

export const api = {
  isLoggedIn: () => getToken() !== null,

  async login(email: string, password: string) {
    const res = await call<{ token: string; expires_at: string }>('POST', '/login', { email, password })
    setToken(res.token)
  },
  async logout() {
    try {
      await call<void>('POST', '/logout')
    } finally {
      setToken(null)
    }
  },
  me: () => call<Admin>('GET', '/me'),

  projects: () => call<Project[]>('GET', '/projects'),
  createProject: (p: { code: string; name: string }) => call<Project>('POST', '/projects', p),
}

/** Отдаёт браузеру Blob как скачиваемый файл. */
export function saveBlob(b: Blob, filename: string) {
  const url = URL.createObjectURL(b)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

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

export type Locale = 'ru' | 'kk' | 'bilingual'
export type Engine = 'html' | 'docx'

export interface Template {
  id: string
  project: string
  code: string
  locale: Locale
  name: string
  engine: Engine
  status: 'draft' | 'active' | 'archived'
  retention_days: number
  active_version_id: string | null
  created_at: string
  updated_at: string
}

export interface TemplateVersion {
  id: string
  template_id: string
  version: number
  status: 'draft' | 'published'
  file_sha256: string
  data_schema: unknown
  test_data: unknown
  created_by: string
  created_at: string
  updated_at: string
  published_by: string | null
  published_at: string | null
}

export interface SchemaChange {
  path: string
  kind: string
  breaking: boolean
  message: string
}

export interface SchemaDiff {
  base_version: number | null
  breaking: boolean
  changes: SchemaChange[]
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

  get diff(): SchemaDiff | undefined {
    return this.code === 'breaking_changes' ? (this.details as SchemaDiff) : undefined
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

const confirmQuery = (confirm: boolean) => (confirm ? '?confirm_breaking=true' : '')

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

  templates: (project?: string) =>
    call<Template[]>('GET', '/templates' + (project ? '?project=' + encodeURIComponent(project) : '')),
  template: (id: string) => call<Template>('GET', `/templates/${id}`),
  createTemplate: (t: Pick<Template, 'project' | 'code' | 'locale' | 'name' | 'engine' | 'retention_days'>) =>
    call<Template>('POST', '/templates', t),
  activate: (templateId: string, versionId: string, confirm = false) =>
    call<Template>('POST', `/templates/${templateId}/activate${confirmQuery(confirm)}`, { version_id: versionId }),
  archive: (templateId: string) => call<Template>('POST', `/templates/${templateId}/archive`),

  versions: (templateId: string) => call<TemplateVersion[]>('GET', `/templates/${templateId}/versions`),
  async uploadVersion(templateId: string, file: File, schema: string, testData: string) {
    const form = new FormData()
    form.append('file', file)
    form.append('schema', schema)
    if (testData.trim()) form.append('test_data', testData)
    const res = await request('POST', `/templates/${templateId}/versions`, form, false)
    return (await res.json()) as TemplateVersion
  },
  publish: (versionId: string, confirm = false) =>
    call<TemplateVersion>('POST', `/versions/${versionId}/publish${confirmQuery(confirm)}`),
  diff: (versionId: string) => call<SchemaDiff>('GET', `/versions/${versionId}/diff`),
  preview: (versionId: string) => blob(`/versions/${versionId}/preview`),
  versionFile: (versionId: string) => blob(`/versions/${versionId}/file`),
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

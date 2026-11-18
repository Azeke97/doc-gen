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
  retention_days: number
  created_at: string
}

/** API-ключ портала: сам ключ не хранится, видно только его начало. */
export interface ApiKey {
  id: string
  name: string
  project: string
  key_prefix: string
  created_at: string
  revoked_at: string | null
  /** По ключу есть документы или журнал: его можно только отозвать. */
  used?: boolean
}

/** Только что выданный ключ: key показывается один раз. */
export interface IssuedKey extends ApiKey {
  key: string
}

export type JournalStatus = 'created' | 'repeated' | 'rejected' | 'failed'

export interface JournalEntry {
  id: string
  created_at: string
  project: string
  project_name: string
  request_id: string
  template_name: string | null
  template_sha256: string | null
  format: string | null
  external_id: string | null
  requested_by: string | null
  idempotency_key: string | null
  status: JournalStatus
  http_status: number
  error_code: string | null
  error_message: string | null
  error_details: Array<{ field?: string; part?: string; context?: string; message: string }> | null
  document_id: string | null
  result_size: number | null
  duration_ms: number
}

export interface JournalPage {
  items: JournalEntry[]
  next_cursor: string | null
}

export interface JournalCard {
  entry: JournalEntry
  document_state: 'available' | 'expired' | 'deleted' | null
  document: {
    id: string
    format: string
    file_size: number
    file_sha256: string
    created_at: string
    expires_at: string
    expired: boolean
    data: unknown
  } | null
}

export interface JournalFilter {
  project?: string
  template_name?: string
  external_id?: string
  status?: JournalStatus[]
  from?: string // YYYY-MM-DD, день по Астане
  to?: string
  cursor?: string
}

export interface ReportGroup {
  project: string
  project_name?: string
  template_name?: string
  created: number
  errors: number
  avg_duration_ms?: number
}

export interface Report {
  from: string
  to: string
  totals: { requests: number; created: number; repeated: number; rejected: number; failed: number; avg_duration_ms: number }
  by_project: ReportGroup[]
  by_template: ReportGroup[]
  by_day: Array<{ day: string; created: number; errors: number }>
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

/** Параметры запроса: пустые значения не отправляются, массивы — повторяющимся параметром. */
function query(params: object): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (Array.isArray(v)) v.forEach((x) => q.append(k, String(x)))
    else if (v !== undefined && v !== null && v !== '') q.append(k, String(v))
  }
  const s = q.toString()
  return s ? '?' + s : ''
}

export const api = {
  isLoggedIn: () => getToken() !== null,
  loginInfo: () => call<{ auth_required: boolean }>('GET', '/login-info'),

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
  createProject: (p: { code: string; name: string; retention_days: number }) => call<Project>('POST', '/projects', p),
  updateProject: (code: string, p: { name?: string; retention_days?: number }) =>
    call<Project>('PATCH', `/projects/${encodeURIComponent(code)}`, p),
  keys: (project: string) => call<ApiKey[]>('GET', `/projects/${encodeURIComponent(project)}/keys`),
  issueKey: (project: string, name: string) =>
    call<IssuedKey>('POST', `/projects/${encodeURIComponent(project)}/keys`, { name }),
  revokeKey: (id: string) => call<ApiKey>('POST', `/keys/${id}/revoke`),
  deleteKey: (id: string) => call<void>('DELETE', `/keys/${id}`),

  journal: (f: JournalFilter & { limit?: number }) => call<JournalPage>('GET', '/journal' + query(f)),
  journalEntry: (id: string) => call<JournalCard>('GET', `/journal/${id}`),
  reports: (f: { project?: string; from?: string; to?: string }) => call<Report>('GET', '/reports' + query(f)),
}

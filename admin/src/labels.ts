import type { JournalStatus } from './api'

export const statusLabels: Record<JournalStatus, { label: string; type: 'success' | 'info' | 'warning' | 'danger' }> = {
  created: { label: 'Выпущен', type: 'success' },
  repeated: { label: 'Повтор', type: 'info' },
  rejected: { label: 'Отклонён', type: 'warning' },
  failed: { label: 'Сбой', type: 'danger' },
}

/** Что означают коды ошибок генерации — для людей, читающих журнал. */
export const errorLabels: Record<string, string> = {
  invalid_form: 'Неверный формат запроса',
  invalid_template: 'Ошибка в шаблоне',
  invalid_data: 'Данные не подходят шаблону',
  invalid_format: 'Неверный формат результата',
  invalid_template_name: 'Нет имени шаблона',
  invalid_external_id: 'Нет внешнего ID',
  invalid_requested_by: 'Нет автора запроса',
  invalid_retention_days: 'Неверный срок хранения',
  invalid_idempotency_key: 'Нет ключа идемпотентности',
  idempotency_conflict: 'Ключ уже использован для другого запроса',
  conversion_failed: 'Не удалось сделать PDF',
  converter_unavailable: 'Конвертер PDF недоступен',
  request_too_large: 'Слишком большой запрос',
  internal: 'Внутренняя ошибка сервиса',
}

const dateTime = new Intl.DateTimeFormat('ru-RU', {
  timeZone: 'Asia/Almaty',
  day: '2-digit',
  month: '2-digit',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
})

const dateOnly = new Intl.DateTimeFormat('ru-RU', { timeZone: 'Asia/Almaty', day: '2-digit', month: '2-digit', year: 'numeric' })

/** Дата и время по Астане, как в документах. */
export function formatDateTime(iso: string | null | undefined): string {
  return iso ? dateTime.format(new Date(iso)) : '—'
}

/** День «2026-11-17» → «17.11.2026». */
export function formatDay(day: string): string {
  return dateOnly.format(new Date(day + 'T12:00:00+05:00'))
}

export function formatSize(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined) return '—'
  if (bytes < 1024) return `${bytes} Б`
  if (bytes < 1 << 20) return `${(bytes / 1024).toFixed(1)} КБ`
  return `${(bytes / (1 << 20)).toFixed(1)} МБ`
}

export function prettyJSON(v: unknown): string {
  return v === null || v === undefined ? '' : JSON.stringify(v, null, 2)
}

/** Текст ошибки API для уведомления. */
export function errorText(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

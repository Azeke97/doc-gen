const dateTime = new Intl.DateTimeFormat('ru-RU', {
  timeZone: 'Asia/Almaty',
  day: '2-digit',
  month: '2-digit',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

/** Дата и время по Астане, как в документах. */
export function formatDateTime(iso: string | null | undefined): string {
  return iso ? dateTime.format(new Date(iso)) : '—'
}

export function prettyJSON(v: unknown): string {
  return v === null || v === undefined ? '' : JSON.stringify(v, null, 2)
}

/** Текст ошибки API для уведомления. */
export function errorText(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

import type { Locale, Template, TemplateVersion } from './api'

export const localeLabels: Record<Locale, string> = {
  ru: 'Русский',
  kk: 'Казахский',
  bilingual: 'Два языка (kk + ru)',
}

export const templateStatus: Record<Template['status'], { label: string; type: 'info' | 'success' | 'danger' }> = {
  draft: { label: 'Не активен', type: 'info' },
  active: { label: 'Активен', type: 'success' },
  archived: { label: 'В архиве', type: 'danger' },
}

export const versionStatus: Record<TemplateVersion['status'], { label: string; type: 'warning' | 'success' }> = {
  draft: { label: 'Черновик', type: 'warning' },
  published: { label: 'Опубликована', type: 'success' },
}

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

const errorTexts: Record<string, string> = {
  invalid_file: 'Файл шаблона не принят',
  invalid_schema: 'Схема данных не принята',
  invalid_test_data: 'Тестовые данные не подходят под схему',
  render_failed: 'Шаблон не удалось отрисовать',
  no_test_data: 'У версии нет тестовых данных — предпросмотр невозможен',
  preview_not_supported: 'Предпросмотр доступен только для HTML-шаблонов',
  template_archived: 'Шаблон в архиве, изменять его нельзя',
  version_already_published: 'Версия уже опубликована',
  version_not_published: 'Активной можно сделать только опубликованную версию',
  template_not_found: 'Шаблон не найден',
  version_not_found: 'Версия не найдена',
  request_too_large: 'Файл слишком большой',
  draft_changed: 'Черновик за это время изменил другой пользователь',
}

/** Понятный текст ошибки API, после двоеточия — сообщение сервера. */
export function errorText(e: unknown): string {
  if (!(e instanceof Error)) return String(e)
  const code = (e as { code?: string }).code
  const known = code ? errorTexts[code] : undefined
  return known ? `${known}: ${e.message}` : e.message
}

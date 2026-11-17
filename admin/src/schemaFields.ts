// Поля из JSON Schema для панели «Поля» редактора: что есть в данных и как это вывести в шаблоне.

export interface SchemaField {
  /** Путь для вставки: ".lots" или внутри range — ".name". */
  path: string
  /** Как поле видно в списке: "lots[].name". */
  label: string
  type: string
  title?: string
  required: boolean
  snippet: string
  depth: number
}

type Node = Record<string, unknown>

const isObject = (v: unknown): v is Node => typeof v === 'object' && v !== null && !Array.isArray(v)

/** Разворачивает локальную ссылку "#/$defs/name" (внешних $ref в схемах DocGen не бывает). */
function resolve(root: Node, node: Node): Node {
  const ref = node.$ref
  if (typeof ref !== 'string' || !ref.startsWith('#/')) return node
  let cur: unknown = root
  for (const part of ref.slice(2).split('/')) cur = isObject(cur) ? cur[part] : undefined
  return isObject(cur) ? { ...cur, ...node, $ref: undefined } : node
}

function typeOf(node: Node): string {
  const t = node.type
  if (typeof t === 'string') return t
  if (Array.isArray(t)) return t.filter((x) => x !== 'null').join(' | ')
  return ''
}

/** Поле вида строка или {"ru": "...", "kk": "..."} — выводится через tr. */
function isLocalized(root: Node, node: Node): boolean {
  const variants = [node.oneOf, node.anyOf].find(Array.isArray) as unknown[] | undefined
  return !!variants?.some((v) => {
    const r = isObject(v) ? resolve(root, v) : undefined
    const props = r && isObject(r.properties) ? r.properties : undefined
    return !!props && ('ru' in props || 'kk' in props)
  })
}

function snippetFor(root: Node, node: Node, path: string): string {
  if (isLocalized(root, node)) return `{{tr ${path} "ru"}}`
  switch (node.format) {
    case 'date':
      return `{{date ${path}}}`
    case 'date-time':
      return `{{datetime ${path}}}`
    case 'kz-iin':
      return `{{iin ${path}}}`
    case 'kz-bin':
      return `{{bin ${path}}}`
  }
  const type = typeOf(node)
  if (type === 'boolean') return `{{check ${path}}}`
  if (type === 'number' || type === 'integer') return `{{number ${path}}}`
  return `{{${path}}}`
}

function walk(root: Node, node: Node, path: string, label: string, depth: number, out: SchemaField[]) {
  const props = isObject(node.properties) ? node.properties : {}
  const required = new Set(Array.isArray(node.required) ? (node.required as string[]) : [])

  for (const [name, raw] of Object.entries(props)) {
    if (!isObject(raw)) continue
    const child = resolve(root, raw)
    const childPath = `${path}.${name}`
    const childLabel = label ? `${label}.${name}` : name
    const type = typeOf(child)
    const title = typeof child.title === 'string' ? child.title : typeof child.description === 'string' ? child.description : undefined
    const base = { label: childLabel, title, required: required.has(name), depth }

    if (type === 'array' && isObject(child.items)) {
      out.push({
        ...base,
        path: childPath,
        type: 'array',
        snippet: `{{range $i, $item := ${childPath}}}\n  {{inc $i}}. \n{{else}}\n  Нет данных\n{{end}}`,
      })
      // Внутри range точка — текущий элемент, поэтому пути элементов начинаются заново.
      walk(root, resolve(root, child.items), '', `${childLabel}[]`, depth + 1, out)
    } else if (type === 'object' && isObject(child.properties) && !isLocalized(root, child)) {
      // Объект — заголовок группы: вставлять имеет смысл его поля.
      out.push({ ...base, path: childPath, type: 'object', snippet: '' })
      walk(root, child, childPath, childLabel, depth + 1, out)
    } else {
      out.push({ ...base, path: childPath, type: isLocalized(root, child) ? 'ru/kk' : type, snippet: snippetFor(root, child, childPath) })
    }
  }
}

/** Поля схемы по порядку, вложенные — за родителем. Невалидный JSON — пустой список. */
export function schemaFields(schemaText: string): SchemaField[] {
  let root: unknown
  try {
    root = JSON.parse(schemaText)
  } catch {
    return []
  }
  if (!isObject(root)) return []
  const out: SchemaField[] = []
  walk(root, root, '', '', 0, out)
  return out
}

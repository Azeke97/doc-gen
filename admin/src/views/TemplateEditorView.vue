<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router'

import { api, ApiError, type FieldError, type Template, type TemplateVersion } from '../api'
import CodeEditor from '../components/CodeEditor.vue'
import PdfDialog from '../components/PdfDialog.vue'
import { errorText, formatDateTime, prettyJSON } from '../labels'
import { schemaFields } from '../schemaFields'

const props = defineProps<{ id: string }>()

// Новый шаблон начинается с каркаса: A4, поля под колонтитул, шрифт с казахскими буквами.
const STARTER_HTML = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<title>Документ</title>
<style>
  @page { size: A4; margin: 20mm 15mm 20mm 20mm; }
  body { font-family: "Liberation Serif", serif; font-size: 12pt; }
</style>
</head>
<body>
  <h1>{{.title}}</h1>
</body>
</html>
`
const STARTER_SCHEMA = '{\n  "type": "object",\n  "required": ["title"],\n  "properties": {\n    "title": { "type": "string", "title": "Заголовок" }\n  }\n}\n'
const STARTER_DATA = '{\n  "title": "Пример"\n}\n'

const template = ref<Template | null>(null)
const base = ref<TemplateVersion | null>(null) // от какой версии начали
const draftNumber = ref<number | null>(null) // номер черновика, если он есть
const expectedDraft = ref('none')
const loading = ref(true)
const saving = ref(false)

const html = ref('')
const schema = ref('')
const testData = ref('')
const saved = ref({ html: '', schema: '', testData: '' })
const dirty = computed(() => html.value !== saved.value.html || schema.value !== saved.value.schema || testData.value !== saved.value.testData)

const error = ref('')
const fieldErrors = ref<FieldError[]>([])
const tab = ref('fields')
const htmlEditor = ref<InstanceType<typeof CodeEditor>>()
const pdfDialog = ref<InstanceType<typeof PdfDialog>>()

const fields = computed(() => schemaFields(schema.value))
const archived = computed(() => template.value?.status === 'archived')

const status = computed(() => {
  if (draftNumber.value !== null) return `Черновик версии ${draftNumber.value}`
  if (base.value) return `Новая версия на основе версии ${base.value.version}`
  return 'Первая версия'
})

async function load() {
  loading.value = true
  try {
    const [t, versions] = await Promise.all([api.template(props.id), api.versions(props.id)])
    template.value = t
    const sorted = [...versions].sort((a, b) => b.version - a.version)
    const draft = sorted.find((v) => v.status === 'draft')
    base.value = draft ?? sorted[0] ?? null
    draftNumber.value = draft?.version ?? null
    expectedDraft.value = draft?.updated_at ?? 'none'

    if (base.value) {
      const { blob } = await api.versionFile(base.value.id)
      html.value = await blob.text()
      schema.value = prettyJSON(base.value.data_schema)
      testData.value = prettyJSON(base.value.test_data)
    } else {
      html.value = STARTER_HTML
      schema.value = STARTER_SCHEMA
      testData.value = STARTER_DATA
    }
    // Каркас нового шаблона ещё не сохранён — считаем его правкой.
    saved.value = base.value ? { html: html.value, schema: schema.value, testData: testData.value } : { html: '', schema: '', testData: '' }
  } catch (e) {
    ElMessage.error(errorText(e))
  } finally {
    loading.value = false
  }
}

function checkJSON(text: string, what: string, optional: boolean): boolean {
  if (optional && !text.trim()) return true
  try {
    JSON.parse(text)
    return true
  } catch (e) {
    error.value = `${what}: это не JSON (${(e as Error).message})`
    return false
  }
}

/** Сохраняет правки черновиком. Возвращает черновик или null, если сохранить не удалось. */
async function save(): Promise<TemplateVersion | null> {
  if (!template.value || saving.value) return null
  error.value = ''
  fieldErrors.value = []
  if (!checkJSON(schema.value, 'Схема данных', false)) {
    tab.value = 'schema'
    return null
  }
  if (!checkJSON(testData.value, 'Тестовые данные', true)) {
    tab.value = 'data'
    return null
  }

  saving.value = true
  try {
    const file = new File([html.value], `${template.value.code}.html`, { type: 'text/html' })
    const v = await api.uploadVersion(template.value.id, file, schema.value, testData.value, expectedDraft.value)
    expectedDraft.value = v.updated_at
    draftNumber.value = v.version
    base.value = v
    saved.value = { html: html.value, schema: schema.value, testData: testData.value }
    ElMessage.success(`Черновик версии ${v.version} сохранён`)
    return v
  } catch (e) {
    if (e instanceof ApiError && e.code === 'draft_changed') {
      await ElMessageBox.alert(
        'Пока вы редактировали, черновик изменил другой пользователь (или его опубликовали). Ваши правки не сохранены и остаются в редакторе: скопируйте нужное, затем откройте редактор заново.',
        'Черновик изменился',
        { type: 'warning', confirmButtonText: 'Понятно' },
      ).catch(() => {})
      error.value = errorText(e)
    } else {
      error.value = errorText(e)
      if (e instanceof ApiError) fieldErrors.value = e.fieldErrors
    }
    return null
  } finally {
    saving.value = false
  }
}

const previewing = ref(false)

async function preview() {
  if (!testData.value.trim()) {
    error.value = 'Для предпросмотра нужны тестовые данные'
    tab.value = 'data'
    return
  }
  previewing.value = true
  try {
    // Предпросмотр рисует сохранённую версию, поэтому несохранённые правки сначала сохраняем.
    const v = dirty.value || !base.value ? await save() : base.value
    if (!v) return
    const { blob, filename } = await api.preview(v.id)
    const title = v.status === 'draft' ? `Предпросмотр черновика версии ${v.version}` : `Предпросмотр версии ${v.version}`
    pdfDialog.value?.show(blob, title, filename)
  } catch (e) {
    error.value = errorText(e)
  } finally {
    previewing.value = false
  }
}

function insertField(snippet: string) {
  if (snippet) htmlEditor.value?.insert(snippet)
}

// Несохранённые правки: спрашиваем при уходе со страницы и при закрытии вкладки.
async function confirmLeave() {
  if (!dirty.value) return true
  return ElMessageBox.confirm('Несохранённые правки пропадут.', 'Уйти из редактора?', {
    confirmButtonText: 'Уйти', cancelButtonText: 'Остаться', type: 'warning',
  }).then(() => true, () => false)
}
onBeforeRouteLeave(confirmLeave)
onBeforeRouteUpdate(confirmLeave)
function beforeUnload(e: BeforeUnloadEvent) {
  if (dirty.value) e.preventDefault()
}

onMounted(() => {
  window.addEventListener('beforeunload', beforeUnload)
  void load()
})
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
</script>

<template>
  <div v-loading="loading" class="editor-page">
    <div class="bar">
      <div class="bar-title">
        <el-breadcrumb>
          <el-breadcrumb-item :to="{ path: '/templates' }">Шаблоны</el-breadcrumb-item>
          <el-breadcrumb-item :to="{ path: `/templates/${id}` }">{{ template?.name }}</el-breadcrumb-item>
          <el-breadcrumb-item>Редактор</el-breadcrumb-item>
        </el-breadcrumb>
        <div class="state">
          <strong>{{ status }}</strong>
          <span v-if="draftNumber !== null && base" class="muted">· {{ base.created_by }}, {{ formatDateTime(base.updated_at) }}</span>
          <el-tag v-if="dirty" type="warning" size="small" disable-transitions>есть несохранённые правки</el-tag>
        </div>
      </div>
      <div class="bar-actions">
        <el-button :loading="previewing" :disabled="archived" @click="preview">Предпросмотр PDF</el-button>
        <el-button type="primary" :loading="saving" :disabled="archived || !dirty" @click="save">
          Сохранить черновик
        </el-button>
      </div>
    </div>

    <el-alert v-if="archived" type="info" :closable="false" show-icon title="Шаблон в архиве — изменять его нельзя." class="note" />
    <el-alert v-if="error" type="error" show-icon :title="error" class="note" @close="error = ''">
      <ul v-if="fieldErrors.length" class="field-errors">
        <li v-for="(fe, i) in fieldErrors" :key="i"><span class="mono">{{ fe.field || '(корень)' }}</span> — {{ fe.message }}</li>
      </ul>
    </el-alert>

    <div v-if="!loading && template" class="panes">
      <section class="pane-main">
        <div class="pane-label">HTML-шаблон <span class="muted">· Ctrl+S — сохранить</span></div>
        <div class="pane-body">
          <CodeEditor ref="htmlEditor" v-model="html" language="html" :readonly="archived" @save="save" />
        </div>
      </section>

      <section class="pane-side">
        <el-tabs v-model="tab" class="side-tabs">
          <el-tab-pane label="Поля" name="fields">
            <p class="muted small">Нажмите на поле — вставка появится в месте курсора.</p>
            <p v-if="!fields.length" class="muted small">В схеме нет полей или она не разбирается как JSON.</p>
            <ul class="fields">
              <li
                v-for="f in fields"
                :key="f.label"
                :class="{ group: !f.snippet }"
                :style="{ paddingLeft: 8 + f.depth * 16 + 'px' }"
                :title="f.snippet || undefined"
                @click="insertField(f.snippet)"
              >
                <span class="mono">{{ f.label.split('.').pop() }}</span>
                <span v-if="f.required" class="req" title="обязательное">*</span>
                <span class="type">{{ f.type }}</span>
                <div v-if="f.title" class="muted small">{{ f.title }}</div>
              </li>
            </ul>
          </el-tab-pane>
          <el-tab-pane label="Схема" name="schema">
            <div class="json-pane"><CodeEditor v-model="schema" language="json" :readonly="archived" @save="save" /></div>
          </el-tab-pane>
          <el-tab-pane label="Данные" name="data">
            <div class="json-pane"><CodeEditor v-model="testData" language="json" :readonly="archived" @save="save" /></div>
          </el-tab-pane>
          <el-tab-pane label="Справка" name="help">
            <div class="help">
              <p><code v-pre>{{.field}}</code> — значение, <code v-pre>{{tr .name "kk"}}</code> — перевод из <code>{"ru","kk"}</code>.</p>
              <p><code v-pre>{{money .sum}}</code> → 6 000 000.00, <code v-pre>{{number .n}}</code> → 1 500.</p>
              <p><code v-pre>{{date .d}}</code>, <code v-pre>{{datetime .d}}</code>, <code v-pre>{{longdate .d "ru"}}</code> — время Астаны.</p>
              <p><code v-pre>{{words .n "ru"}}</code>, <code v-pre>{{moneywords .sum "kk"}}</code> — прописью; <code v-pre>{{capfirst ...}}</code>.</p>
              <p><code v-pre>{{iin .x}}</code>, <code v-pre>{{bin .x}}</code> — с проверкой; <code v-pre>{{check .flag}}</code> → ☒ / ☐.</p>
              <p><code v-pre>{{range $i, $x := .list}}{{inc $i}}{{else}}нет{{end}}</code> — список; <code v-pre>{{if gt .count 0}}…{{end}}</code> — условие.</p>
              <p><code v-pre>{{docid}}</code>, <code v-pre>{{verifyurl}}</code>, <code v-pre>&lt;img src="{{qr}}"&gt;</code> — идентификатор, ссылка и QR проверки.</p>
              <p>Страница — CSS <code>@page</code> (без неё A4); нижнее поле не меньше 15 мм — там колонтитул. Обязателен <code>&lt;title&gt;</code>. Картинки — только <code>data:</code>, шрифты: Liberation, DejaVu, Noto.</p>
            </div>
          </el-tab-pane>
        </el-tabs>
      </section>
    </div>

    <PdfDialog ref="pdfDialog" />
  </div>
</template>

<style scoped>
.editor-page {
  padding: 16px;
  height: calc(100vh - 61px);
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.bar {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.state {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 8px;
}

.bar-actions {
  display: flex;
  gap: 8px;
}

.note {
  flex: none;
}

.field-errors {
  margin: 4px 0 0;
  padding-left: 18px;
}

.panes {
  flex: 1;
  min-height: 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 380px;
  gap: 16px;
}

.pane-main {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.pane-label {
  font-size: 13px;
  margin-bottom: 6px;
}

.pane-body {
  flex: 1;
  min-height: 0;
}

.pane-side {
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: #fff;
  border: 1px solid var(--el-border-color-light);
  border-radius: 4px;
  padding: 0 12px 12px;
}

.side-tabs {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.side-tabs :deep(.el-tabs__content) {
  flex: 1;
  min-height: 0;
  overflow: auto;
}

.side-tabs :deep(.el-tab-pane) {
  height: 100%;
}

.json-pane {
  height: 100%;
  min-height: 300px;
}

.fields {
  list-style: none;
  margin: 0;
  padding: 0;
}

.fields li {
  padding: 6px 8px;
  border-radius: 4px;
  cursor: pointer;
}

.fields li:hover {
  background: var(--el-fill-color-light);
}

.fields li.group {
  cursor: default;
  font-weight: 600;
}

.fields li.group:hover {
  background: none;
}

.req {
  color: var(--el-color-danger);
  margin-left: 2px;
}

.type {
  margin-left: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.small {
  font-size: 12px;
  margin: 2px 0 0;
}

.help p {
  font-size: 13px;
  line-height: 1.6;
  margin: 0 0 10px;
}

.help code {
  font-family: ui-monospace, "SFMono-Regular", Menlo, monospace;
  font-size: 12px;
  background: var(--el-fill-color-light);
  padding: 1px 4px;
  border-radius: 3px;
}

@media (max-width: 900px) {
  .panes {
    grid-template-columns: 1fr;
  }

  .editor-page {
    height: auto;
  }

  .pane-body {
    height: 60vh;
  }
}
</style>

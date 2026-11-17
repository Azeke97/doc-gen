<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

import { api, ApiError, saveBlob, type Project, type SchemaDiff, type Template, type TemplateVersion } from '../api'
import SchemaChanges from '../components/SchemaChanges.vue'
import UploadVersionDialog from '../components/UploadVersionDialog.vue'
import { errorText, formatDateTime, localeLabels, prettyJSON, templateStatus, versionStatus } from '../labels'

const props = defineProps<{ id: string }>()

const template = ref<Template | null>(null)
const project = ref<Project | null>(null)
const versions = ref<TemplateVersion[]>([])
const loading = ref(true)
const busy = ref('') // id версии, над которой идёт действие

const archived = computed(() => template.value?.status === 'archived')
const draft = computed(() => versions.value.find((v) => v.status === 'draft'))
const latest = computed(() => versions.value[0])
const active = computed(() => versions.value.find((v) => v.id === template.value?.active_version_id))

async function load() {
  try {
    const [t, list, projects] = await Promise.all([api.template(props.id), api.versions(props.id), api.projects()])
    template.value = t
    versions.value = [...list].sort((a, b) => b.version - a.version)
    project.value = projects.find((p) => p.code === t.project) ?? null
  } catch (e) {
    ElMessage.error(errorText(e))
  } finally {
    loading.value = false
  }
}

// ---- ломающие изменения схемы: показать и повторить с подтверждением ----

const breaking = ref<{ diff: SchemaDiff; title: string; retry: () => Promise<void> } | null>(null)
const breakingVisible = ref(false)

async function withBreakingCheck(title: string, run: (confirm: boolean) => Promise<void>) {
  try {
    await run(false)
  } catch (e) {
    if (e instanceof ApiError && e.diff) {
      breaking.value = { diff: e.diff, title, retry: () => run(true) }
      breakingVisible.value = true
      return
    }
    throw e
  }
}

async function confirmBreaking() {
  if (!breaking.value) return
  const { retry } = breaking.value
  breakingVisible.value = false
  try {
    await retry()
  } catch (e) {
    ElMessage.error(errorText(e))
  }
}

// ---- действия с версиями ----

async function act(v: TemplateVersion, fn: () => Promise<void>) {
  busy.value = v.id
  try {
    await fn()
  } catch (e) {
    ElMessage.error(errorText(e))
  } finally {
    busy.value = ''
  }
}

async function publish(v: TemplateVersion) {
  const ok = await ElMessageBox.confirm(
    `Опубликованную версию ${v.version} больше нельзя будет изменить.`,
    'Опубликовать версию?',
    { confirmButtonText: 'Опубликовать', cancelButtonText: 'Отмена', type: 'warning' },
  ).then(() => true, () => false)
  if (!ok) return
  await act(v, () =>
    withBreakingCheck(`Публикация версии ${v.version}`, async (confirm) => {
      await api.publish(v.id, confirm)
      ElMessage.success(`Версия ${v.version} опубликована`)
      await load()
      await offerActivate(v)
    }),
  )
}

async function offerActivate(v: TemplateVersion) {
  const ok = await ElMessageBox.confirm(
    `Порталы начнут выпускать документы по версии ${v.version}.`,
    'Сделать её активной сейчас?',
    { confirmButtonText: 'Сделать активной', cancelButtonText: 'Позже', type: 'info' },
  ).then(() => true, () => false)
  if (ok) await doActivate(v)
}

async function doActivate(v: TemplateVersion) {
  await withBreakingCheck(`Активация версии ${v.version}`, async (confirm) => {
    template.value = await api.activate(props.id, v.id, confirm)
    ElMessage.success(`Активна версия ${v.version}`)
  })
}

function activate(v: TemplateVersion) {
  const rollback = active.value && v.version < active.value.version
  return act(v, async () => {
    const ok = await ElMessageBox.confirm(
      `Порталы начнут выпускать документы по версии ${v.version}.`,
      rollback ? `Откатить на версию ${v.version}?` : `Сделать версию ${v.version} активной?`,
      { confirmButtonText: rollback ? 'Откатить' : 'Сделать активной', cancelButtonText: 'Отмена', type: 'warning' },
    ).then(() => true, () => false)
    if (ok) await doActivate(v)
  })
}

async function archive() {
  const ok = await ElMessageBox.confirm(
    'Порталы больше не смогут выпускать документы по этому шаблону. Уже выпущенные документы останутся. Вернуть шаблон из архива нельзя.',
    'Перенести шаблон в архив?',
    { confirmButtonText: 'В архив', cancelButtonText: 'Отмена', type: 'warning', confirmButtonClass: 'el-button--danger' },
  ).then(() => true, () => false)
  if (!ok) return
  try {
    template.value = await api.archive(props.id)
    ElMessage.success('Шаблон в архиве')
  } catch (e) {
    ElMessage.error(errorText(e))
  }
}

// ---- просмотр: PDF, схема, изменения ----

const pdf = ref<{ url: string; title: string; filename: string } | null>(null)
const pdfVisible = ref(false)

function preview(v: TemplateVersion) {
  return act(v, async () => {
    const { blob, filename } = await api.preview(v.id)
    if (pdf.value) URL.revokeObjectURL(pdf.value.url)
    pdf.value = { url: URL.createObjectURL(blob), title: `Предпросмотр версии ${v.version} на тестовых данных`, filename }
    pdfVisible.value = true
  })
}

function download(v: TemplateVersion) {
  return act(v, async () => {
    const { blob, filename } = await api.versionFile(v.id)
    saveBlob(blob, filename)
  })
}

const details = ref<TemplateVersion | null>(null)
const detailsTab = ref('schema')
const detailsVisible = ref(false)

function showDetails(v: TemplateVersion) {
  details.value = v
  detailsTab.value = 'schema'
  detailsVisible.value = true
}

const diff = ref<{ version: number; diff: SchemaDiff } | null>(null)
const diffVisible = ref(false)

function showDiff(v: TemplateVersion) {
  return act(v, async () => {
    diff.value = { version: v.version, diff: await api.diff(v.id) }
    diffVisible.value = true
  })
}

const uploadVisible = ref(false)

onMounted(load)
onBeforeUnmount(() => {
  if (pdf.value) URL.revokeObjectURL(pdf.value.url)
})
</script>

<template>
  <div v-loading="loading" class="page">
    <el-breadcrumb class="crumbs">
      <el-breadcrumb-item :to="{ path: '/templates' }">Шаблоны</el-breadcrumb-item>
      <el-breadcrumb-item>{{ template?.name }}</el-breadcrumb-item>
    </el-breadcrumb>

    <template v-if="template">
      <div class="page-head">
        <div>
          <h1>
            {{ template.name }}
            <el-tag :type="templateStatus[template.status].type" disable-transitions class="status">
              {{ templateStatus[template.status].label }}
            </el-tag>
          </h1>
        </div>
        <div v-if="!archived" class="actions">
          <el-button type="primary" @click="uploadVisible = true">
            {{ draft ? 'Заменить черновик' : 'Загрузить версию' }}
          </el-button>
          <el-button @click="archive">В архив</el-button>
        </div>
      </div>

      <el-descriptions :column="3" border class="info">
        <el-descriptions-item label="Код"><span class="mono">{{ template.code }}</span></el-descriptions-item>
        <el-descriptions-item label="Язык">{{ localeLabels[template.locale] }}</el-descriptions-item>
        <el-descriptions-item label="Проект">{{ project?.name ?? template.project }}</el-descriptions-item>
        <el-descriptions-item label="Активная версия">{{ active ? active.version : '—' }}</el-descriptions-item>
        <el-descriptions-item label="Формат">{{ template.engine === 'html' ? 'HTML → PDF' : 'DOCX' }}</el-descriptions-item>
        <el-descriptions-item label="Хранение документов">{{ template.retention_days }} дн.</el-descriptions-item>
      </el-descriptions>

      <el-alert
        v-if="!archived && !active"
        type="info"
        show-icon
        :closable="false"
        class="hint"
        :title="versions.length ? 'Опубликуйте версию и сделайте её активной — тогда порталы смогут выпускать документы.' : 'Загрузите первую версию: файл шаблона, схему данных и тестовые данные.'"
      />

      <h2>Версии</h2>
      <el-table :data="versions" empty-text="Версий пока нет">
        <el-table-column label="№" width="130">
          <template #default="{ row }">
            <strong>{{ row.version }}</strong>
            <el-tag v-if="row.id === template.active_version_id" type="success" effect="dark" size="small" disable-transitions class="active-tag">
              активная
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Статус" width="140">
          <template #default="{ row }">
            <el-tag :type="versionStatus[row.status as TemplateVersion['status']].type" disable-transitions>
              {{ versionStatus[row.status as TemplateVersion['status']].label }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="Загрузил" min-width="170">
          <template #default="{ row }">
            <div>{{ row.created_by }}</div>
            <!-- Публикация меняет updated_at, поэтому у опубликованной версии время загрузки — created_at. -->
            <div class="muted small">{{ formatDateTime(row.status === 'draft' ? row.updated_at : row.created_at) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="Опубликовал" min-width="170">
          <template #default="{ row }">
            <template v-if="row.published_by">
              <div>{{ row.published_by }}</div>
              <div class="muted small">{{ formatDateTime(row.published_at) }}</div>
            </template>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="" min-width="330" align="right">
          <template #default="{ row }">
            <div class="row-actions">
              <el-button
                v-if="!archived && row.status === 'draft'"
                type="primary"
                size="small"
                :loading="busy === row.id"
                @click="publish(row)"
              >
                Опубликовать
              </el-button>
              <el-button
                v-if="!archived && row.status === 'published' && row.id !== template.active_version_id"
                size="small"
                :loading="busy === row.id"
                @click="activate(row)"
              >
                {{ active && row.version < active.version ? 'Откатить на неё' : 'Сделать активной' }}
              </el-button>
              <el-button size="small" :disabled="template.engine !== 'html' || !row.test_data" @click="preview(row)">
                Предпросмотр
              </el-button>
              <el-dropdown trigger="click">
                <el-button size="small">Ещё ▾</el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item @click="showDetails(row)">Схема и тестовые данные</el-dropdown-item>
                    <el-dropdown-item @click="showDiff(row)">Изменения схемы</el-dropdown-item>
                    <el-dropdown-item @click="download(row)">Скачать файл шаблона</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </template>

    <UploadVersionDialog
      v-if="template"
      v-model="uploadVisible"
      :template-id="template.id"
      :schema="prettyJSON(latest?.data_schema)"
      :test-data="prettyJSON(latest?.test_data)"
      :has-draft="!!draft"
      @uploaded="load"
    />

    <el-dialog v-model="breakingVisible" :title="breaking?.title" width="min(760px, 94vw)">
      <el-alert
        type="error"
        show-icon
        :closable="false"
        title="В схеме есть ломающие изменения"
        description="Порталы, которые отправляют данные по прежней схеме, начнут получать ошибку invalid_data. Продолжайте, только если порталы уже обновлены."
        class="hint"
      />
      <p v-if="breaking?.diff.base_version" class="muted">Сравнение с версией {{ breaking.diff.base_version }}, по которой порталы работают сейчас.</p>
      <SchemaChanges v-if="breaking" :changes="breaking.diff.changes" />
      <template #footer>
        <el-button @click="breakingVisible = false">Отмена</el-button>
        <el-button type="danger" @click="confirmBreaking">Всё равно продолжить</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="diffVisible" :title="`Изменения схемы в версии ${diff?.version}`" width="min(760px, 94vw)">
      <p v-if="diff" class="muted">
        {{ diff.diff.base_version ? `Сравнение с версией ${diff.diff.base_version}, по которой порталы работают сейчас.` : 'Сравнивать не с чем: активной версии ещё нет.' }}
      </p>
      <SchemaChanges v-if="diff" :changes="diff.diff.changes" />
    </el-dialog>

    <el-dialog v-model="detailsVisible" :title="`Версия ${details?.version}`" width="min(820px, 94vw)">
      <el-tabs v-model="detailsTab">
        <el-tab-pane label="Схема данных" name="schema">
          <pre class="json">{{ prettyJSON(details?.data_schema) }}</pre>
        </el-tab-pane>
        <el-tab-pane label="Тестовые данные" name="data">
          <pre v-if="details?.test_data" class="json">{{ prettyJSON(details.test_data) }}</pre>
          <p v-else class="muted">Тестовых данных нет.</p>
        </el-tab-pane>
      </el-tabs>
      <p class="muted small">SHA-256 файла: <span class="mono">{{ details?.file_sha256 }}</span></p>
    </el-dialog>

    <el-dialog v-model="pdfVisible" :title="pdf?.title" width="min(1100px, 96vw)" top="3vh" class="pdf-dialog">
      <iframe v-if="pdf" :src="pdf.url" class="pdf" title="PDF" />
      <template #footer>
        <el-button v-if="pdf" tag="a" :href="pdf.url" :download="pdf.filename">Скачать PDF</el-button>
        <el-button type="primary" @click="pdfVisible = false">Закрыть</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.crumbs {
  margin-bottom: 16px;
}

h1 {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

h2 {
  margin: 28px 0 12px;
  font-size: 18px;
  font-weight: 600;
}

.status {
  font-weight: normal;
}

.actions {
  display: flex;
  gap: 8px;
}

.info {
  background: #fff;
}

.hint {
  margin: 16px 0;
}

.active-tag {
  margin-left: 8px;
}

.small {
  font-size: 12px;
}

.row-actions {
  display: inline-flex;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.row-actions .el-button + .el-button {
  margin-left: 0;
}

.pdf {
  width: 100%;
  height: 75vh;
  border: 1px solid var(--el-border-color-light);
  border-radius: 4px;
}
</style>

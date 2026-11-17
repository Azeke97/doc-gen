<script setup lang="ts">
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { api, ApiError, type Engine, type Locale, type Project, type Template } from '../api'
import { formatDateTime, localeLabels, templateStatus } from '../labels'

const route = useRoute()
const router = useRouter()

const projects = ref<Project[]>([])
const templates = ref<Template[]>([])
const loading = ref(false)
const search = ref('')

// Фильтр по проекту хранится в адресе: ссылку можно отправить коллеге.
const project = computed({
  get: () => (typeof route.query.project === 'string' ? route.query.project : ''),
  set: (v: string) => void router.replace({ query: v ? { project: v } : {} }),
})

const projectNames = computed(() => Object.fromEntries(projects.value.map((p) => [p.code, p.name])))

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return templates.value
  return templates.value.filter((t) => t.name.toLowerCase().includes(q) || t.code.includes(q))
})

async function load() {
  loading.value = true
  try {
    templates.value = await api.templates(project.value || undefined)
  } catch (e) {
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

const dialog = ref(false)
const saving = ref(false)
const formRef = ref<FormInstance>()
const form = reactive({
  project: '',
  code: '',
  locale: 'bilingual' as Locale,
  name: '',
  engine: 'html' as Engine,
  retention_days: 1825,
})
const rules: FormRules = {
  project: [{ required: true, message: 'Выберите проект', trigger: 'change' }],
  code: [
    { required: true, message: 'Укажите код', trigger: 'blur' },
    { pattern: /^[a-z][a-z0-9_]{1,99}$/, message: 'Латинские строчные буквы, цифры и _, от 2 до 100 символов', trigger: 'blur' },
  ],
  name: [{ required: true, message: 'Укажите название', trigger: 'blur' }],
}

function openCreate() {
  Object.assign(form, { project: project.value, code: '', locale: 'bilingual', name: '', engine: 'html', retention_days: 1825 })
  dialog.value = true
}

async function create() {
  if (!(await formRef.value?.validate().catch(() => false))) return
  saving.value = true
  try {
    const t = await api.createTemplate({ ...form })
    dialog.value = false
    ElMessage.success('Шаблон создан, загрузите первую версию')
    await router.push(`/templates/${t.id}`)
  } catch (e) {
    ElMessage.error(e instanceof ApiError && e.code === 'template_exists' ? 'Такой шаблон (проект, код, язык) уже есть' : (e as Error).message)
  } finally {
    saving.value = false
  }
}

watch(project, load)
onMounted(async () => {
  projects.value = await api.projects().catch(() => [])
  await load()
})
</script>

<template>
  <div class="page">
    <div class="page-head">
      <h1>Шаблоны</h1>
      <el-button type="primary" :disabled="projects.length === 0" @click="openCreate">Новый шаблон</el-button>
    </div>

    <div class="filters">
      <el-select v-model="project" placeholder="Все проекты" clearable class="filter">
        <el-option v-for="p in projects" :key="p.code" :label="p.name" :value="p.code" />
      </el-select>
      <el-input v-model="search" placeholder="Поиск по названию или коду" clearable class="filter" />
    </div>

    <el-table
      v-loading="loading"
      :data="filtered"
      class="clickable-rows"
      :empty-text="projects.length === 0 ? 'Сначала создайте проект' : 'Шаблонов нет'"
      @row-click="(row: Template) => router.push(`/templates/${row.id}`)"
    >
      <el-table-column label="Название" min-width="240">
        <template #default="{ row }">
          <div>{{ row.name }}</div>
          <div class="mono muted">{{ row.code }}</div>
        </template>
      </el-table-column>
      <el-table-column label="Язык" width="180">
        <template #default="{ row }">{{ localeLabels[row.locale as Locale] }}</template>
      </el-table-column>
      <el-table-column label="Проект" min-width="160">
        <template #default="{ row }">{{ projectNames[row.project] ?? row.project }}</template>
      </el-table-column>
      <el-table-column label="Статус" width="130">
        <template #default="{ row }">
          <el-tag :type="templateStatus[row.status as Template['status']].type" disable-transitions>
            {{ templateStatus[row.status as Template['status']].label }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="Изменён" width="160">
        <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialog" title="Новый шаблон" width="min(520px, 92vw)">
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="create">
        <el-form-item label="Проект" prop="project">
          <el-select v-model="form.project" placeholder="Выберите проект">
            <el-option v-for="p in projects" :key="p.code" :label="p.name" :value="p.code" />
          </el-select>
        </el-form-item>
        <el-form-item label="Название" prop="name">
          <el-input v-model="form.name" placeholder="Протокол вскрытия" />
        </el-form-item>
        <el-form-item label="Код" prop="code">
          <el-input v-model="form.code" placeholder="opening_protocol" />
          <div class="hint">По коду портал выбирает шаблон, менять его потом нельзя.</div>
        </el-form-item>
        <el-form-item label="Язык документа">
          <el-radio-group v-model="form.locale">
            <el-radio-button v-for="(label, value) in localeLabels" :key="value" :value="value">{{ label }}</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="Формат шаблона">
          <el-radio-group v-model="form.engine">
            <el-radio-button value="html">HTML → PDF</el-radio-button>
            <el-radio-button value="docx" disabled>DOCX (позже)</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="Срок хранения документов, дней">
          <el-input-number v-model="form.retention_days" :min="1" :max="36500" />
          <div class="hint">После этого срока выпущенные документы удаляются вместе с файлами.</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">Отмена</el-button>
        <el-button type="primary" :loading="saving" @click="create">Создать</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.filters {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}

.filter {
  width: 280px;
  max-width: 100%;
}

.hint {
  width: 100%;
  font-size: 12px;
  line-height: 1.4;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}
</style>

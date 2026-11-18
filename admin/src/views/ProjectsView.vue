<script setup lang="ts">
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { onMounted, reactive, ref } from 'vue'

import { api, ApiError, type Project } from '../api'
import { errorText, formatDateTime } from '../labels'
import KeysDialog from './KeysDialog.vue'

const projects = ref<Project[]>([])
const keysOpen = ref(false)
const keysProject = ref<Project | null>(null)
const loading = ref(false)

const dialog = ref(false)
const editing = ref<Project | null>(null) // null — создаём новый проект
const saving = ref(false)
const formRef = ref<FormInstance>()
const form = reactive({ code: '', name: '', retention_days: 365 })
const rules: FormRules = {
  code: [
    { required: true, message: 'Укажите код', trigger: 'blur' },
    { pattern: /^[a-z][a-z0-9_]{1,49}$/, message: 'Латинские строчные буквы, цифры и _, от 2 до 50 символов', trigger: 'blur' },
  ],
  name: [{ required: true, message: 'Укажите название', trigger: 'blur' }],
}

async function load() {
  loading.value = true
  try {
    projects.value = await api.projects()
  } catch (e) {
    ElMessage.error(errorText(e))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  Object.assign(form, { code: '', name: '', retention_days: 365 })
  dialog.value = true
}

function openKeys(p: Project) {
  keysProject.value = p
  keysOpen.value = true
}

function openEdit(p: Project) {
  editing.value = p
  Object.assign(form, { code: p.code, name: p.name, retention_days: p.retention_days })
  dialog.value = true
}

async function save() {
  if (!(await formRef.value?.validate().catch(() => false))) return
  saving.value = true
  try {
    if (editing.value) {
      await api.updateProject(editing.value.code, { name: form.name.trim(), retention_days: form.retention_days })
      ElMessage.success('Проект изменён')
    } else {
      await api.createProject({ ...form })
      ElMessage.success('Проект создан')
    }
    dialog.value = false
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError && e.code === 'project_exists' ? 'Проект с таким кодом уже есть' : errorText(e))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="page-head">
      <h1>Проекты</h1>
      <el-button type="primary" @click="openCreate">Новый проект</el-button>
    </div>
    <p class="muted">Проект — портал, который выпускает документы: у него свои API-ключи, срок хранения документов и записи в журнале.</p>

    <el-table v-loading="loading" :data="projects" empty-text="Проектов пока нет">
      <el-table-column prop="code" label="Код" width="200">
        <template #default="{ row }"><span class="mono">{{ row.code }}</span></template>
      </el-table-column>
      <el-table-column prop="name" label="Название" min-width="200" />
      <el-table-column label="Срок хранения" width="160">
        <template #default="{ row }">{{ row.retention_days }} дн.</template>
      </el-table-column>
      <el-table-column label="Создан" width="180">
        <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="" width="190" align="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openKeys(row)">Ключи</el-button>
          <el-button link type="primary" @click="openEdit(row)">Изменить</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialog" :title="editing ? `Проект ${editing.code}` : 'Новый проект'" width="min(480px, 92vw)">
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="save">
        <el-form-item v-if="!editing" label="Код" prop="code">
          <el-input v-model="form.code" placeholder="procurement" />
        </el-form-item>
        <el-form-item label="Название" prop="name">
          <el-input v-model="form.name" placeholder="Портал закупок" />
        </el-form-item>
        <el-form-item label="Срок хранения документов, дней">
          <el-input-number v-model="form.retention_days" :min="1" :max="36500" />
          <div class="hint">
            Наибольший срок, который портал может запросить для документа. После него документ удаляется
            вместе с файлом и входными данными.<template v-if="editing"> Выпущенные документы сохраняют свой срок.</template>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">Отмена</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ editing ? 'Сохранить' : 'Создать' }}</el-button>
      </template>
    </el-dialog>

    <KeysDialog v-model="keysOpen" :project="keysProject" />
  </div>
</template>

<style scoped>
.hint {
  width: 100%;
  font-size: 12px;
  line-height: 1.4;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}
</style>

<script setup lang="ts">
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { onMounted, reactive, ref } from 'vue'

import { api, ApiError, type Project } from '../api'
import { formatDateTime } from '../labels'

const projects = ref<Project[]>([])
const loading = ref(false)

const dialog = ref(false)
const saving = ref(false)
const formRef = ref<FormInstance>()
const form = reactive({ code: '', name: '' })
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
    ElMessage.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

function openCreate() {
  form.code = ''
  form.name = ''
  dialog.value = true
}

async function create() {
  if (!(await formRef.value?.validate().catch(() => false))) return
  saving.value = true
  try {
    await api.createProject({ ...form })
    dialog.value = false
    ElMessage.success('Проект создан')
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError && e.code === 'project_exists' ? 'Проект с таким кодом уже есть' : (e as Error).message)
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
    <p class="muted">Проект — портал, который выпускает документы. Шаблоны и API-ключи принадлежат проекту.</p>

    <el-table v-loading="loading" :data="projects" empty-text="Проектов пока нет">
      <el-table-column prop="code" label="Код" width="220">
        <template #default="{ row }"><span class="mono">{{ row.code }}</span></template>
      </el-table-column>
      <el-table-column prop="name" label="Название" />
      <el-table-column label="Создан" width="180">
        <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialog" title="Новый проект" width="min(480px, 92vw)">
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="create">
        <el-form-item label="Код" prop="code">
          <el-input v-model="form.code" placeholder="procurement" />
        </el-form-item>
        <el-form-item label="Название" prop="name">
          <el-input v-model="form.name" placeholder="Портал закупок" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">Отмена</el-button>
        <el-button type="primary" :loading="saving" @click="create">Создать</el-button>
      </template>
    </el-dialog>
  </div>
</template>

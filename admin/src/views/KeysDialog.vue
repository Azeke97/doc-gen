<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { ref, watch } from 'vue'

import { api, ApiError, type ApiKey, type IssuedKey, type Project } from '../api'
import { errorText, formatDateTime } from '../labels'

const props = defineProps<{ project: Project | null }>()
const open = defineModel<boolean>({ required: true })

const keys = ref<ApiKey[]>([])
const loading = ref(false)
const name = ref('')
const issuing = ref(false)
const issued = ref<IssuedKey | null>(null)

async function load() {
  if (!props.project) return
  loading.value = true
  try {
    keys.value = await api.keys(props.project.code)
  } catch (e) {
    ElMessage.error(errorText(e))
  } finally {
    loading.value = false
  }
}

// Каждое открытие — свежий список; выданный ранее ключ больше не показываем.
watch(open, (v) => {
  if (!v) return
  issued.value = null
  name.value = props.project?.name ?? ''
  void load()
})

async function issue() {
  if (!props.project || !name.value.trim()) return
  issuing.value = true
  try {
    issued.value = await api.issueKey(props.project.code, name.value.trim())
    await load()
  } catch (e) {
    ElMessage.error(e instanceof ApiError && e.code === 'invalid_name' ? 'Укажите название ключа (до 200 символов)' : errorText(e))
  } finally {
    issuing.value = false
  }
}

async function copy() {
  if (!issued.value) return
  try {
    await navigator.clipboard.writeText(issued.value.key)
    ElMessage.success('Ключ скопирован')
  } catch {
    ElMessage.warning('Не удалось скопировать: выделите ключ и скопируйте вручную')
  }
}

async function revoke(k: ApiKey) {
  try {
    await ElMessageBox.confirm(
      `Портал с ключом ${k.key_prefix}… сразу перестанет выпускать документы. Отменить отзыв нельзя.`,
      `Отозвать ключ «${k.name}»?`,
      { confirmButtonText: 'Отозвать', cancelButtonText: 'Отмена', type: 'warning' },
    )
  } catch {
    return
  }
  try {
    await api.revokeKey(k.id)
    if (issued.value?.id === k.id) issued.value = null
    ElMessage.success('Ключ отозван')
  } catch (e) {
    ElMessage.error(e instanceof ApiError && e.code === 'key_not_found' ? 'Ключ уже отозван' : errorText(e))
  }
  await load()
}
</script>

<template>
  <el-dialog v-model="open" :title="project ? `Ключи: ${project.name}` : 'Ключи'" width="min(760px, 94vw)">
    <el-alert v-if="issued" type="success" :closable="false" class="issued">
      <template #title>Ключ выдан. Скопируйте его сейчас — больше он не покажется.</template>
      <div class="key-row">
        <el-input :model-value="issued.key" readonly class="mono" @focus="($event.target as HTMLInputElement).select()" />
        <el-button type="primary" @click="copy">Скопировать</el-button>
      </div>
      <div class="hint">Портал передаёт его в заголовке <span class="mono">X-API-Key</span>.</div>
    </el-alert>

    <el-form class="issue" @submit.prevent="issue">
      <el-input v-model="name" placeholder="Название, например «Портал закупок, тест»" maxlength="200" />
      <el-button type="primary" :loading="issuing" :disabled="!name.trim()" native-type="submit">Выдать ключ</el-button>
    </el-form>

    <el-table v-loading="loading" :data="keys" empty-text="Ключей пока нет">
      <el-table-column label="Ключ" width="130">
        <template #default="{ row }"><span class="mono">{{ row.key_prefix }}…</span></template>
      </el-table-column>
      <el-table-column prop="name" label="Название" min-width="180" />
      <el-table-column label="Выдан" width="170">
        <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="Статус" width="170">
        <template #default="{ row }">
          <el-tag v-if="!row.revoked_at" type="success" size="small">Действует</el-tag>
          <span v-else class="muted">Отозван {{ formatDateTime(row.revoked_at) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="" width="100" align="right">
        <template #default="{ row }">
          <el-button v-if="!row.revoked_at" link type="danger" @click="revoke(row)">Отозвать</el-button>
        </template>
      </el-table-column>
    </el-table>
  </el-dialog>
</template>

<style scoped>
.issued {
  margin-bottom: 16px;
}
.key-row {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}
.issue {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}
.hint {
  margin-top: 6px;
  font-size: 12px;
}
</style>

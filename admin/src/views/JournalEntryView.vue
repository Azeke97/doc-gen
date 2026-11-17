<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { computed, onMounted, ref } from 'vue'

import { api, type JournalCard } from '../api'
import { errorLabels, errorText, formatDateTime, formatSize, prettyJSON, statusLabels } from '../labels'

const props = defineProps<{ id: string }>()

const card = ref<JournalCard | null>(null)
const loading = ref(true)

const entry = computed(() => card.value?.entry)
// Ошибки данных — по полям (field), ошибки шаблона — по меткам (part, context).
const templateProblems = computed(() => entry.value?.error_code === 'invalid_template' && entry.value.error_details?.some((d) => d.part !== undefined))

const documentState = computed(() => {
  switch (card.value?.document_state) {
    case 'available':
      return 'Хранится'
    case 'expired':
      return 'Срок хранения истёк, документ будет удалён при очистке'
    case 'deleted':
      return 'Удалён по истечении срока хранения'
    default:
      return 'Документ не выпускался'
  }
})

onMounted(async () => {
  try {
    card.value = await api.journalEntry(props.id)
  } catch (e) {
    ElMessage.error(errorText(e))
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div v-loading="loading" class="page">
    <el-breadcrumb class="crumbs">
      <el-breadcrumb-item :to="{ path: '/journal' }">Журнал</el-breadcrumb-item>
      <el-breadcrumb-item>Запрос</el-breadcrumb-item>
    </el-breadcrumb>

    <template v-if="entry && card">
      <div class="page-head">
        <h1>
          {{ entry.template_name ?? 'Запрос без имени шаблона' }}
          <el-tag :type="statusLabels[entry.status].type" disable-transitions class="status">
            {{ statusLabels[entry.status].label }} · {{ entry.http_status }}
          </el-tag>
        </h1>
      </div>

      <el-descriptions :column="2" border class="block">
        <el-descriptions-item label="Время">{{ formatDateTime(entry.created_at) }}</el-descriptions-item>
        <el-descriptions-item label="Портал">{{ entry.project_name }} <span class="mono muted">({{ entry.project }})</span></el-descriptions-item>
        <el-descriptions-item label="Внешний ID"><span class="mono">{{ entry.external_id ?? '—' }}</span></el-descriptions-item>
        <el-descriptions-item label="Автор запроса"><span class="mono">{{ entry.requested_by ?? '—' }}</span></el-descriptions-item>
        <el-descriptions-item label="Формат">{{ entry.format ?? '—' }}</el-descriptions-item>
        <el-descriptions-item label="Время выпуска">{{ entry.duration_ms }} мс</el-descriptions-item>
        <el-descriptions-item label="Размер результата">{{ formatSize(entry.result_size) }}</el-descriptions-item>
        <el-descriptions-item label="Ключ идемпотентности"><span class="mono">{{ entry.idempotency_key ?? '—' }}</span></el-descriptions-item>
        <el-descriptions-item label="SHA-256 шаблона" :span="2"><span class="mono">{{ entry.template_sha256 ?? '—' }}</span></el-descriptions-item>
        <el-descriptions-item label="ID запроса (логи)" :span="2"><span class="mono">{{ entry.request_id }}</span></el-descriptions-item>
      </el-descriptions>

      <template v-if="entry.error_code">
        <h2>Ошибка</h2>
        <el-alert
          :type="entry.status === 'failed' ? 'error' : 'warning'"
          :closable="false"
          show-icon
          :title="errorLabels[entry.error_code] ?? entry.error_code"
          :description="`${entry.error_code}: ${entry.error_message ?? ''}`"
          class="block"
        />
        <el-table v-if="entry.error_details?.length" :data="entry.error_details" size="small" class="block">
          <template v-if="templateProblems">
            <el-table-column label="Где" width="180">
              <template #default="{ row }">{{ row.part }}</template>
            </el-table-column>
            <el-table-column label="Рядом с текстом" min-width="240">
              <template #default="{ row }"><span class="muted">{{ row.context || '—' }}</span></template>
            </el-table-column>
          </template>
          <el-table-column v-else label="Поле в данных" width="240">
            <template #default="{ row }"><span class="mono">{{ row.field || '(корень)' }}</span></template>
          </el-table-column>
          <el-table-column prop="message" label="Что не так" min-width="240" />
        </el-table>
      </template>

      <h2>Документ</h2>
      <p class="muted">{{ documentState }}</p>
      <template v-if="card.document">
        <el-descriptions :column="2" border class="block">
          <el-descriptions-item label="ID документа"><span class="mono">{{ card.document.id }}</span></el-descriptions-item>
          <el-descriptions-item label="Хранится до">{{ formatDateTime(card.document.expires_at) }}</el-descriptions-item>
          <el-descriptions-item label="Размер">{{ formatSize(card.document.file_size) }}</el-descriptions-item>
          <el-descriptions-item label="SHA-256 файла"><span class="mono">{{ card.document.file_sha256 }}</span></el-descriptions-item>
        </el-descriptions>
        <template v-if="card.document.data">
          <h2>Входные данные</h2>
          <pre class="json">{{ prettyJSON(card.document.data) }}</pre>
        </template>
      </template>
    </template>
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

.block {
  margin-bottom: 12px;
}
</style>

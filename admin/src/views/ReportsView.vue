<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { computed, onMounted, ref, watch } from 'vue'

import { api, type Project, type Report } from '../api'
import { errorText, formatDay } from '../labels'

const projects = ref<Project[]>([])
const project = ref('')
const period = ref<string[]>([]) // пусто — последние 30 дней
const report = ref<Report | null>(null)
const loading = ref(false)

const shownPeriod = computed(() => {
  if (!report.value) return ''
  // to в ответе не включается: показываем последний день периода.
  const to = new Date(new Date(report.value.to).getTime() - 1).toLocaleDateString('ru-RU', { timeZone: 'Asia/Almaty' })
  const from = new Date(report.value.from).toLocaleDateString('ru-RU', { timeZone: 'Asia/Almaty' })
  return `${from} — ${to}`
})

const errorShare = computed(() => {
  const t = report.value?.totals
  if (!t || t.requests === 0) return '—'
  return `${(((t.rejected + t.failed) / t.requests) * 100).toFixed(1)} %`
})

async function load() {
  loading.value = true
  try {
    report.value = await api.reports({ project: project.value || undefined, from: period.value[0], to: period.value[1] })
  } catch (e) {
    ElMessage.error(errorText(e))
  } finally {
    loading.value = false
  }
}

watch([project, period], load)
onMounted(async () => {
  projects.value = await api.projects().catch(() => [])
  await load()
})
</script>

<template>
  <div v-loading="loading" class="page">
    <div class="page-head">
      <h1>Отчёты</h1>
      <span class="muted">{{ shownPeriod }}</span>
    </div>

    <div class="filters">
      <el-select v-model="project" placeholder="Все порталы" clearable class="f-project">
        <el-option v-for="p in projects" :key="p.code" :label="p.name" :value="p.code" />
      </el-select>
      <el-date-picker
        v-model="period"
        type="daterange"
        value-format="YYYY-MM-DD"
        format="DD.MM.YYYY"
        start-placeholder="С"
        end-placeholder="По"
        style="width: 260px"
      />
      <span v-if="!period?.length" class="muted small">по умолчанию — последние 30 дней</span>
    </div>

    <template v-if="report">
      <el-descriptions :column="3" border class="block">
        <el-descriptions-item label="Выпущено документов">{{ report.totals.created }}</el-descriptions-item>
        <el-descriptions-item label="Повторы по ключу">{{ report.totals.repeated }}</el-descriptions-item>
        <el-descriptions-item label="Среднее время выпуска">{{ report.totals.avg_duration_ms }} мс</el-descriptions-item>
        <el-descriptions-item label="Отклонено (ошибки порталов)">{{ report.totals.rejected }}</el-descriptions-item>
        <el-descriptions-item label="Сбои сервиса">{{ report.totals.failed }}</el-descriptions-item>
        <el-descriptions-item label="Доля ошибок">{{ errorShare }}</el-descriptions-item>
      </el-descriptions>

      <div class="grid">
        <section>
          <h2>По порталам</h2>
          <el-table :data="report.by_project" size="small" empty-text="Запросов не было">
            <el-table-column label="Портал" min-width="160">
              <template #default="{ row }">{{ row.project_name }}</template>
            </el-table-column>
            <el-table-column prop="created" label="Выпущено" width="100" align="right" />
            <el-table-column prop="errors" label="Ошибок" width="100" align="right" />
          </el-table>
        </section>

        <section>
          <h2>По дням</h2>
          <el-table :data="report.by_day" size="small" empty-text="Запросов не было" max-height="360">
            <el-table-column label="День" min-width="120">
              <template #default="{ row }">{{ formatDay(row.day) }}</template>
            </el-table-column>
            <el-table-column prop="created" label="Выпущено" width="100" align="right" />
            <el-table-column prop="errors" label="Ошибок" width="100" align="right" />
          </el-table>
        </section>
      </div>

      <h2>По шаблонам</h2>
      <el-table :data="report.by_template" size="small" empty-text="Запросов не было">
        <el-table-column label="Шаблон" min-width="200">
          <template #default="{ row }">
            <span class="mono">{{ row.template_name || '(без имени)' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="Портал" min-width="140">
          <template #default="{ row }">{{ projects.find((p) => p.code === row.project)?.name ?? row.project }}</template>
        </el-table-column>
        <el-table-column prop="created" label="Выпущено" width="100" align="right" />
        <el-table-column prop="errors" label="Ошибок" width="100" align="right" />
        <el-table-column label="Среднее, мс" width="110" align="right">
          <template #default="{ row }">{{ row.created ? row.avg_duration_ms : '—' }}</template>
        </el-table-column>
      </el-table>
    </template>
  </div>
</template>

<style scoped>
.filters {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  align-items: center;
  margin-bottom: 16px;
}

.f-project {
  width: 220px;
}

.small {
  font-size: 12px;
}

.block {
  margin-bottom: 8px;
  background: #fff;
}

h2 {
  margin: 24px 0 12px;
  font-size: 18px;
  font-weight: 600;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 24px;
}
</style>

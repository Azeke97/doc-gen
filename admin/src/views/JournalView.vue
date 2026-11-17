<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { type LocationQuery, useRoute, useRouter } from 'vue-router'

import { api, type JournalEntry, type JournalFilter, type JournalStatus, type Project } from '../api'
import { errorLabels, errorText, formatDateTime, statusLabels } from '../labels'

const route = useRoute()
const router = useRouter()

const projects = ref<Project[]>([])
const items = ref<JournalEntry[]>([])
const cursor = ref<string | null>(null)
const loading = ref(false)

// Фильтры живут в адресе: ссылку на выборку можно отправить коллеге.
const str = (v: unknown) => (typeof v === 'string' ? v : '')
const fromQuery = (q: LocationQuery) => ({
  project: str(q.project),
  template_name: str(q.template_name),
  external_id: str(q.external_id),
  status: ([] as JournalStatus[]).concat((q.status ?? []) as JournalStatus[]),
  period: (q.from && q.to ? [str(q.from), str(q.to)] : []) as string[],
})
const filter = reactive(fromQuery(route.query))

const request = computed<JournalFilter>(() => ({
  project: filter.project || undefined,
  template_name: filter.template_name.trim() || undefined,
  external_id: filter.external_id.trim() || undefined,
  status: filter.status.length ? filter.status : undefined,
  from: filter.period[0],
  to: filter.period[1],
}))

const hasFilter = computed(() => Object.values(request.value).some((v) => v !== undefined))

async function load(more = false) {
  loading.value = true
  try {
    const page = await api.journal({ ...request.value, cursor: more ? cursor.value ?? undefined : undefined })
    items.value = more ? [...items.value, ...page.items] : page.items
    cursor.value = page.next_cursor
  } catch (e) {
    ElMessage.error(errorText(e))
  } finally {
    loading.value = false
  }
}

let timer: ReturnType<typeof setTimeout> | undefined
let written = JSON.stringify(route.query)
watch(
  request,
  (r) => {
    // Текстовые поля — с небольшой задержкой, чтобы не запрашивать на каждую букву.
    clearTimeout(timer)
    timer = setTimeout(() => {
      const query = Object.fromEntries(Object.entries(r).filter(([, v]) => v !== undefined))
      written = JSON.stringify(query)
      void router.replace({ query })
      void load()
    }, 300)
  },
  { deep: true },
)

// Адрес сменился без перемонтирования страницы («Назад», правка адреса): фильтры берутся из него.
watch(
  () => route.query,
  (q) => {
    if (route.path !== '/journal' || JSON.stringify(q) === written) return
    written = JSON.stringify(q)
    Object.assign(filter, fromQuery(q))
  },
)

function reset() {
  Object.assign(filter, { project: '', template_name: '', external_id: '', status: [], period: [] })
}

function open(row: JournalEntry) {
  void router.push(`/journal/${row.id}`)
}

onMounted(async () => {
  projects.value = await api.projects().catch(() => [])
  await load()
})
</script>

<template>
  <div class="page">
    <div class="page-head">
      <h1>Журнал запросов</h1>
      <el-button :loading="loading" @click="load()">Обновить</el-button>
    </div>

    <div class="filters">
      <el-select v-model="filter.project" placeholder="Все порталы" clearable class="f-project">
        <el-option v-for="p in projects" :key="p.code" :label="p.name" :value="p.code" />
      </el-select>
      <el-input v-model="filter.template_name" placeholder="Шаблон, например opening_protocol" clearable class="f-text" />
      <el-input v-model="filter.external_id" placeholder="Внешний ID, например lot:209092" clearable class="f-text" />
      <el-select v-model="filter.status" multiple collapse-tags placeholder="Все статусы" clearable class="f-status">
        <el-option v-for="(s, key) in statusLabels" :key="key" :label="s.label" :value="key" />
      </el-select>
      <el-date-picker
        v-model="filter.period"
        type="daterange"
        value-format="YYYY-MM-DD"
        format="DD.MM.YYYY"
        start-placeholder="С"
        end-placeholder="По"
        style="width: 260px"
      />
      <el-button v-if="hasFilter" link @click="reset">Сбросить</el-button>
    </div>

    <el-table
      v-loading="loading && items.length === 0"
      :data="items"
      class="clickable-rows"
      empty-text="Запросов нет"
      @row-click="open"
    >
      <el-table-column label="Время" width="170">
        <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="Портал" min-width="140">
        <template #default="{ row }">{{ row.project_name }}</template>
      </el-table-column>
      <el-table-column label="Шаблон" min-width="170">
        <template #default="{ row }">
          <span class="mono">{{ row.template_name ?? '—' }}</span>
          <span v-if="row.format" class="muted small"> · {{ row.format }}</span>
        </template>
      </el-table-column>
      <el-table-column label="Внешний ID" min-width="140">
        <template #default="{ row }"><span class="mono">{{ row.external_id ?? '—' }}</span></template>
      </el-table-column>
      <el-table-column label="Статус" width="130">
        <template #default="{ row }">
          <el-tag :type="statusLabels[row.status as JournalStatus].type" disable-transitions>
            {{ statusLabels[row.status as JournalStatus].label }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="Ошибка" min-width="220">
        <template #default="{ row }">
          <span v-if="row.error_code">{{ errorLabels[row.error_code] ?? row.error_code }}</span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="Время, мс" width="100" align="right">
        <template #default="{ row }">{{ row.duration_ms }}</template>
      </el-table-column>
    </el-table>

    <div v-if="cursor" class="more">
      <el-button :loading="loading" @click="load(true)">Показать ещё</el-button>
    </div>
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

.f-project,
.f-status {
  width: 200px;
}

.f-text {
  width: 240px;
}

.small {
  font-size: 12px;
}

.more {
  display: flex;
  justify-content: center;
  margin-top: 16px;
}
</style>

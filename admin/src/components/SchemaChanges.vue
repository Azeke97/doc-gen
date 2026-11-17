<script setup lang="ts">
import type { SchemaChange } from '../api'

defineProps<{ changes: SchemaChange[] }>()

// Виды изменений из dataschema.Diff; подробность (значения, ограничения) — в message сервера.
const kinds: Record<string, string> = {
  field_added: 'Новое поле',
  field_removed: 'Поле удалено',
  became_required: 'Поле стало обязательным',
  type_changed: 'Изменился тип',
  enum_narrowed: 'Сужен список значений',
  additional_properties_closed: 'Запрещены лишние поля',
  constraint_changed: 'Изменилось ограничение',
}
</script>

<template>
  <el-table :data="changes" size="small" empty-text="Схема не изменилась">
    <el-table-column label="Поле" min-width="160">
      <template #default="{ row }"><span class="mono">{{ row.path || '(корень)' }}</span></template>
    </el-table-column>
    <el-table-column label="Изменение" min-width="260">
      <template #default="{ row }">
        <div>{{ kinds[row.kind] ?? row.kind }}</div>
        <div class="muted detail">{{ row.message }}</div>
      </template>
    </el-table-column>
    <el-table-column label="" width="120">
      <template #default="{ row }">
        <el-tag v-if="row.breaking" type="danger" size="small" disable-transitions>ломающее</el-tag>
        <el-tag v-else type="info" size="small" disable-transitions>совместимое</el-tag>
      </template>
    </el-table-column>
  </el-table>
</template>

<style scoped>
.detail {
  font-size: 12px;
}
</style>

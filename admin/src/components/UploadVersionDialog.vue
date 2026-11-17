<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { ref, watch } from 'vue'

import { api, ApiError, type FieldError } from '../api'
import { errorText } from '../labels'

const props = defineProps<{ templateId: string; schema: string; testData: string; hasDraft: boolean; expectedDraft: string }>()
const visible = defineModel<boolean>({ required: true })
const emit = defineEmits<{ uploaded: [] }>()

const file = ref<File | null>(null)
const fileInput = ref<HTMLInputElement>()
const schema = ref('')
const testData = ref('')
const saving = ref(false)
const error = ref('')
const fieldErrors = ref<FieldError[]>([])
const expected = ref('none') // каким был черновик, когда открыли диалог

// Схема и тестовые данные подставляются из последней версии: обычно их правят, а не пишут заново.
watch(visible, (open) => {
  if (!open) return
  file.value = null
  if (fileInput.value) fileInput.value.value = ''
  expected.value = props.expectedDraft
  schema.value = props.schema
  testData.value = props.testData
  error.value = ''
  fieldErrors.value = []
})

function pickFile(e: Event) {
  file.value = (e.target as HTMLInputElement).files?.[0] ?? null
}

async function loadInto(target: 'schema' | 'testData', e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  if (f) (target === 'schema' ? schema : testData).value = await f.text()
  input.value = ''
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

async function submit() {
  error.value = ''
  fieldErrors.value = []
  if (!file.value) {
    error.value = 'Выберите файл шаблона'
    return
  }
  if (!checkJSON(schema.value, 'Схема данных', false) || !checkJSON(testData.value, 'Тестовые данные', true)) return

  saving.value = true
  try {
    await api.uploadVersion(props.templateId, file.value, schema.value, testData.value, expected.value)
    ElMessage.success(props.hasDraft ? 'Черновик заменён' : 'Версия загружена')
    visible.value = false
    emit('uploaded')
  } catch (e) {
    error.value = e instanceof ApiError && e.code === 'draft_changed'
      ? 'Черновик за это время изменил другой пользователь. Закройте окно и обновите страницу, чтобы увидеть его правки.'
      : errorText(e)
    if (e instanceof ApiError) fieldErrors.value = e.fieldErrors
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="hasDraft ? 'Заменить черновик' : 'Новая версия'" width="min(760px, 94vw)" top="5vh">
    <el-alert
      v-if="hasDraft"
      type="warning"
      :closable="false"
      show-icon
      title="У шаблона уже есть черновик — он будет заменён этой загрузкой."
      class="block"
    />
    <el-form label-position="top" @submit.prevent="submit">
      <el-form-item label="Файл шаблона (.html)" required>
        <input ref="fileInput" type="file" accept=".html,.htm,text/html" @change="pickFile">
      </el-form-item>

      <el-form-item required>
        <template #label>
          <span>Схема данных (JSON Schema)</span>
          <label class="from-file">загрузить из файла<input type="file" accept=".json,application/json" hidden @change="loadInto('schema', $event)"></label>
        </template>
        <el-input v-model="schema" type="textarea" :rows="10" class="code" spellcheck="false" />
      </el-form-item>

      <el-form-item>
        <template #label>
          <span>Тестовые данные (JSON) — для проверки и предпросмотра</span>
          <label class="from-file">загрузить из файла<input type="file" accept=".json,application/json" hidden @change="loadInto('testData', $event)"></label>
        </template>
        <el-input v-model="testData" type="textarea" :rows="8" class="code" spellcheck="false" />
      </el-form-item>
    </el-form>

    <el-alert v-if="error" type="error" :closable="false" show-icon :title="error" class="block">
      <ul v-if="fieldErrors.length" class="field-errors">
        <li v-for="(fe, i) in fieldErrors" :key="i">
          <span class="mono">{{ fe.field || '(корень)' }}</span> — {{ fe.message }}
        </li>
      </ul>
    </el-alert>

    <template #footer>
      <el-button @click="visible = false">Отмена</el-button>
      <el-button type="primary" :loading="saving" @click="submit">Загрузить</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.block {
  margin-bottom: 16px;
}

.from-file {
  margin-left: 12px;
  color: var(--el-color-primary);
  cursor: pointer;
  font-weight: normal;
}

.code :deep(textarea) {
  font-family: ui-monospace, "SFMono-Regular", Menlo, monospace;
  font-size: 13px;
}

.field-errors {
  margin: 4px 0 0;
  padding-left: 18px;
}
</style>

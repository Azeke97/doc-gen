<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'

const visible = ref(false)
const title = ref('')
const url = ref('')
const filename = ref('')

/** Показывает PDF из Blob; прошлый object URL освобождается. */
function show(blob: Blob, dialogTitle: string, name: string) {
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = URL.createObjectURL(blob)
  title.value = dialogTitle
  filename.value = name
  visible.value = true
}

onBeforeUnmount(() => {
  if (url.value) URL.revokeObjectURL(url.value)
})

defineExpose({ show })
</script>

<template>
  <el-dialog v-model="visible" :title="title" width="min(1100px, 96vw)" top="3vh">
    <iframe v-if="url" :src="url" class="pdf" title="PDF" />
    <template #footer>
      <el-button v-if="url" tag="a" :href="url" :download="filename">Скачать PDF</el-button>
      <el-button type="primary" @click="visible = false">Закрыть</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.pdf {
  width: 100%;
  height: 75vh;
  border: 1px solid var(--el-border-color-light);
  border-radius: 4px;
}
</style>

<script setup lang="ts">
import { html } from '@codemirror/lang-html'
import { json } from '@codemirror/lang-json'
import { EditorState } from '@codemirror/state'
import { Decoration, EditorView, keymap, MatchDecorator, ViewPlugin, type ViewUpdate } from '@codemirror/view'
import { basicSetup } from 'codemirror'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps<{ language: 'html' | 'json'; readonly?: boolean }>()
const model = defineModel<string>({ required: true })
const emit = defineEmits<{ save: [] }>()

const host = ref<HTMLDivElement>()
let view: EditorView | null = null

// Вставки шаблонизатора {{ ... }} подсвечиваются поверх HTML: так видно, где данные, а где разметка.
const actionMatcher = new MatchDecorator({
  regexp: /\{\{.*?\}\}/g,
  decoration: Decoration.mark({ class: 'cm-tpl-action' }),
})
const templateActions = ViewPlugin.define((v) => ({
  decorations: actionMatcher.createDeco(v),
  update(u: ViewUpdate) {
    this.decorations = actionMatcher.updateDeco(u, this.decorations)
  },
}), { decorations: (p) => p.decorations })

onMounted(() => {
  view = new EditorView({
    parent: host.value!,
    state: EditorState.create({
      doc: model.value,
      extensions: [
        keymap.of([{ key: 'Mod-s', preventDefault: true, run: () => (emit('save'), true) }]),
        basicSetup,
        props.language === 'html' ? [html(), templateActions] : json(),
        EditorView.lineWrapping,
        EditorState.readOnly.of(!!props.readonly),
        EditorView.updateListener.of((u) => {
          if (u.docChanged) model.value = u.state.doc.toString()
        }),
      ],
    }),
  })
})

// Текст поменяли снаружи (загрузили другую версию) — заменяем документ целиком.
watch(model, (text) => {
  if (view && text !== view.state.doc.toString()) {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } })
  }
})

onBeforeUnmount(() => view?.destroy())

/** Вставляет текст на место курсора (или вместо выделения). */
function insert(text: string) {
  if (!view) return
  view.dispatch(view.state.replaceSelection(text))
  view.focus()
}

defineExpose({ insert })
</script>

<template>
  <div ref="host" class="code-editor" />
</template>

<style scoped>
.code-editor {
  height: 100%;
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
  overflow: hidden;
  background: #fff;
}

.code-editor :deep(.cm-editor) {
  height: 100%;
  font-size: 13px;
}

.code-editor :deep(.cm-scroller) {
  font-family: ui-monospace, "SFMono-Regular", Menlo, monospace;
}

.code-editor :deep(.cm-tpl-action) {
  background: #fdf6ec;
  color: #b25d00;
  border-radius: 2px;
}
</style>

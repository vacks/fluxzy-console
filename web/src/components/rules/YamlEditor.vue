<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { basicSetup, EditorView } from 'codemirror'
import { Compartment, EditorState } from '@codemirror/state'
import { keymap } from '@codemirror/view'
import { indentWithTab } from '@codemirror/commands'
import { yaml } from '@codemirror/lang-yaml'
import { oneDark } from '@codemirror/theme-one-dark'
import { useThemeStore } from '@/stores/theme'

const props = withDefaults(defineProps<{ modelValue: string; fill?: boolean }>(), { fill: false })
const emit = defineEmits<{ 'update:modelValue': [string] }>()
const theme = useThemeStore()
const el = ref<HTMLDivElement>()
let view: EditorView | null = null
const themeCompartment = new Compartment()

function buildState(doc: string) {
  return EditorState.create({
    doc,
    extensions: [
      basicSetup,
      yaml(),
      keymap.of([indentWithTab]),
      EditorView.lineWrapping,
      themeCompartment.of(theme.isDark ? oneDark : []),
      EditorView.theme({
        '&': { fontSize: '12px', backgroundColor: 'transparent' },
        '.cm-content': { fontFamily: '"JetBrains Mono Variable", ui-monospace, monospace', padding: '10px 0' },
        '.cm-gutters': { backgroundColor: 'transparent', borderRight: '1px solid rgba(148,163,184,.12)' },
        '.cm-activeLine': { backgroundColor: 'rgba(129,140,248,.06)' },
        '.cm-activeLineGutter': { backgroundColor: 'rgba(129,140,248,.08)' },
        '&.cm-focused': { outline: 'none' },
        '.cm-scroller': { overflow: 'auto' },
      }),
      EditorView.updateListener.of((update) => {
        if (update.docChanged) emit('update:modelValue', update.state.doc.toString())
      }),
    ],
  })
}

onMounted(() => {
  if (!el.value) return
  view = new EditorView({ state: buildState(props.modelValue), parent: el.value })
})

watch(
  () => props.modelValue,
  (v) => {
    if (!view) return
    if (view.state.doc.toString() !== v) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v } })
    }
  },
)

watch(
  () => theme.isDark,
  (dark) => {
    view?.dispatch({ effects: themeCompartment.reconfigure(dark ? oneDark : []) })
  },
)

onBeforeUnmount(() => {
  view?.destroy()
  view = null
})
</script>

<template>
  <div
    ref="el"
    class="yaml-editor overflow-auto rounded-xl border border-line/60 bg-[rgb(var(--bg-soft))]"
    :class="fill ? 'h-full min-h-0' : 'max-h-[560px] min-h-[460px]'"
  />
</template>

<style scoped>
.yaml-editor :deep(.cm-editor) {
  min-height: 460px;
}
.yaml-editor.h-full :deep(.cm-editor) {
  height: 100%;
  min-height: 0;
}
.yaml-editor.h-full :deep(.cm-scroller) {
  overflow: auto;
}
</style>

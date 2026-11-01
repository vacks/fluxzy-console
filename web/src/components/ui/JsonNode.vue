<script setup lang="ts">
import { computed, ref } from 'vue'
import { ChevronDown, ChevronRight } from 'lucide-vue-next'

const props = withDefaults(
  defineProps<{ name?: string | number; value: unknown; depth?: number; expandDepth?: number }>(),
  { depth: 0, expandDepth: 2 },
)

const isObject = computed(() => props.value !== null && typeof props.value === 'object')
const isArray = computed(() => Array.isArray(props.value))
const entries = computed<[string | number, unknown][]>(() => {
  if (!isObject.value) return []
  if (isArray.value) return (props.value as unknown[]).map((v, i) => [i, v])
  return Object.entries(props.value as Record<string, unknown>)
})
const open = ref(props.depth < props.expandDepth)
const showAll = ref(false)
const limited = computed(() => !showAll.value && entries.value.length > 300)
const visibleEntries = computed(() => (limited.value ? entries.value.slice(0, 300) : entries.value))
const summary = computed(() => (isArray.value ? `[${entries.value.length}]` : `{${entries.value.length}}`))
const scalarClass = computed(() => {
  if (props.value === null) return 'text-faint italic'
  switch (typeof props.value) {
    case 'string':
      return 'text-emerald-500 dark:text-emerald-300'
    case 'number':
      return 'text-sky-500 dark:text-sky-300'
    case 'boolean':
      return 'text-amber-500 dark:text-amber-300'
    default:
      return 'text-ink'
  }
})
function scalarText(v: unknown): string {
  if (v === null) return 'null'
  if (typeof v === 'string') return JSON.stringify(v)
  return String(v)
}
</script>

<template>
  <div class="font-mono text-[12px] leading-6">
    <div class="flex items-start gap-1">
      <button
        v-if="isObject"
        type="button"
        class="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded text-faint hover:bg-panel-strong hover:text-ink"
        @click="open = !open"
      >
        <component :is="open ? ChevronDown : ChevronRight" class="h-3.5 w-3.5" />
      </button>
      <span v-else class="w-5 shrink-0" />
      <span v-if="name !== undefined" class="shrink-0 text-ink">
        <span class="text-violet-500 dark:text-violet-300">{{ String(name) }}</span><span class="text-faint">:</span>
      </span>
      <span v-if="isObject && !open" class="text-faint">{{ summary }}</span>
      <span v-else-if="!isObject" :class="scalarClass" class="break-all">{{ scalarText(value) }}</span>
      <span v-else-if="open" class="text-faint">{{ isArray ? '[' : '{' }}</span>
    </div>
    <div v-if="isObject && open" class="ml-[1.35rem] border-l border-line/40 pl-2">
      <JsonNode
        v-for="[k, v] in visibleEntries"
        :key="String(k)"
        :name="k"
        :value="v"
        :depth="depth + 1"
        :expand-depth="expandDepth"
      />
      <button
        v-if="limited"
        type="button"
        class="my-1 rounded-md border border-line/60 px-2 py-0.5 text-[11px] text-muted hover:bg-panel-strong"
        @click="showAll = true"
      >
        显示全部 {{ entries.length }} 项
      </button>
      <div class="text-faint">{{ isArray ? ']' : '}' }}</div>
    </div>
  </div>
</template>

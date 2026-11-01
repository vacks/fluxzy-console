<script setup lang="ts">
import CopyButton from './CopyButton.vue'
withDefaults(defineProps<{ code: string; language?: string; maxHeight?: string; fill?: boolean }>(), {
  language: '',
  maxHeight: '420px',
  fill: false,
})
</script>

<template>
  <div
    class="overflow-hidden rounded-xl border border-line/60 bg-[rgb(var(--bg-soft))]"
    :class="fill ? 'flex min-h-0 flex-1 flex-col' : ''"
  >
    <div class="flex shrink-0 items-center justify-between border-b border-line/50 px-3 py-1.5">
      <span class="font-mono text-[10.5px] uppercase tracking-wider text-faint">{{ language || 'code' }}</span>
      <CopyButton :text="code" small label="复制" />
    </div>
    <pre
      class="overflow-auto p-3 font-mono text-[12px] leading-relaxed text-ink"
      :class="fill ? 'min-h-0 flex-1' : ''"
      :style="fill ? undefined : { maxHeight }"
    ><code>{{ code }}</code></pre>
  </div>
</template>

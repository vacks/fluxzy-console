<script setup lang="ts">
import { Inbox } from 'lucide-vue-next'
withDefaults(defineProps<{ title?: string; description?: string }>(), {
  title: '暂无数据',
  description: '',
})
</script>

<template>
  <div class="flex flex-col items-center justify-center gap-2 px-6 py-14 text-center">
    <div class="flex h-12 w-12 items-center justify-center rounded-2xl border border-line/60 bg-panel-strong/70 text-faint">
      <slot name="icon"><Inbox class="h-5 w-5" /></slot>
    </div>
    <div class="mt-1 text-[13px] font-semibold text-ink">{{ title }}</div>
    <div v-if="description" class="max-w-md text-[12px] leading-relaxed text-muted">{{ description }}</div>
    <div v-if="$slots.action" class="mt-2">
      <slot name="action" />
    </div>
  </div>
</template>

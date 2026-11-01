<script setup lang="ts">
import type { Component } from 'vue'
import MiniSparkline from '@/components/charts/MiniSparkline.vue'

withDefaults(
  defineProps<{
    label: string
    value: string
    hint?: string
    icon: Component
    tone?: 'accent' | 'ok' | 'warn' | 'bad' | 'cyan'
    spark?: number[]
    loading?: boolean
  }>(),
  { tone: 'accent', loading: false },
)

const toneMap = {
  accent: { text: 'text-accent', bg: 'bg-accent/10', color: '#818cf8' },
  cyan: { text: 'text-accent-2', bg: 'bg-accent-2/10', color: '#22d3ee' },
  ok: { text: 'text-ok', bg: 'bg-ok/10', color: '#34d399' },
  warn: { text: 'text-warn', bg: 'bg-warn/10', color: '#fbbf24' },
  bad: { text: 'text-bad', bg: 'bg-bad/10', color: '#fb7185' },
}
</script>

<template>
  <div class="panel relative overflow-hidden p-4">
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <div class="text-[11px] font-medium uppercase tracking-[0.12em] text-faint">{{ label }}</div>
        <div v-if="loading" class="mt-2.5 h-8 w-24 skeleton" />
        <div v-else class="mt-1.5 font-mono text-[25px] font-semibold leading-none tracking-tight text-ink">{{ value }}</div>
        <div v-if="hint" class="mt-2 truncate text-[11px] text-muted">{{ hint }}</div>
      </div>
      <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl" :class="toneMap[tone].bg">
        <component :is="icon" class="h-4 w-4" :class="toneMap[tone].text" />
      </div>
    </div>
    <div v-if="spark && spark.length" class="pointer-events-none absolute -bottom-1 right-2 opacity-80">
      <MiniSparkline :data="spark" :color="toneMap[tone].color" :width="96" :height="28" />
    </div>
  </div>
</template>

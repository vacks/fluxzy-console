<script setup lang="ts">
import { computed } from 'vue'
import { formatNumber } from '@/utils/format'

const props = defineProps<{ s2: number; s3: number; s4: number; s5: number; pending?: number }>()

const total = computed(() => props.s2 + props.s3 + props.s4 + props.s5)
const parts = computed(() => {
  const t = total.value || 1
  return [
    { key: '2xx', label: '2xx 成功', value: props.s2, color: 'rgb(52 211 153)', pct: (props.s2 / t) * 100 },
    { key: '3xx', label: '3xx 重定向', value: props.s3, color: 'rgb(34 211 238)', pct: (props.s3 / t) * 100 },
    { key: '4xx', label: '4xx 客户端', value: props.s4, color: 'rgb(251 191 36)', pct: (props.s4 / t) * 100 },
    { key: '5xx', label: '5xx 服务端', value: props.s5, color: 'rgb(251 113 133)', pct: (props.s5 / t) * 100 },
  ]
})
const gradient = computed(() => {
  if (!total.value) return 'conic-gradient(rgb(var(--line)) 0 100%)'
  let acc = 0
  const stops: string[] = []
  for (const p of parts.value) {
    const start = acc
    acc += p.pct
    stops.push(`${p.color} ${start}% ${acc}%`)
  }
  return `conic-gradient(${stops.join(', ')})`
})
</script>

<template>
  <div class="flex items-center gap-5">
    <div class="relative h-[132px] w-[132px] shrink-0">
      <div class="h-full w-full rounded-full" :style="{ background: gradient }" />
      <div class="absolute inset-[15px] flex flex-col items-center justify-center rounded-full border border-line/50 bg-panel">
        <div class="font-mono text-[20px] font-semibold tracking-tight text-ink">{{ formatNumber(total) }}</div>
        <div class="mt-0.5 text-[10px] uppercase tracking-wider text-faint">responses</div>
      </div>
    </div>
    <div class="min-w-0 flex-1 space-y-2">
      <div v-for="p in parts" :key="p.key" class="flex items-center gap-2 text-[11.5px]">
        <span class="h-2 w-2 rounded-sm" :style="{ background: p.color }" />
        <span class="text-muted">{{ p.label }}</span>
        <span class="ml-auto font-mono text-ink">{{ formatNumber(p.value) }}</span>
        <span class="w-10 text-right font-mono text-[10.5px] text-faint">{{ p.pct.toFixed(1) }}%</span>
      </div>
      <div v-if="pending" class="mt-2 flex items-center gap-2 border-t border-line/50 pt-2 text-[11px] text-faint">
        <span class="h-1.5 w-1.5 animate-pulse rounded-full bg-accent" />
        {{ formatNumber(pending) }} 个请求进行中
      </div>
    </div>
  </div>
</template>

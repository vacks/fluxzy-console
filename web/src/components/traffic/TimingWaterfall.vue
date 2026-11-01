<script setup lang="ts">
import { computed } from 'vue'
import type { ExchangeDetail } from '@/api/types'
import EmptyState from '@/components/ui/EmptyState.vue'
import { formatDuration, formatISO } from '@/utils/format'

const props = defineProps<{ metrics: ExchangeDetail['metrics'] }>()

const defs: { key: string; label: string; color: string }[] = [
  { key: 'receivedFromProxy', label: '请求到达 fluxzy', color: 'rgb(var(--accent))' },
  { key: 'retrievingPool', label: '获取连接池', color: 'rgb(var(--faint))' },
  { key: 'requestHeaderSending', label: '发送请求头', color: 'rgb(var(--accent-2))' },
  { key: 'requestHeaderSent', label: '请求头发送完成', color: 'rgb(var(--accent-2))' },
  { key: 'requestBodySent', label: '请求体发送完成', color: 'rgb(var(--accent-2))' },
  { key: 'responseHeaderStart', label: '收到响应头首字节', color: 'rgb(var(--warn))' },
  { key: 'responseHeaderEnd', label: '响应头接收完成', color: 'rgb(var(--warn))' },
  { key: 'responseBodyStart', label: '响应体首字节', color: 'rgb(var(--ok))' },
  { key: 'responseBodyEnd', label: '响应体接收完成', color: 'rgb(var(--ok))' },
]

const phases = computed(() => {
  const rows = defs
    .map((d) => {
      const iso = props.metrics.times[d.key]
      if (!iso) return null
      const t = new Date(iso).getTime()
      if (Number.isNaN(t)) return null
      return { ...d, t, iso }
    })
    .filter(Boolean) as { key: string; label: string; color: string; t: number; iso: string }[]
  rows.sort((a, b) => a.t - b.t)
  return rows
})

const total = computed(() => {
  if (phases.value.length < 2) return props.metrics.durationMs || 0
  return phases.value[phases.value.length - 1].t - phases.value[0].t
})

const segments = computed(() => {
  const rows = phases.value
  if (rows.length < 2) return []
  const start = rows[0].t
  const end = rows[rows.length - 1].t
  const span = Math.max(1, end - start)
  const out: { label: string; color: string; width: number; ms: number }[] = []
  for (let i = 1; i < rows.length; i++) {
    const ms = rows[i].t - rows[i - 1].t
    if (ms <= 0) continue
    out.push({ label: rows[i - 1].label, color: rows[i - 1].color, width: Math.max(0.4, (ms / span) * 100), ms })
  }
  return out
})
</script>

<template>
  <div v-if="phases.length" class="space-y-5">
    <div class="rounded-xl border border-line/50 bg-[rgb(var(--bg-soft))] p-4">
      <div class="mb-3 flex items-center justify-between">
        <span class="text-[11.5px] font-medium text-muted">时间线</span>
        <span class="font-mono text-[12px] text-ink">{{ formatDuration(total) }}</span>
      </div>
      <div class="flex h-7 w-full overflow-hidden rounded-lg bg-panel-strong">
        <div
          v-for="(s, i) in segments"
          :key="i"
          class="group relative h-full border-r border-bg/20 last:border-0"
          :style="{ width: `${s.width}%`, background: s.color, opacity: .85 }"
          :title="`${s.label}: ${formatDuration(s.ms)}`"
        >
          <div class="absolute inset-0 opacity-0 transition group-hover:opacity-100 group-hover:bg-white/15" />
        </div>
      </div>
      <div class="mt-3 flex flex-wrap gap-x-4 gap-y-1.5 text-[10.5px] text-faint">
        <span v-for="s in segments" :key="s.label" class="flex items-center gap-1.5">
          <span class="h-2 w-2 rounded-sm" :style="{ background: s.color }" />
          {{ s.label }} · {{ formatDuration(s.ms) }}
        </span>
      </div>
    </div>

    <div class="overflow-hidden rounded-xl border border-line/50">
      <table class="w-full text-left">
        <thead>
          <tr class="border-b border-line/50 bg-panel-strong/50 text-[10px] uppercase tracking-wider text-faint">
            <th class="px-3 py-2 font-semibold">阶段</th>
            <th class="px-3 py-2 text-right font-semibold">相对</th>
            <th class="px-3 py-2 text-right font-semibold">时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(p, i) in phases" :key="p.key" class="border-b border-line/30 last:border-0">
            <td class="px-3 py-2">
              <span class="flex items-center gap-2 text-[11.5px] text-muted">
                <span class="h-2 w-2 rounded-sm" :style="{ background: p.color }" />
                {{ p.label }}
              </span>
            </td>
            <td class="px-3 py-2 text-right font-mono text-[11px] text-ink">
              {{ i === 0 ? '0 ms' : formatDuration(p.t - phases[0].t) }}
            </td>
            <td class="px-3 py-2 text-right font-mono text-[10.5px] text-faint">{{ formatISO(p.iso) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
  <EmptyState v-else title="没有时间数据" description="该交换没有记录到可用的时间阶段。" />
</template>

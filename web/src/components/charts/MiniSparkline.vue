<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{ data: number[]; color?: string; width?: number; height?: number }>(),
  { color: '#818cf8', width: 104, height: 30 },
)

const points = computed(() => {
  const d = props.data.length ? props.data : [0, 0, 0, 0]
  const max = Math.max(...d, 1)
  const min = Math.min(...d, 0)
  const span = max - min || 1
  const step = d.length > 1 ? props.width / (d.length - 1) : props.width
  return d
    .map((v, i) => {
      const x = i * step
      const y = props.height - 3 - ((v - min) / span) * (props.height - 6)
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
})
const area = computed(() => `0,${props.height} ${points.value} ${props.width},${props.height}`)
const id = `spark-${Math.random().toString(36).slice(2, 8)}`
</script>

<template>
  <svg :width="width" :height="height" :viewBox="`0 0 ${width} ${height}`" class="overflow-visible">
    <defs>
      <linearGradient :id="id" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" :stop-color="color" stop-opacity=".32" />
        <stop offset="1" :stop-color="color" stop-opacity="0" />
      </linearGradient>
    </defs>
    <polygon :points="area" :fill="`url(#${id})`" />
    <polyline :points="points" fill="none" :stroke="color" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
  </svg>
</template>

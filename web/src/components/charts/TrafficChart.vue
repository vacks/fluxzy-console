<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { useThemeStore } from '@/stores/theme'

echarts.use([LineChart, GridComponent, TooltipComponent, CanvasRenderer])

const props = withDefaults(
  defineProps<{
    labels: string[]
    series: { name: string; color: string; data: number[] }[]
    height?: number
  }>(),
  { height: 240 },
)

const theme = useThemeStore()
const el = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null
let resizeObserver: ResizeObserver | null = null

function render() {
  if (!chart) return
  const dark = theme.isDark
  const text = dark ? '#94a3b8' : '#64748b'
  const grid = dark ? 'rgba(148,163,184,.14)' : 'rgba(100,116,139,.14)'
  chart.setOption(
    {
      animationDuration: 350,
      grid: { left: 8, right: 8, top: 18, bottom: 4, containLabel: true },
      tooltip: {
        trigger: 'axis',
        backgroundColor: dark ? 'rgba(15,23,42,.96)' : 'rgba(255,255,255,.98)',
        borderColor: dark ? 'rgba(148,163,184,.18)' : 'rgba(148,163,184,.3)',
        textStyle: { color: dark ? '#e2e8f0' : '#0f172a', fontSize: 11 },
        axisPointer: { lineStyle: { color: grid } },
      },
      xAxis: {
        type: 'category',
        boundaryGap: false,
        data: props.labels,
        axisLine: { lineStyle: { color: grid } },
        axisTick: { show: false },
        axisLabel: { color: text, fontSize: 10, hideOverlap: true },
      },
      yAxis: {
        type: 'value',
        splitLine: { lineStyle: { color: grid, type: 'dashed' } },
        axisLabel: { color: text, fontSize: 10 },
      },
      series: props.series.map((s, i) => ({
        name: s.name,
        type: 'line',
        smooth: true,
        showSymbol: false,
        data: s.data,
        lineStyle: { width: 2, color: s.color },
        itemStyle: { color: s.color },
        areaStyle:
          i === 0
            ? {
                color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
                  { offset: 0, color: s.color + '55' },
                  { offset: 1, color: s.color + '00' },
                ]),
              }
            : undefined,
      })),
    },
    true,
  )
}

onMounted(() => {
  if (!el.value) return
  chart = echarts.init(el.value, undefined, { renderer: 'canvas' })
  render()
  resizeObserver = new ResizeObserver(() => chart?.resize())
  resizeObserver.observe(el.value)
})

watch(() => props.series, render, { deep: true })
watch(() => props.labels, render, { deep: true })
watch(() => theme.isDark, render)

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  chart?.dispose()
  chart = null
})
</script>

<template>
  <div ref="el" :style="{ height: `${height}px`, width: '100%' }" />
</template>

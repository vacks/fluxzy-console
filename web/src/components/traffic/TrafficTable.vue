<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import type { ExchangeRow } from '@/api/types'
import { AlertCircle, Radio } from 'lucide-vue-next'
import Badge from '@/components/ui/Badge.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import {
  contentTypeLabel,
  formatBytes,
  formatDuration,
  formatTime,
  methodTone,
  shortUrl,
  statusTone,
} from '@/utils/format'

const props = withDefaults(
  defineProps<{
    items: ExchangeRow[]
    selectedId?: number
    live?: boolean
    loading?: boolean
    sampled?: boolean
    fill?: boolean
    height?: string
    emptyTitle?: string
    emptyDescription?: string
  }>(),
  {
    live: false,
    loading: false,
    sampled: false,
    fill: false,
    height: 'calc(100vh - 336px)',
    emptyTitle: '暂无流量',
    emptyDescription: 'fluxzy 捕获到请求后，这里会实时显示。',
  },
)

const emit = defineEmits<{ select: [ExchangeRow] }>()

const rowHeight = 46
const overscan = 8
const scroller = ref<HTMLElement>()
const scrollTop = ref(0)
const viewHeight = ref(520)
let ro: ResizeObserver | null = null

const totalHeight = computed(() => props.items.length * rowHeight)
const start = computed(() => Math.max(0, Math.floor(scrollTop.value / rowHeight) - overscan))
const end = computed(() =>
  Math.min(props.items.length, Math.ceil((scrollTop.value + viewHeight.value) / rowHeight) + overscan),
)
const visible = computed(() =>
  props.items.slice(start.value, end.value).map((row, i) => ({ row, index: start.value + i })),
)

const wide = useMediaQuery('(min-width: 1000px)')
const gridCols = computed(() =>
  wide.value
    ? '72px 52px 58px minmax(110px, .85fr) minmax(220px, 2.4fr) 58px 72px 68px 96px'
    : '64px 46px 52px minmax(80px, .9fr) minmax(140px, 2fr)',
)

function onScroll() {
  scrollTop.value = scroller.value?.scrollTop || 0
}

onMounted(() => {
  if (!scroller.value) return
  viewHeight.value = scroller.value.clientHeight
  ro = new ResizeObserver(() => {
    viewHeight.value = scroller.value?.clientHeight || 520
  })
  ro.observe(scroller.value)
})

onBeforeUnmount(() => ro?.disconnect())

let prevFirstId = 0
watch(
  () => props.items[0]?.id,
  (id) => {
    if (!id) return
    if (prevFirstId && id > prevFirstId && props.live) {
      // Keep the same rows under the cursor when new rows are prepended while
      // the user has scrolled away from the live edge.
      if ((scroller.value?.scrollTop || 0) > 8) {
        scroller.value!.scrollTop += rowHeight
      }
    }
    prevFirstId = id
  },
  { flush: 'post' },
)

function scrollToTop() {
  scroller.value?.scrollTo({ top: 0, behavior: 'smooth' })
  scrollTop.value = 0
}
function isAtTop() {
  return (scroller.value?.scrollTop || 0) < 8
}
defineExpose({ scrollToTop, isAtTop })

function rowClass(row: ExchangeRow) {
  return [
    props.selectedId === row.id ? 'bg-accent/8 shadow-[inset_2px_0_0_0_rgb(var(--accent))]' : '',
    row.pending ? 'bg-accent/[.035]' : '',
  ].join(' ')
}
</script>

<template>
  <div class="relative" :class="fill ? 'flex min-h-0 flex-1 flex-col' : ''">
    <div
      class="grid items-center gap-2 border-b border-line/60 bg-panel/95 px-3 py-2 text-[10px] font-semibold uppercase tracking-[0.1em] text-faint backdrop-blur"
      :style="{ gridTemplateColumns: gridCols }"
    >
      <span class="flex items-center gap-1.5">
        <Radio v-if="live" class="h-3 w-3 text-ok" />
        Time
      </span>
      <span>Method</span>
      <span>Status</span>
      <span>Host</span>
      <span>Path</span>
      <span v-if="wide">Type</span>
      <span v-if="wide" class="text-right">Size</span>
      <span v-if="wide" class="text-right">Time</span>
      <span v-if="wide">Tags</span>
    </div>

    <div
      ref="scroller"
      class="relative overflow-y-auto overflow-x-hidden"
      :class="fill ? 'min-h-0 flex-1' : 'min-h-[320px]'"
      :style="fill ? undefined : { height }"
      @scroll.passive="onScroll"
    >
      <div class="relative w-full" :style="{ height: `${totalHeight}px` }">
        <div
          v-for="{ row, index } in visible"
          :key="row.id"
          class="absolute left-0 top-0 grid w-full cursor-pointer items-center gap-2 border-b border-line/30 px-3 transition-colors duration-100 hover:bg-panel-strong/55"
          :style="{ height: `${rowHeight}px`, transform: `translateY(${index * rowHeight}px)`, gridTemplateColumns: gridCols }"
          :class="rowClass(row)"
          @click="emit('select', row)"
        >
          <span class="font-mono text-[10.5px] tabular-nums text-faint">{{ formatTime(row.startedAt) }}</span>

          <span class="inline-flex w-fit items-center rounded-md px-1.5 py-0.5 font-mono text-[10.5px] font-semibold" :class="methodTone(row.method)">
            {{ row.method || '—' }}
          </span>

          <span class="flex items-center gap-1.5">
            <span
              class="inline-flex items-center rounded-md px-1.5 py-0.5 font-mono text-[10.5px] font-semibold"
              :class="{
                'bg-ok/12 text-ok': statusTone(row.status) === 'ok',
                'bg-warn/12 text-warn': statusTone(row.status) === 'warn',
                'bg-bad/12 text-bad': statusTone(row.status) === 'bad',
                'bg-accent/12 text-accent': statusTone(row.status) === 'accent',
                'bg-panel-strong text-faint': statusTone(row.status) === 'muted',
              }"
            >
              {{ row.pending && !row.status ? '···' : row.status || '—' }}
            </span>
            <span v-if="row.pending" class="h-1.5 w-1.5 animate-pulse rounded-full bg-accent" />
            <AlertCircle v-if="row.hasError" class="h-3 w-3 text-bad" />
          </span>

          <span class="truncate font-mono text-[11px] text-muted" :title="row.host">{{ row.host || '—' }}</span>

          <span class="flex min-w-0 items-center gap-1.5">
            <span class="truncate font-mono text-[11px] text-muted" :title="row.url">{{ shortUrl(row.path || row.url, 120) }}</span>
            <span v-if="row.isWebSocket" class="badge-accent shrink-0">WS</span>
          </span>

          <span v-if="wide">
            <Badge variant="muted">{{ contentTypeLabel(row.contentType) }}</Badge>
          </span>

          <span v-if="wide" class="text-right font-mono text-[10.5px] text-muted">{{ formatBytes(row.reqSize + row.respSize) }}</span>

          <span
            v-if="wide"
            class="text-right font-mono text-[10.5px]"
            :class="row.durationMs > 1000 ? 'text-warn' : row.durationMs > 300 ? 'text-muted' : 'text-faint'"
          >
            {{ formatDuration(row.durationMs) }}
          </span>

          <span v-if="wide" class="flex min-w-0 items-center gap-1">
            <span
              v-for="tag in row.tags.slice(0, 1)"
              :key="tag"
              class="max-w-[70px] truncate rounded-md bg-accent/10 px-1.5 py-0.5 text-[10px] text-accent"
            >{{ tag }}</span>
            <span v-if="row.tags.length > 1" class="text-[10px] text-faint">+{{ row.tags.length - 1 }}</span>
          </span>
        </div>
      </div>

      <div v-if="!items.length && !loading" class="absolute inset-0 flex items-center justify-center">
        <EmptyState :title="emptyTitle" :description="emptyDescription" />
      </div>
      <div v-if="loading" class="absolute inset-0 flex items-center justify-center bg-panel/40 backdrop-blur-[1px]">
        <div class="flex items-center gap-2 rounded-xl border border-line/60 bg-panel px-4 py-2 text-[12px] text-muted">
          <span class="h-3.5 w-3.5 animate-spin rounded-full border-2 border-accent/30 border-t-accent" />
          加载中…
        </div>
      </div>
    </div>
  </div>
</template>

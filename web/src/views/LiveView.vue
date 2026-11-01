<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import {
  ArrowDownToLine,
  ChevronUp,
  CirclePause,
  CirclePlay,
  Download,
  ExternalLink,
  Radio,
  Trash2,
  Wifi,
  WifiOff,
} from 'lucide-vue-next'
import type { ExchangeRow } from '@/api/types'
import { fetchDownload } from '@/api/client'
import { useCaptureStore } from '@/stores/capture'
import { useLiveStore } from '@/stores/live'
import { useToastStore } from '@/stores/toast'
import Button from '@/components/ui/Button.vue'
import Badge from '@/components/ui/Badge.vue'
import StatusDot from '@/components/ui/StatusDot.vue'
import TrafficTable from '@/components/traffic/TrafficTable.vue'
import TrafficFilters from '@/components/traffic/TrafficFilters.vue'
import type { TrafficFilter } from '@/components/traffic/filters'
import ExchangeDetail from '@/components/traffic/ExchangeDetail.vue'
import { formatBytes, formatNumber, formatRate } from '@/utils/format'

const capture = useCaptureStore()
const live = useLiveStore()
const toast = useToastStore()

const filter = ref<TrafficFilter>({ q: '', method: '', statusClass: '', pending: false, errors: false })
const selected = ref<ExchangeRow | null>(null)
const drawerOpen = ref(false)
const exportingHar = ref(false)
const tableRef = ref<{ scrollToTop: () => void; isAtTop: () => boolean } | null>(null)

const filtered = computed(() => {
  const f = filter.value
  const q = f.q.trim().toLowerCase()
  return live.events.filter((r) => {
    if (q && !(`${r.url} ${r.host} ${r.path}`.toLowerCase().includes(q))) return false
    if (f.method && r.method.toUpperCase() !== f.method) return false
    if (f.statusClass) {
      const n = r.status
      if (f.statusClass === '2xx' && !(n >= 200 && n < 300)) return false
      if (f.statusClass === '3xx' && !(n >= 300 && n < 400)) return false
      if (f.statusClass === '4xx' && !(n >= 400 && n < 500)) return false
      if (f.statusClass === '5xx' && !(n >= 500)) return false
    }
    if (f.pending && !r.pending) return false
    if (f.errors && !r.hasError) return false
    return true
  })
})

const liveSessionId = computed(() => live.activeSessionId || capture.activeSessionId)
const sampledNotice = computed(() => capture.stats.rps > live.maxPerSec || live.sampled)

const isEmpty = computed(() => !live.events.length)

function openRow(row: ExchangeRow) {
  selected.value = row
  drawerOpen.value = true
}

function togglePause() {
  if (live.paused) live.resume()
  else live.pause()
}

function jumpLatest() {
  tableRef.value?.scrollToTop()
  live.markSeen()
}

async function exportHar() {
  if (!liveSessionId.value) return
  exportingHar.value = true
  try {
    await fetchDownload(
      `/capture/sessions/${encodeURIComponent(liveSessionId.value)}/export?format=har`,
      `${liveSessionId.value}.har`,
    )
    toast.success('导出完成', '浏览器已开始下载文件')
  } catch (e: any) {
    toast.error('导出失败', e?.message || '未知错误')
  } finally {
    exportingHar.value = false
  }
}

watch(
  () => live.events.length,
  () => {
    if (tableRef.value?.isAtTop()) live.markSeen()
  },
)

watch(
  () => live.activeSessionId,
  () => {
    drawerOpen.value = false
    selected.value = null
  },
)

onMounted(() => {
  live.enableBackfill()
  if (!live.events.length && live.loadedSessionId !== liveSessionId.value && liveSessionId.value) {
    void live.loadInitial(liveSessionId.value)
  }
})

onUnmounted(() => {
  live.disableBackfill()
})
</script>

<template>
  <div class="flex h-[calc(100vh-7rem)] flex-col gap-4">
    <!-- header -->
    <section class="panel flex flex-col gap-4 p-4 lg:flex-row lg:items-center lg:justify-between">
      <div class="flex min-w-0 items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-accent/10">
          <Radio class="h-5 w-5 text-accent" :class="live.connected ? 'animate-pulse' : ''" />
        </div>
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="text-[15px] font-semibold tracking-tight text-ink">实时流量</h2>
            <Badge :variant="live.connected ? 'ok' : 'muted'" dot>
              {{ live.connected ? 'live' : 'disconnected' }}
            </Badge>
            <Badge v-if="live.paused" variant="warn">已暂停显示</Badge>
            <Badge
              v-if="sampledNotice"
              variant="accent"
              :title="`实时推送每秒最多 ${live.maxPerSec} 条交换事件；超出的按抽样显示，完整记录请在当前会话索引中查看`"
            >
              实时推送限流 {{ live.maxPerSec }}/s · 抽样显示
            </Badge>
          </div>
          <div class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted">
            <span class="font-mono">{{ liveSessionId || '等待会话' }}</span>
            <span class="flex items-center gap-1">
              <StatusDot :tone="capture.online ? 'ok' : 'muted'" :pulse="capture.online" />
              {{ capture.online ? 'fluxzy 在线' : 'fluxzy 离线' }}
            </span>
            <span title="实时列表只保留最近 5,000 条；当前会话的全部索引记录可在“完整会话”中查看">
              实时列表 {{ formatNumber(live.events.length) }} 条（窗口 5,000，完整记录见当前会话）
            </span>
          </div>
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <button
          v-if="live.newCount > 0"
          class="btn border-accent/30 bg-accent/10 text-accent"
          @click="jumpLatest"
        >
          <ChevronUp class="h-3.5 w-3.5" /> {{ live.newCount }} 条新流量
        </button>
        <Button :variant="live.paused ? 'primary' : 'secondary'" @click="togglePause">
          <template #icon>
            <component :is="live.paused ? CirclePlay : CirclePause" class="h-4 w-4" />
          </template>
          {{ live.paused ? '恢复显示' : '暂停显示' }}
        </Button>
        <Button variant="ghost" :disabled="isEmpty" @click="live.clear()">
          <template #icon><Trash2 class="h-3.5 w-3.5" /></template>
          清空
        </Button>
        <Button variant="ghost" :disabled="!liveSessionId" :loading="exportingHar" @click="exportHar">
          <template #icon><Download class="h-3.5 w-3.5" /></template>
          HAR
        </Button>
        <RouterLink v-if="liveSessionId" :to="{ name: 'session', params: { id: liveSessionId } }">
          <Button variant="ghost" title="查看当前会话的全部索引记录">
            <template #icon><ExternalLink class="h-3.5 w-3.5" /></template>
            完整会话
          </Button>
        </RouterLink>
      </div>
    </section>

    <!-- filters -->
    <section class="panel p-3">
      <TrafficFilters v-model="filter" />
    </section>

    <!-- table -->
    <section class="panel flex min-h-0 flex-1 flex-col overflow-hidden">
      <div v-if="!capture.online && !live.events.length" class="flex items-center gap-3 border-b border-line/50 bg-panel-strong/40 px-4 py-2.5 text-[12px] text-muted">
        <WifiOff class="h-4 w-4 text-faint" />
        fluxzy 不可达。请确认容器已启动，并检查 fluxzy 地址 {{ capture.status?.proxyAddress }}。
      </div>
      <TrafficTable
        ref="tableRef"
        :items="filtered"
        :selected-id="selected?.id"
        live
        fill
        :sampled="sampledNotice"
        empty-title="等待流量"
        empty-description="fluxzy 捕获到请求后，这里会实时显示；如果一直没有数据，请检查 dump 目录是否挂载到 /data。"
        @select="openRow"
      />
      <div class="flex flex-wrap items-center gap-x-4 gap-y-1 border-t border-line/50 bg-panel-strong/30 px-4 py-2 text-[10.5px] text-faint">
        <span class="flex items-center gap-1.5">
          <component :is="live.connected ? Wifi : WifiOff" class="h-3 w-3" :class="live.connected ? 'text-ok' : 'text-bad'" />
          {{ live.connected ? 'WebSocket 已连接' : 'WebSocket 未连接' }}
        </span>
        <span>RPS <b class="font-mono text-muted">{{ Math.round(capture.stats.rps) }}</b></span>
        <span>带宽 <b class="font-mono text-muted">{{ formatRate(capture.stats.bytesPerSec) }}</b></span>
        <span>累计 <b class="font-mono text-muted">{{ formatBytes(capture.stats.reqBytes + capture.stats.respBytes) }}</b></span>
        <span title="本次 WebSocket 连接成功推送到浏览器的交换事件数">
          推送 <b class="font-mono text-muted">{{ formatNumber(live.sent) }}</b>
        </span>
        <span title="因实时限流、暂停或推送缓冲已满而未通过 WebSocket 推送的交换事件；实时页按抽样显示，完整记录请在当前会话索引中查看">
          · 实时抽样跳过 <b class="font-mono text-muted">{{ formatNumber(live.dropped) }}</b>
        </span>
        <span v-if="live.recovered" title="最近窗口刷新时补充的新记录数">
          · 窗口刷新 <b class="font-mono text-muted">{{ formatNumber(live.recovered) }}</b>
        </span>
        <span class="ml-auto flex items-center gap-1">
          <ArrowDownToLine class="h-3 w-3" />
          {{ sampledNotice ? '高流量：实时推送限流，当前为抽样显示' : '实时推送' }}
        </span>
      </div>
    </section>

    <ExchangeDetail v-model="drawerOpen" :session-id="liveSessionId" :row="selected" />
  </div>
</template>

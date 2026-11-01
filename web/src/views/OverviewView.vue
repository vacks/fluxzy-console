<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import {
  Activity,
  AlertTriangle,
  ArrowUpRight,
  Gauge,
  History,
  Radio,
  RefreshCw,
  Server,
  SlidersHorizontal,
  Waves,
} from 'lucide-vue-next'
import { api } from '@/api/client'
import type { HostCount, RulesStatus } from '@/api/types'
import { useCaptureStore } from '@/stores/capture'
import { useLiveStore } from '@/stores/live'
import Card from '@/components/ui/Card.vue'
import Button from '@/components/ui/Button.vue'
import Badge from '@/components/ui/Badge.vue'
import StatCard from '@/components/ui/StatCard.vue'
import StatusDot from '@/components/ui/StatusDot.vue'
import TrafficChart from '@/components/charts/TrafficChart.vue'
import StatusDonut from '@/components/charts/StatusDonut.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import {
  formatBytes,
  formatDateTime,
  formatNumber,
  formatRate,
  formatRelative,
  percent,
} from '@/utils/format'

const capture = useCaptureStore()
const live = useLiveStore()

const rules = ref<RulesStatus | null>(null)
const topHosts = ref<HostCount[]>([])
const ring = ref<number[]>([])
const labels = ref<string[]>([])
const recent = ref<{
  sessionId: string
  at: number
  count: number
  bytes: number
  active: boolean
  sampled: boolean
  expired: boolean
}[]>([])
let ticker: number | undefined

const liveRps = computed(() => live.stats?.rps ?? capture.stats.rps)
const errorCount = computed(() => capture.stats.status4xx + capture.stats.status5xx + capture.stats.errors)
const errorRate = computed(() => percent(errorCount.value, capture.stats.total))
const totalBytes = computed(() => capture.stats.reqBytes + capture.stats.respBytes)

async function loadRules() {
  try {
    rules.value = await api.get<RulesStatus>('/rules')
  } catch {
    rules.value = null
  }
}

async function loadTopHosts() {
  const id = capture.activeSessionId
  if (!id) {
    topHosts.value = []
    return
  }
  try {
    const r = await api.get<{ items: HostCount[] }>(
      `/capture/sessions/${encodeURIComponent(id)}/top-hosts`,
    )
    topHosts.value = r.items || []
  } catch {
    topHosts.value = []
  }
}

async function loadRecent() {
  await capture.fetchSessions(6)
  recent.value = capture.sessions.map((s) => ({
    sessionId: s.id,
    at: s.captureDate,
    count: s.exchangeCount,
    bytes: s.totalBytes,
    active: !!s.active,
    sampled: !s.active && !!s.hasCold,
    expired: !s.active && !s.hasRaw && !s.hasCold,
  }))
}

function pushPoint() {
  const rps = Math.max(0, Math.round(liveRps.value))
  ring.value = [...ring.value.slice(-59), rps]
  labels.value = [
    ...labels.value.slice(-59),
    new Date().toLocaleTimeString('zh-CN', { hour12: false }),
  ]
}

const chartSeries = computed(() => [{ name: 'RPS', color: '#818cf8', data: ring.value }])

const sessionAge = computed(() =>
  capture.activeSession?.captureDate ? formatRelative(capture.activeSession.captureDate) : '—',
)

onMounted(() => {
  pushPoint()
  startTicker()
  document.addEventListener('visibilitychange', onVisibility)
  void (async () => {
    try {
      await Promise.all([loadRules(), loadRecent()])
    } catch {
      // A sessions/rules failure must not prevent the live chart timer.
    }
    try {
      await loadTopHosts()
    } catch {
      topHosts.value = []
    }
  })()
})

function startTicker() {
  if (ticker) return
  ticker = window.setInterval(() => {
    pushPoint()
  }, 2000)
}

function stopTicker() {
  if (ticker) {
    window.clearInterval(ticker)
    ticker = undefined
  }
}

function onVisibility() {
  if (document.hidden) {
    stopTicker()
  } else {
    pushPoint()
    startTicker()
  }
}

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibility)
  stopTicker()
})

watch(
  () => capture.activeSessionId,
  async () => {
    ring.value = []
    labels.value = []
    await loadTopHosts()
    await loadRules()
  },
)

watch(
  () => rules.value?.pending,
  () => {
    // The banner is reactive; no extra fetch needed.
  },
)
</script>

<template>
  <div class="space-y-5">
    <!-- pending / offline notices -->
    <div
      v-if="rules?.pending"
      class="flex flex-wrap items-center gap-3 rounded-2xl border border-warn/25 bg-warn/8 px-4 py-3 text-[12.5px] text-warn"
    >
      <AlertTriangle class="h-4 w-4 shrink-0" />
      <span class="font-medium">规则已修改，等待应用。</span>
      <span class="text-warn/75">在规则页点击“立即应用”即可热更新规则。</span>
      <RouterLink to="/rules" class="ml-auto inline-flex items-center gap-1 text-[12px] font-medium underline-offset-4 hover:underline">
        去规则管理 <ArrowUpRight class="h-3.5 w-3.5" />
      </RouterLink>
    </div>

    <div
      v-if="!capture.online"
      class="flex flex-wrap items-center gap-3 rounded-2xl border border-line/60 bg-panel/60 px-4 py-3 text-[12.5px] text-muted"
    >
      <StatusDot tone="muted" />
      <span>fluxzy 当前不可达（{{ capture.status?.proxyAddress || '127.0.0.1:443' }}）。</span>
      <span class="text-faint">请到“运行时”页面查看进程状态和日志。</span>
      <RouterLink to="/runtime" class="ml-auto text-[12px] font-medium text-accent underline-offset-4 hover:underline">打开运行时</RouterLink>
    </div>

    <!-- hero -->
    <section class="panel relative overflow-hidden p-5 lg:p-6">
      <div class="pointer-events-none absolute inset-0 -z-0">
        <div class="absolute -right-24 -top-32 h-[320px] w-[320px] rounded-full bg-accent/12 blur-[90px]" />
        <div class="absolute -bottom-24 left-[18%] h-[240px] w-[240px] rounded-full bg-accent-2/8 blur-[80px]" />
      </div>
      <div class="relative flex flex-col justify-between gap-5 lg:flex-row lg:items-center">
        <div class="min-w-0">
          <div class="flex items-center gap-2 text-[11px] font-medium uppercase tracking-[0.14em] text-faint">
            <Radio class="h-3.5 w-3.5 text-accent" /> Capture Overview
          </div>
          <div class="mt-2 flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <h2 class="text-[22px] font-semibold tracking-tight text-ink">
              {{ capture.online ? '采集运行中' : '等待 fluxzy' }}
            </h2>
            <span v-if="capture.activeSession" class="font-mono text-[12px] text-muted">{{ capture.activeSession.id }}</span>
            <Badge v-if="capture.activeSession" :variant="capture.online ? 'ok' : 'warn'" dot>
              {{ capture.online ? 'live' : 'offline' }}
            </Badge>
          </div>
          <div class="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-[11.5px] text-muted">
            <span>会话开始于 {{ sessionAge }}</span>
            <span v-if="capture.status?.indexing" class="flex items-center gap-1.5 text-accent">
              <RefreshCw class="h-3 w-3 animate-spin" /> 正在建立索引
            </span>
            <span v-else class="flex items-center gap-1.5">
              <StatusDot :tone="capture.online ? 'ok' : 'muted'" :pulse="capture.online" />
              {{ capture.online ? 'fluxzy 可达' : 'fluxzy 不可达' }}
            </span>
          </div>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <RouterLink to="/live">
            <Button variant="primary">
              <template #icon><Activity class="h-4 w-4" /></template>
              实时流量
            </Button>
          </RouterLink>
          <RouterLink to="/rules">
            <Button>
              <template #icon><SlidersHorizontal class="h-4 w-4" /></template>
              规则管理
            </Button>
          </RouterLink>
          <RouterLink to="/sessions">
            <Button variant="ghost">
              <template #icon><History class="h-4 w-4" /></template>
              会话历史
            </Button>
          </RouterLink>
        </div>
      </div>
    </section>

    <!-- stat cards -->
    <section class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <StatCard
        label="Total Requests"
        :value="formatNumber(capture.stats.total)"
        :hint="`进行中 ${formatNumber(capture.stats.pending)} · 错误 ${formatNumber(errorCount)}`"
        :icon="Server"
        tone="accent"
      />
      <StatCard
        label="Current RPS"
        :value="formatNumber(Math.round(liveRps))"
        :hint="capture.online ? '实时采样 · 8 秒滑动窗口' : '等待采集'"
        :icon="Gauge"
        tone="cyan"
        :spark="ring"
      />
      <StatCard
        label="Bandwidth"
        :value="formatRate(capture.stats.bytesPerSec)"
        :hint="`累计 ${formatBytes(totalBytes)}`"
        :icon="Waves"
        tone="ok"
      />
      <StatCard
        label="Error Rate"
        :value="`${errorRate}%`"
        :hint="`4xx ${formatNumber(capture.stats.status4xx)} · 5xx ${formatNumber(capture.stats.status5xx)}`"
        :icon="AlertTriangle"
        :tone="errorRate > 5 ? 'bad' : errorRate > 1 ? 'warn' : 'ok'"
      />
    </section>

    <!-- chart + donut -->
    <section class="grid gap-4 xl:grid-cols-[1.55fr_1fr]">
      <Card title="实时吞吐" subtitle="最近 120 秒 · RPS">
        <template #actions>
          <Badge variant="muted">{{ Math.round(liveRps) }} rps</Badge>
        </template>
        <TrafficChart v-if="ring.length" :labels="labels" :series="chartSeries" :height="246" />
        <EmptyState v-else title="等待流量" description="fluxzy 开始接收请求后，这里会显示实时 RPS 曲线。" />
      </Card>

      <Card title="响应状态分布" subtitle="当前会话累计">
        <StatusDonut
          :s2="capture.stats.status2xx"
          :s3="capture.stats.status3xx"
          :s4="capture.stats.status4xx"
          :s5="capture.stats.status5xx"
          :pending="capture.stats.pending"
        />
      </Card>
    </section>

    <!-- top hosts + recent sessions -->
    <section class="grid gap-4 xl:grid-cols-2">
      <Card title="Top Hosts" subtitle="按请求数排序">
        <template #actions>
          <Button variant="ghost" size="sm" @click="loadTopHosts">
            <template #icon><RefreshCw class="h-3.5 w-3.5" /></template>
            刷新
          </Button>
        </template>
        <div v-if="topHosts.length" class="space-y-3">
          <div v-for="(h, i) in topHosts.slice(0, 8)" :key="h.host" class="group">
            <div class="flex items-center justify-between gap-3 text-[11.5px]">
              <div class="flex min-w-0 items-center gap-2">
                <span class="w-4 text-right font-mono text-[10px] text-faint">{{ i + 1 }}</span>
                <span class="truncate font-mono text-muted group-hover:text-ink">{{ h.host }}</span>
              </div>
              <span class="shrink-0 font-mono text-ink">{{ formatNumber(h.count) }}</span>
            </div>
            <div class="mt-1.5 h-1 overflow-hidden rounded-full bg-panel-strong">
              <div
                class="h-full rounded-full bg-gradient-to-r from-accent to-accent-2 transition-all duration-500"
                :style="{ width: `${Math.max(3, (h.count / (topHosts[0]?.count || 1)) * 100)}%` }"
              />
            </div>
          </div>
        </div>
        <EmptyState v-else title="还没有域名统计" description="采集到流量后，这里会按请求数展示 Top Hosts。" />
      </Card>

      <Card title="最近会话" subtitle="每次 fluxzy 重启或轮转生成一个新会话">
        <template #actions>
          <RouterLink to="/sessions" class="btn-ghost px-2 py-1 text-[11.5px]">查看全部</RouterLink>
        </template>
        <div v-if="recent.length" class="divide-y divide-line/40">
          <RouterLink
            v-for="s in recent"
            :key="s.sessionId"
            :to="{ name: 'session', params: { id: s.sessionId } }"
            class="flex items-center gap-3 py-2.5 transition hover:bg-panel-strong/40"
          >
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span class="truncate font-mono text-[11.5px] text-ink">{{ s.sessionId }}</span>
                <Badge :variant="s.active ? 'ok' : s.sampled || s.expired ? 'warn' : 'muted'">
                  {{ s.active ? 'live' : s.sampled ? 'sampled' : s.expired ? 'expired' : 'archived' }}
                </Badge>
              </div>
              <div class="mt-0.5 text-[10.5px] text-faint">{{ formatDateTime(s.at) }}</div>
            </div>
            <div class="text-right">
              <div class="font-mono text-[11.5px] text-muted">{{ formatNumber(s.count) }} req</div>
              <div class="text-[10.5px] text-faint">{{ formatBytes(s.bytes) }}</div>
            </div>
            <ArrowUpRight class="h-3.5 w-3.5 shrink-0 text-faint" />
          </RouterLink>
        </div>
        <EmptyState v-else title="还没有会话" description="确认 fluxzy 已启动，并且 dump 目录挂载到了 /data。" />
      </Card>
    </section>

    <div class="flex items-center justify-center gap-2 pb-2 text-[10.5px] text-faint">
      <span>状态更新于 {{ formatRelative(capture.lastUpdated) }}</span>
      <span>·</span>
      <span>{{ live.sampled || capture.stats.rps > live.maxPerSec ? '高流量：仅实时列表限流，统计与历史记录为全量' : '实时索引正常' }}</span>
    </div>
  </div>
</template>

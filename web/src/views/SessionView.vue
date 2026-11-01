<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import {
  Activity,
  ArrowLeft,
  Download,
  FileJson,
  Pin,
  PinOff,
  RefreshCw,
  SlidersHorizontal,
} from 'lucide-vue-next'
import { api, fetchDownload, qs } from '@/api/client'
import type { CaptureSession, ExchangeList, ExchangeRow, Stats } from '@/api/types'
import { useCaptureStore } from '@/stores/capture'
import { useToastStore } from '@/stores/toast'
import Button from '@/components/ui/Button.vue'
import Badge from '@/components/ui/Badge.vue'
import Modal from '@/components/ui/Modal.vue'
import StatusDot from '@/components/ui/StatusDot.vue'
import CodeBlock from '@/components/ui/CodeBlock.vue'
import TrafficTable from '@/components/traffic/TrafficTable.vue'
import TrafficFilters from '@/components/traffic/TrafficFilters.vue'
import type { TrafficFilter } from '@/components/traffic/filters'
import ExchangeDetail from '@/components/traffic/ExchangeDetail.vue'
import { formatBytes, formatDateTime, formatNumber, formatRelative } from '@/utils/format'

const route = useRoute()
const capture = useCaptureStore()
const toast = useToastStore()

const id = computed(() => String(route.params.id || ''))
const session = ref<CaptureSession | null>(null)
const meta = ref<any>(null)
const indexErrors = ref(0)
const stats = ref<Stats | null>(null)
const items = ref<ExchangeRow[]>([])
const loading = ref(false)
const loadingMore = ref(false)
const nextCursor = ref(0)
const hasMore = ref(false)
const cold = ref(false)
const error = ref('')
const exporting = ref(false)
const filter = ref<TrafficFilter>({ q: '', method: '', statusClass: '', pending: false, errors: false })
const selected = ref<ExchangeRow | null>(null)
const drawerOpen = ref(false)
const configOpen = ref(false)
let debounce: number | undefined
let loadSeq = 0

const isActive = computed(() => session.value?.active && capture.activeSessionId === id.value)
const statusLabel = computed(() => {
  if (!session.value) return '—'
  if (isActive.value) return capture.online ? 'live' : 'offline'
  if (session.value.status === 'indexing') return 'indexing'
  if (session.value.hasCold) return 'sampled'
  if (session.value.status === 'expired' || !session.value.hasRaw) return 'expired'
  return 'archived'
})
const statusVariant = computed<'ok' | 'warn' | 'accent' | 'muted'>(() =>
  statusLabel.value === 'live'
    ? 'ok'
    : statusLabel.value === 'offline' || statusLabel.value === 'sampled' || statusLabel.value === 'expired'
      ? 'warn'
      : statusLabel.value === 'indexing'
        ? 'accent'
        : 'muted',
)

const total = computed(() => stats.value?.total || session.value?.exchangeCount || 0)
const errorsCount = computed(() => (stats.value?.status4xx || 0) + (stats.value?.status5xx || 0) + (stats.value?.errors || 0))
const totalBytes = computed(() => (stats.value?.reqBytes || 0) + (stats.value?.respBytes || 0) || session.value?.totalBytes || 0)

async function loadMeta() {
  const requested = id.value
  try {
    const r = await api.get<{ session: CaptureSession; meta?: any; indexErrors: number }>(
      `/capture/sessions/${encodeURIComponent(requested)}`,
    )
    if (id.value !== requested) return
    session.value = r.session
    meta.value = r.meta || null
    indexErrors.value = r.indexErrors || 0
  } catch (e: any) {
    if (id.value === requested) error.value = e?.message || '读取会话失败'
  }
}

async function loadStats() {
  const requested = id.value
  try {
    const value = await api.get<Stats>(`/capture/sessions/${encodeURIComponent(requested)}/stats`)
    if (id.value !== requested) return
    stats.value = value
  } catch {
    if (id.value === requested) stats.value = null
  }
}

async function load(reset = false) {
  const requested = id.value
  const seq = ++loadSeq
  if (reset) {
    loading.value = true
    error.value = ''
    items.value = []
    nextCursor.value = 0
    hasMore.value = false
    cold.value = false
    selected.value = null
    drawerOpen.value = false
  } else {
    loadingMore.value = true
  }
  try {
    const q = qs({
      limit: 200,
      before: reset ? undefined : nextCursor.value || undefined,
      q: filter.value.q || undefined,
      method: filter.value.method || undefined,
      statusClass: filter.value.statusClass || undefined,
      pending: filter.value.pending ? 1 : undefined,
      errors: filter.value.errors ? 1 : undefined,
    })
    const r = await api.get<ExchangeList>(`/capture/sessions/${encodeURIComponent(requested)}/exchanges${q}`)
    if (id.value !== requested || seq !== loadSeq) return
    if (reset) {
      items.value = r.items || []
      cold.value = !!r.cold
    } else {
      items.value = [...items.value, ...(r.items || [])]
    }
    nextCursor.value = r.nextCursor || 0
    hasMore.value = (r.items || []).length >= 200 && (r.nextCursor || 0) > 0
  } catch (e: any) {
    if (id.value !== requested || seq !== loadSeq) return
    if (reset) error.value = e?.message || '读取流量失败'
    else toast.error('加载更多失败', e?.message)
  } finally {
    if (id.value === requested && seq === loadSeq) {
      loading.value = false
      loadingMore.value = false
    }
  }
}

async function refreshAll() {
  await Promise.all([loadMeta(), loadStats()])
  await load(true)
}

onMounted(refreshAll)
watch(id, () => {
  loadSeq++
  void refreshAll()
})

watch(
  filter,
  () => {
    window.clearTimeout(debounce)
    debounce = window.setTimeout(() => void load(true), 300)
  },
  { deep: true },
)

function onSearch() {
  window.clearTimeout(debounce)
  void load(true)
}

onBeforeUnmount(() => window.clearTimeout(debounce))

async function togglePin() {
  if (!session.value) return
  try {
    await api.post(`/capture/sessions/${encodeURIComponent(id.value)}/pin`, { pinned: !session.value.pinned })
    session.value.pinned = !session.value.pinned
    toast.success(session.value.pinned ? '已固定保留' : '已取消固定')
  } catch (e: any) {
    toast.error('操作失败', e?.message)
  }
}

async function exportHar() {
  exporting.value = true
  try {
    await fetchDownload(
      `/capture/sessions/${encodeURIComponent(id.value)}/export?format=har`,
      `${id.value}.har`,
    )
    toast.success('导出完成', '浏览器已开始下载文件')
  } catch (e: any) {
    toast.error('导出失败', e?.message || '未知错误')
  } finally {
    exporting.value = false
  }
}

const configJSON = computed(() => {
  if (!meta.value?.capturedSetting) return ''
  return JSON.stringify(meta.value.capturedSetting, null, 2)
})
</script>

<template>
  <div class="space-y-4">
    <section class="panel p-4 lg:p-5">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div class="min-w-0">
          <RouterLink to="/sessions" class="mb-2 inline-flex items-center gap-1.5 text-[11px] text-muted transition hover:text-ink">
            <ArrowLeft class="h-3.5 w-3.5" /> 会话历史
          </RouterLink>
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="font-mono text-[17px] font-semibold tracking-tight text-ink">{{ id }}</h2>
            <Badge variant="muted">
              <StatusDot :tone="statusVariant" :pulse="statusLabel === 'live'" />
              {{ statusLabel }}
            </Badge>
            <Badge v-if="session?.pinned" variant="accent">pinned</Badge>
            <Badge v-if="cold" variant="warn">仅采样索引</Badge>
          </div>
          <div class="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted">
            <span>{{ session ? formatDateTime(session.captureDate) : '—' }}（{{ session ? formatRelative(session.captureDate) : '—' }}）</span>
            <span>{{ formatNumber(session?.exchangeCount) }} 条请求</span>
            <span>{{ formatBytes(session?.totalBytes) }}</span>
            <span v-if="indexErrors" class="text-warn">{{ indexErrors }} 个索引错误</span>
          </div>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <RouterLink v-if="isActive" to="/live">
            <Button variant="primary">
              <template #icon><Activity class="h-4 w-4" /></template>
              实时视图
            </Button>
          </RouterLink>
          <Button variant="ghost" @click="togglePin">
            <template #icon>
              <component :is="session?.pinned ? PinOff : Pin" class="h-4 w-4" />
            </template>
            {{ session?.pinned ? '取消固定' : '固定保留' }}
          </Button>
          <Button variant="ghost" :disabled="!session?.hasRaw" :loading="exporting" @click="exportHar">
            <template #icon><Download class="h-4 w-4" /></template>
            HAR
          </Button>
          <Button variant="ghost" :disabled="!configJSON" @click="configOpen = true">
            <template #icon><SlidersHorizontal class="h-4 w-4" /></template>
            会话配置
          </Button>
          <Button variant="ghost" :loading="loading" @click="refreshAll">
            <template #icon><RefreshCw class="h-4 w-4" /></template>
          </Button>
        </div>
      </div>
    </section>

    <section class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <div class="panel p-3.5">
        <div class="text-[10px] uppercase tracking-wider text-faint">Requests</div>
        <div class="mt-1 font-mono text-[20px] font-semibold text-ink">{{ formatNumber(total) }}</div>
        <div class="mt-1 text-[10.5px] text-faint">{{ formatNumber(stats?.pending || 0) }} 进行中</div>
      </div>
      <div class="panel p-3.5">
        <div class="text-[10px] uppercase tracking-wider text-faint">2xx Success</div>
        <div class="mt-1 font-mono text-[20px] font-semibold text-ok">{{ formatNumber(stats?.status2xx || 0) }}</div>
        <div class="mt-1 text-[10.5px] text-faint">{{ stats?.total ? ((stats.status2xx / stats.total) * 100).toFixed(1) : '0.0' }}%</div>
      </div>
      <div class="panel p-3.5">
        <div class="text-[10px] uppercase tracking-wider text-faint">Errors</div>
        <div class="mt-1 font-mono text-[20px] font-semibold" :class="errorsCount ? 'text-bad' : 'text-ink'">{{ formatNumber(errorsCount) }}</div>
        <div class="mt-1 text-[10.5px] text-faint">4xx {{ formatNumber(stats?.status4xx || 0) }} · 5xx {{ formatNumber(stats?.status5xx || 0) }}</div>
      </div>
      <div class="panel p-3.5">
        <div class="text-[10px] uppercase tracking-wider text-faint">Data</div>
        <div class="mt-1 font-mono text-[20px] font-semibold text-ink">{{ formatBytes(totalBytes) }}</div>
        <div class="mt-1 text-[10.5px] text-faint">{{ cold ? '采样索引' : '原始数据可用' }}</div>
      </div>
    </section>

    <div v-if="cold" class="flex items-center gap-2 rounded-2xl border border-warn/25 bg-warn/8 px-4 py-2.5 text-[12px] text-warn">
      <FileJson class="h-4 w-4" /> 该会话的原始文件已清理，当前仅保留采样索引；header/body 不可用。
    </div>

    <section class="panel p-3">
      <TrafficFilters v-model="filter" @search="onSearch" />
    </section>

    <section class="panel overflow-hidden">
      <TrafficTable
        :items="items"
        :selected-id="selected?.id"
        :loading="loading"
        :sampled="cold"
        height="calc(100vh - 470px)"
        empty-title="没有匹配的请求"
        empty-description="调整筛选条件，或确认该会话已经完成索引。"
        @select="selected = $event; drawerOpen = true"
      />
      <div class="flex items-center justify-between gap-3 border-t border-line/50 bg-panel-strong/30 px-4 py-2.5 text-[11px] text-faint">
        <span>已加载 {{ formatNumber(items.length) }} 条{{ hasMore ? ' · 还有更多' : '' }}</span>
        <Button v-if="hasMore" size="sm" :loading="loadingMore" @click="load(false)">加载更多</Button>
      </div>
    </section>

    <div v-if="error" class="rounded-xl border border-bad/30 bg-bad/8 p-4 text-[12px] text-bad">{{ error }}</div>

    <ExchangeDetail v-model="drawerOpen" :session-id="id" :row="selected" />

    <Modal v-model:open="configOpen" title="会话实际生效的配置" width="860px">
      <div class="space-y-3">
        <p class="text-[11.5px] leading-relaxed text-muted">
          以下内容来自该会话启动时 fluxzy 写入的 <code>meta.json</code>，可用于核对规则和启动参数。
        </p>
        <CodeBlock :code="configJSON" language="json" max-height="560px" />
      </div>
      <template #footer>
        <Button variant="ghost" @click="configOpen = false">关闭</Button>
        <RouterLink to="/rules"><Button variant="primary">打开规则管理</Button></RouterLink>
      </template>
    </Modal>
  </div>
</template>

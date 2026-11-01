<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  AlertTriangle,
  ArrowDownLeft,
  ArrowUpRight,
  Clock,
  Cpu,
  Network,
  RefreshCw,
  Tag,
  Terminal,
} from 'lucide-vue-next'
import { api } from '@/api/client'
import type { ExchangeDetail, ExchangeRow, HeaderField } from '@/api/types'
import { useToastStore } from '@/stores/toast'
import Drawer from '@/components/ui/Drawer.vue'
import Tabs from '@/components/ui/Tabs.vue'
import Badge from '@/components/ui/Badge.vue'
import Button from '@/components/ui/Button.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import CodeBlock from '@/components/ui/CodeBlock.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import HeadersTable from './HeadersTable.vue'
import BodyViewer from './BodyViewer.vue'
import TimingWaterfall from './TimingWaterfall.vue'
import {
  contentTypeLabel,
  formatBytes,
  formatDateTime,
  formatDuration,
  formatISO,
  methodTone,
  statusTone,
} from '@/utils/format'

const props = defineProps<{ sessionId: string; row: ExchangeRow | null }>()
const open = defineModel<boolean>()
const toast = useToastStore()

const detail = ref<ExchangeDetail | null>(null)
const loading = ref(false)
const error = ref('')
const tab = ref('overview')
const curlLoading = ref(false)

const tabs = computed(() => {
  const t = [
    { key: 'overview', label: '概览' },
    { key: 'request', label: '请求', count: detail.value?.request?.headers?.length ?? 0 },
    { key: 'response', label: '响应', count: detail.value?.response?.headers?.length ?? 0 },
    { key: 'timing', label: '时间线' },
  ]
  if (detail.value?.webSocketMessages?.length) t.push({ key: 'ws', label: 'WebSocket', count: detail.value.webSocketMessages.length })
  t.push({ key: 'raw', label: '原始' })
  return t
})

let loadSeq = 0

async function load() {
  if (!open.value || !props.row) return
  const seq = ++loadSeq
  const sessionId = props.sessionId
  const exchangeId = props.row.id
  loading.value = true
  error.value = ''
  detail.value = null
  tab.value = 'overview'
  try {
    const data = await api.get<ExchangeDetail>(
      `/capture/sessions/${encodeURIComponent(sessionId)}/exchanges/${exchangeId}`,
    )
    if (seq !== loadSeq || sessionId !== props.sessionId || exchangeId !== props.row?.id || !open.value) return
    detail.value = data
  } catch (e: any) {
    if (seq === loadSeq) error.value = e?.message || '读取详情失败'
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

watch([open, () => props.sessionId, () => props.row?.id], () => void load(), { immediate: true })

function headerValue(headers: HeaderField[] | undefined, name: string): string {
  const found = headers?.find((h) => h.name.toLowerCase() === name)
  return found?.value || ''
}

const requestContentType = computed(() => headerValue(detail.value?.request?.headers, 'content-type'))
const responseContentType = computed(() => headerValue(detail.value?.response?.headers, 'content-type'))

const summary = computed(() => {
  const d = detail.value
  if (!d) return []
  const row = d.exchange
  return [
    { label: 'URL', value: d.request.url || row.url, mono: true },
    { label: 'Host', value: row.host || d.request.authority || '—', mono: true },
    { label: 'Path', value: d.request.path || row.path || '—', mono: true },
    { label: 'Method', value: row.method || '—', mono: true },
    { label: 'Status', value: String(row.status || '—'), mono: true },
    { label: 'Protocol', value: row.httpVersion || '—', mono: true },
    { label: 'Content-Type', value: row.contentType || responseContentType.value || '—', mono: true },
    { label: 'Duration', value: formatDuration(row.durationMs), mono: true },
    { label: 'Started', value: formatDateTime(row.startedAt), mono: true },
    { label: 'Egress IP', value: row.egressIp || '—', mono: true },
    { label: 'Request Body', value: d.bodies.requestExists ? formatBytes(d.bodies.requestSize) : '—', mono: true },
    { label: 'Response Body', value: d.bodies.responseExists ? formatBytes(d.bodies.responseSize) : '—', mono: true },
    { label: 'Agent', value: d.agent?.friendlyName || row.agent || '—' },
    { label: 'Tags', value: d.tags.length ? d.tags.join(', ') : '—' },
  ]
})

const statusClass = computed(() => {
  const tone = statusTone(detail.value?.exchange.status || 0)
  return {
    'bg-ok/12 text-ok': tone === 'ok',
    'bg-warn/12 text-warn': tone === 'warn',
    'bg-bad/12 text-bad': tone === 'bad',
    'bg-accent/12 text-accent': tone === 'accent',
    'bg-panel-strong text-faint': tone === 'muted',
  }
})

async function copyCurl() {
  const d = detail.value
  if (!d) return
  curlLoading.value = true
  try {
    const skip = new Set(['host', 'content-length', 'proxy-connection', 'connection', 'accept-encoding'])
    const headers = (d.request.headers || []).filter((h) => !h.name.startsWith(':') && !skip.has(h.name.toLowerCase()))
    const parts = ['curl', '-X', d.request.method || 'GET', quote(d.request.url || d.exchange.url)]
    for (const h of headers) parts.push('-H', quote(`${h.name}: ${h.value}`))
    if (d.bodies.requestExists) {
      try {
        const res = await fetch(
          `/api/v1/capture/sessions/${encodeURIComponent(props.sessionId)}/exchanges/${d.exchange.id}/body?kind=request&format=text`,
          { credentials: 'include' },
        )
        if (res.ok) {
          const text = await res.text()
          if (text.length <= 200_000) parts.push('--data-raw', quote(text))
          else parts.push('# 请求体过大，请使用 --data-binary @file')
        } else {
          parts.push('# 请求体为二进制，请使用 --data-binary @file')
        }
      } catch {
        /* ignore */
      }
    }
    const cmd = parts.join(' \\\n  ')
    await navigator.clipboard.writeText(cmd)
    toast.success('已复制 cURL')
  } catch {
    toast.error('复制失败', '浏览器未授予剪贴板权限')
  } finally {
    curlLoading.value = false
  }
}

function quote(s: string) {
  return `'${s.replace(/'/g, `'\\''`)}'`
}

const curlHint = computed(() => {
  const d = detail.value
  if (!d) return ''
  return `curl -X ${d.request.method || 'GET'} ${quote(d.request.url || d.exchange.url)}`
})
</script>

<template>
  <Drawer v-model="open" width="min(96vw, 1000px)">
    <template #header>
      <div class="flex min-w-0 flex-wrap items-center gap-2">
        <span
          v-if="row"
          class="inline-flex items-center rounded-md px-2 py-1 font-mono text-[11px] font-semibold"
          :class="methodTone(row.method)"
        >{{ row.method }}</span>
        <span
          v-if="row"
          class="inline-flex items-center rounded-md px-2 py-1 font-mono text-[11px] font-semibold"
          :class="statusClass"
        >{{ row.pending && !row.status ? '···' : row.status || '—' }}</span>
        <span class="min-w-0 max-w-[520px] truncate font-mono text-[11.5px] text-muted" :title="row?.url">
          {{ row?.url || row?.path || '—' }}
        </span>
      </div>
    </template>
    <template #actions>
      <CopyButton v-if="row" :text="row.url" small label="复制 URL" />
      <Button variant="ghost" size="sm" :loading="loading" @click="load">
        <template #icon><RefreshCw class="h-3.5 w-3.5" /></template>
      </Button>
    </template>

    <div class="p-5">
      <div v-if="loading" class="space-y-3">
        <div class="h-7 w-1/2 skeleton" />
        <div class="h-24 w-full skeleton" />
        <div class="h-64 w-full skeleton" />
      </div>

      <div v-else-if="error" class="rounded-xl border border-bad/30 bg-bad/8 p-4">
        <div class="flex items-center gap-2 text-[12.5px] font-medium text-bad">
          <AlertTriangle class="h-4 w-4" /> {{ error }}
        </div>
        <Button size="sm" class="mt-3" @click="load">重试</Button>
      </div>

      <template v-else-if="detail">
        <div
          v-if="detail.expired"
          class="mb-4 flex items-center gap-2 rounded-xl border border-warn/25 bg-warn/8 px-3.5 py-2.5 text-[12px] text-warn"
        >
          <AlertTriangle class="h-4 w-4" /> 原始数据已过期，当前展示的是采样索引，header/body 不可用。
        </div>

        <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
          <Tabs v-model="tab" :tabs="tabs" />
          <div class="flex items-center gap-1">
            <Button size="sm" :loading="curlLoading" @click="copyCurl">
              <template #icon><Terminal class="h-3.5 w-3.5" /></template>
              Copy as cURL
            </Button>
          </div>
        </div>

        <!-- overview -->
        <div v-if="tab === 'overview'" class="space-y-4">
          <div v-if="detail.clientErrors.length" class="rounded-xl border border-bad/30 bg-bad/8 p-4">
            <div class="flex items-center gap-2 text-[12px] font-semibold text-bad">
              <AlertTriangle class="h-4 w-4" /> 传输错误 ({{ detail.clientErrors.length }})
            </div>
            <div v-for="(e, i) in detail.clientErrors" :key="i" class="mt-2 font-mono text-[11.5px] text-bad/90">
              [{{ e.errorCode }}] {{ e.message }}
              <span v-if="e.networkErrorCode" class="text-bad/60"> · {{ e.networkErrorCode }}</span>
              <div v-if="e.exceptionMessage" class="mt-0.5 text-[10.5px] text-muted">{{ e.exceptionMessage }}</div>
            </div>
          </div>

          <div class="grid gap-2 sm:grid-cols-2">
            <div
              v-for="item in summary"
              :key="item.label"
              class="rounded-xl border border-line/50 bg-panel-strong/35 px-3.5 py-2.5"
            >
              <div class="text-[10.5px] uppercase tracking-wider text-faint">{{ item.label }}</div>
              <div
                class="mt-1 break-all text-[12px] text-ink"
                :class="item.mono ? 'font-mono' : ''"
              >{{ item.value }}</div>
            </div>
          </div>

          <div v-if="detail.tags.length" class="flex flex-wrap items-center gap-1.5">
            <Tag class="h-3.5 w-3.5 text-faint" />
            <span v-for="t in detail.tags" :key="t" class="badge-accent">{{ t }}</span>
          </div>

          <div v-if="detail.connection" class="rounded-xl border border-line/50 bg-panel-strong/35 p-4">
            <div class="flex items-center gap-2 text-[12px] font-semibold text-ink">
              <Network class="h-4 w-4 text-accent" /> 连接信息
            </div>
            <div class="mt-3 grid gap-2 text-[11.5px] sm:grid-cols-2">
              <div class="flex justify-between gap-3"><span class="text-faint">连接 ID</span><span class="font-mono text-muted">{{ detail.connection.id }}</span></div>
              <div class="flex justify-between gap-3"><span class="text-faint">协议</span><span class="font-mono text-muted">{{ detail.connection.httpVersion || '—' }}</span></div>
              <div class="flex justify-between gap-3"><span class="text-faint">上游</span><span class="font-mono text-muted">{{ detail.connection.hostName }}:{{ detail.connection.port }}</span></div>
              <div class="flex justify-between gap-3"><span class="text-faint">TLS</span><span class="font-mono text-muted">{{ detail.connection.secure ? '是' : '否' }}</span></div>
              <div class="flex justify-between gap-3"><span class="text-faint">本地</span><span class="font-mono text-muted">{{ detail.connection.localAddress }}:{{ detail.connection.localPort }}</span></div>
              <div class="flex justify-between gap-3"><span class="text-faint">上游地址</span><span class="font-mono text-muted">{{ detail.connection.remoteAddress || '—' }}</span></div>
            </div>
          </div>

          <div v-if="detail.processInfo" class="rounded-xl border border-line/50 bg-panel-strong/35 p-4">
            <div class="flex items-center gap-2 text-[12px] font-semibold text-ink">
              <Cpu class="h-4 w-4 text-accent-2" /> 发起进程
            </div>
            <div class="mt-2 font-mono text-[11.5px] text-muted">
              PID {{ detail.processInfo.processId }} · {{ detail.processInfo.processPath || '—' }}
            </div>
            <div v-if="detail.processInfo.processArguments" class="mt-1 break-all font-mono text-[10.5px] text-faint">
              {{ detail.processInfo.processArguments }}
            </div>
          </div>
        </div>

        <!-- request -->
        <div v-else-if="tab === 'request'" class="space-y-4">
          <div class="flex flex-wrap items-center gap-2">
            <span class="badge-accent">REQUEST</span>
            <span class="font-mono text-[11.5px] text-muted">{{ detail.request.method }} {{ detail.request.url }}</span>
          </div>
          <HeadersTable :headers="detail.request.headers || []" title="Request Headers" />
          <div>
            <div class="mb-2 text-[11.5px] font-medium text-muted">Request Body</div>
            <BodyViewer
              :session-id="sessionId"
              :exchange-id="detail.exchange.id"
              kind="request"
              :content-type="requestContentType"
              :exists="detail.bodies.requestExists"
              :size="detail.bodies.requestSize"
            />
          </div>
        </div>

        <!-- response -->
        <div v-else-if="tab === 'response'" class="space-y-4">
          <div class="flex flex-wrap items-center gap-2">
            <span class="badge-accent">RESPONSE</span>
            <span class="font-mono text-[11.5px] text-muted">{{ detail.response?.statusCode || '—' }}</span>
            <span class="badge-muted">{{ contentTypeLabel(responseContentType) }}</span>
          </div>
          <HeadersTable :headers="detail.response?.headers || []" title="Response Headers" />
          <div>
            <div class="mb-2 text-[11.5px] font-medium text-muted">Response Body</div>
            <BodyViewer
              :session-id="sessionId"
              :exchange-id="detail.exchange.id"
              kind="response"
              :content-type="responseContentType"
              :exists="detail.bodies.responseExists"
              :size="detail.bodies.responseSize"
            />
          </div>
        </div>

        <!-- timing -->
        <div v-else-if="tab === 'timing'">
          <TimingWaterfall :metrics="detail.metrics" />
        </div>

        <!-- websocket -->
        <div v-else-if="tab === 'ws'" class="space-y-2">
          <div
            v-for="m in detail.webSocketMessages"
            :key="m.id"
            class="rounded-xl border border-line/50 bg-panel-strong/30 p-3"
          >
            <div class="flex items-center gap-2 text-[11px]">
              <component :is="m.direction === 0 ? ArrowUpRight : ArrowDownLeft" class="h-3.5 w-3.5" :class="m.direction === 0 ? 'text-accent' : 'text-accent-2'" />
              <span class="font-mono text-muted">#{{ m.id }}</span>
              <span class="badge-muted">opcode {{ m.opCode }}</span>
              <span class="text-faint">{{ formatBytes(m.length) }}</span>
              <span class="ml-auto text-faint">{{ m.frameCount }} frame(s)</span>
            </div>
            <pre v-if="m.data" class="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-bg-soft/70 p-2 font-mono text-[11px] text-ink">{{ m.data }}</pre>
          </div>
        </div>

        <!-- raw -->
        <div v-else-if="tab === 'raw'">
          <CodeBlock :code="JSON.stringify(detail, null, 2)" language="json" max-height="620px" />
        </div>
      </template>

      <div v-else class="empty-state">
        <EmptyState title="选择一条请求" description="从左侧列表选择请求后，这里会显示详细信息。" />
      </div>
    </div>

    <template #footer>
      <div class="flex flex-wrap items-center gap-3 text-[10.5px] text-faint">
        <span v-if="detail">Exchange #{{ detail.exchange.id }}</span>
        <span v-if="detail">Connection #{{ detail.exchange.connectionId }}</span>
        <span v-if="detail">Started {{ formatISO(detail.metrics.startedAt) }}</span>
        <span class="ml-auto flex items-center gap-1.5">
          <Clock class="h-3 w-3" /> {{ formatDuration(detail?.metrics.durationMs) }}
        </span>
      </div>
    </template>
  </Drawer>
</template>

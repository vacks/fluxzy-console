<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import {
  Archive,
  ChevronLeft,
  ChevronRight,
  Download,
  ExternalLink,
  History,
  Pin,
  PinOff,
  RefreshCw,
  Search,
  Trash2,
} from 'lucide-vue-next'
import { api, fetchDownload } from '@/api/client'
import type { CaptureSession } from '@/api/types'
import { useToastStore } from '@/stores/toast'
import Button from '@/components/ui/Button.vue'
import Badge from '@/components/ui/Badge.vue'
import Modal from '@/components/ui/Modal.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import StatusDot from '@/components/ui/StatusDot.vue'
import { formatBytes, formatDateTime, formatNumber, formatRelative } from '@/utils/format'

const toast = useToastStore()

const search = ref('')
const statusFilter = ref('')
const rows = ref<CaptureSession[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const totalPages = ref(0)
const pageLoading = ref(false)
let searchTimer: number | undefined
let pageRequest = 0
const exportTarget = ref<CaptureSession | null>(null)
const exportOpen = ref(false)
const exportFormat = ref('har')
const deleteTarget = ref<CaptureSession | null>(null)
const deleteOpen = ref(false)
const busy = ref(false)
const exporting = ref(false)

const pageNumbers = computed(() => {
  const last = totalPages.value
  if (last <= 1) return [] as number[]
  const start = Math.max(1, Math.min(page.value - 2, last - 4))
  const end = Math.min(last, start + 4)
  return Array.from({ length: end - start + 1 }, (_, i) => start + i)
})

async function load() {
  const request = ++pageRequest
  pageLoading.value = true
  try {
    const params = new URLSearchParams({
      page: String(page.value),
      pageSize: String(pageSize.value),
    })
    const q = search.value.trim()
    if (q) params.set('search', q)
    if (statusFilter.value) params.set('status', statusFilter.value)
    const r = await api.get<{
      items: CaptureSession[]
      total: number
      page: number
      pageSize: number
      totalPages: number
    }>(`/capture/sessions?${params.toString()}`)
    if (request !== pageRequest) return
    rows.value = r.items || []
    total.value = r.total || 0
    totalPages.value = r.totalPages || 0
    if (r.pageSize > 0) pageSize.value = r.pageSize
    if (rows.value.length === 0 && total.value > 0 && page.value > Math.max(1, totalPages.value)) {
      page.value = Math.max(1, totalPages.value)
      await load()
    }
  } catch (e: any) {
    if (request === pageRequest) toast.error('读取会话列表失败', e?.message)
  } finally {
    if (request === pageRequest) pageLoading.value = false
  }
}

function refresh() {
  return load()
}

function goPage(target: number) {
  const max = Math.max(1, totalPages.value)
  const next = Math.min(Math.max(1, target), max)
  if (next === page.value) return
  page.value = next
  void load()
}

watch(search, () => {
  window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => {
    page.value = 1
    void load()
  }, 300)
})

watch([statusFilter, pageSize], () => {
  page.value = 1
  void load()
})

onMounted(load)

onBeforeUnmount(() => window.clearTimeout(searchTimer))

function statusOf(s: CaptureSession): { label: string; variant: 'ok' | 'warn' | 'accent' | 'muted' } {
  if (s.active) return s.online ? { label: 'live', variant: 'ok' } : { label: 'offline', variant: 'warn' }
  if (s.status === 'indexing') return { label: 'indexing', variant: 'accent' }
  if (s.hasCold) return { label: 'sampled', variant: 'warn' }
  if (s.status === 'expired' || !s.hasRaw) return { label: 'expired', variant: 'warn' }
  return { label: 'archived', variant: 'muted' }
}

function statusTone(s: CaptureSession): 'ok' | 'warn' | 'accent' | 'muted' {
  return statusOf(s).variant
}

function askExport(s: CaptureSession) {
  if (!s.hasRaw) {
    toast.warning('无法导出', s.hasCold ? '该会话仅保留采样索引，原始文件已被清理' : '该会话的原始文件与索引均已清理')
    return
  }
  exportTarget.value = s
  exportFormat.value = 'har'
  exportOpen.value = true
}

function askDelete(s: CaptureSession) {
  if (s.active) {
    toast.warning('不能删除当前会话', 'fluxzy 正在写入该会话，请等待重启或轮转生成新会话后再删除')
    return
  }
  deleteTarget.value = s
  deleteOpen.value = true
}

async function togglePin(s: CaptureSession) {
  try {
    await api.post(`/capture/sessions/${encodeURIComponent(s.id)}/pin`, { pinned: !s.pinned })
    toast.success(s.pinned ? '已取消固定' : '已固定，不会被自动清理')
    await refresh()
  } catch (e: any) {
    toast.error('操作失败', e?.message)
  }
}

async function confirmDelete() {
  if (!deleteTarget.value) return
  busy.value = true
  try {
    await api.del(`/capture/sessions/${encodeURIComponent(deleteTarget.value.id)}`)
    toast.success('会话已删除')
    deleteTarget.value = null
    deleteOpen.value = false
    if (rows.value.length === 1 && page.value > 1) page.value--
    await refresh()
  } catch (e: any) {
    toast.error('删除失败', e?.message)
  } finally {
    busy.value = false
  }
}

async function startExport() {
  if (!exportTarget.value) return
  exporting.value = true
  try {
    const id = exportTarget.value.id
    const format = exportFormat.value
    await fetchDownload(
      `/capture/sessions/${encodeURIComponent(id)}/export?format=${format}`,
      `${id}.${format}`,
    )
    toast.success('导出完成', '浏览器已开始下载文件')
    exportTarget.value = null
    exportOpen.value = false
  } catch (e: any) {
    toast.error('导出失败', e?.message || '未知错误')
  } finally {
    exporting.value = false
  }
}
</script>

<template>
  <div class="flex h-[calc(100dvh-6.375rem)] min-h-0 flex-col gap-4 lg:h-[calc(100dvh-6.875rem)]">
    <section class="panel flex shrink-0 flex-col gap-4 p-4 lg:flex-row lg:items-center lg:justify-between">
      <div class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-accent/10">
          <History class="h-5 w-5 text-accent" />
        </div>
        <div>
          <h2 class="text-[15px] font-semibold tracking-tight text-ink">会话历史</h2>
          <p class="mt-0.5 text-[11.5px] text-muted">每次 fluxzy 重启或轮转生成一个独立会话；过期后按冷数据配置保留采样索引。</p>
        </div>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <div class="relative">
          <Search class="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-faint" />
          <input v-model="search" class="input h-9 w-[220px] py-2 pl-9 text-[12px]" placeholder="搜索会话 ID" />
        </div>
        <select v-model="statusFilter" class="select h-9 w-[130px] py-2 text-[12px]">
          <option value="">全部状态</option>
          <option value="live">live</option>
          <option value="archived">archived</option>
          <option value="sampled">已过期</option>
        </select>
        <Button size="sm" :loading="pageLoading" @click="refresh">
          <template #icon><RefreshCw class="h-3.5 w-3.5" /></template>
          刷新
        </Button>
      </div>
    </section>

    <section class="panel flex min-h-0 flex-1 flex-col overflow-hidden">
      <div class="min-h-0 flex-1 overflow-auto">
        <table class="table">
          <thead>
            <tr>
              <th>会话</th>
              <th>采集时间</th>
              <th class="whitespace-nowrap text-right">请求数</th>
              <th class="hidden whitespace-nowrap text-right md:table-cell">大小</th>
              <th>状态</th>
              <th class="hidden md:table-cell">固定</th>
              <th class="whitespace-nowrap text-right">操作</th>
            </tr>
          </thead>
          <tbody v-if="rows.length">
            <tr v-for="s in rows" :key="s.id">
              <td>
                <RouterLink :to="{ name: 'session', params: { id: s.id } }" class="group inline-flex items-center gap-2">
                  <span class="font-mono text-[11.5px] text-ink group-hover:text-accent">{{ s.id }}</span>
                  <ExternalLink class="h-3 w-3 text-faint opacity-0 transition group-hover:opacity-100" />
                </RouterLink>
                <div class="mt-0.5 flex items-center gap-1.5 text-[10px] text-faint">
                  <span v-if="s.hasRaw">raw</span>
                  <span v-if="s.hasCold">· sampled</span>
                </div>
              </td>
              <td>
                <div class="text-[11.5px] text-muted">{{ formatDateTime(s.captureDate) }}</div>
                <div class="text-[10px] text-faint">{{ formatRelative(s.captureDate) }}</div>
              </td>
              <td class="whitespace-nowrap text-right font-mono text-[11.5px] text-muted">{{ formatNumber(s.exchangeCount) }}</td>
              <td class="hidden whitespace-nowrap text-right font-mono text-[11.5px] text-muted md:table-cell">{{ formatBytes(s.totalBytes) }}</td>
              <td>
                <Badge variant="muted">
                  <StatusDot :tone="statusTone(s)" :pulse="s.active && s.online" />
                  {{ statusOf(s).label }}
                </Badge>
              </td>
              <td class="hidden md:table-cell">
                <button
                  class="btn-ghost h-7 w-7 px-0"
                  :title="s.pinned ? '取消固定' : '固定保留'"
                  @click="togglePin(s)"
                >
                  <component :is="s.pinned ? PinOff : Pin" class="h-3.5 w-3.5" :class="s.pinned ? 'text-accent' : ''" />
                </button>
              </td>
              <td>
                <div class="flex items-center justify-end gap-1">
                  <RouterLink :to="{ name: 'session', params: { id: s.id } }">
                    <Button size="sm" variant="ghost">查看</Button>
                  </RouterLink>
                  <Button
                    size="sm"
                    variant="ghost"
                    class="inline-flex"
                    :title="s.hasRaw ? '导出会话' : '仅保留采样索引，无法导出'"
                    @click="askExport(s)"
                  >
                    <template #icon><Download class="h-3.5 w-3.5" /></template>
                    <span class="hidden md:inline">导出</span>
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    class="inline-flex text-bad hover:text-bad"
                    :title="s.active ? '当前会话正在写入，不能删除' : '删除会话'"
                    @click="askDelete(s)"
                  >
                    <template #icon><Trash2 class="h-3.5 w-3.5" /></template>
                  </Button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <EmptyState
          v-if="!rows.length && !pageLoading"
          :title="search || statusFilter ? '没有匹配的会话' : '还没有会话'"
          :description="search || statusFilter ? '尝试调整搜索关键字或状态筛选。' : '确认 fluxzy 已启动，并且 /data 目录挂载到了控制台。'"
        >
          <template #icon><Archive class="h-5 w-5" /></template>
        </EmptyState>
      </div>

      <footer class="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t border-line/50 bg-panel/60 px-4 py-3">
        <div class="text-[11.5px] text-muted">
          共 <b class="font-mono text-ink">{{ formatNumber(total) }}</b> 个会话
          <span v-if="totalPages > 1" class="text-faint">· 第 {{ page }} / {{ totalPages }} 页</span>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <select v-model.number="pageSize" class="select h-8 w-[92px] py-1 text-[11.5px]" aria-label="每页数量">
            <option :value="20">20 / 页</option>
            <option :value="50">50 / 页</option>
            <option :value="100">100 / 页</option>
          </select>
          <Button size="sm" variant="ghost" :disabled="page <= 1 || pageLoading" @click="goPage(page - 1)">
            <template #icon><ChevronLeft class="h-3.5 w-3.5" /></template>
            上一页
          </Button>
          <button
            v-for="p in pageNumbers"
            :key="p"
            class="h-8 min-w-8 rounded-lg px-2 font-mono text-[11.5px] transition"
            :class="p === page ? 'bg-accent/12 text-accent' : 'text-muted hover:bg-panel-strong/70 hover:text-ink'"
            :aria-current="p === page ? 'page' : undefined"
            @click="goPage(p)"
          >
            {{ p }}
          </button>
          <Button size="sm" variant="ghost" :disabled="page >= totalPages || pageLoading" @click="goPage(page + 1)">
            下一页
            <template #icon><ChevronRight class="h-3.5 w-3.5" /></template>
          </Button>
        </div>
      </footer>
    </section>

    <Modal v-model:open="exportOpen" title="导出会话" width="520px">
      <div v-if="exportTarget" class="space-y-4">
        <div class="rounded-xl border border-line/60 bg-panel-strong/40 p-3">
          <div class="font-mono text-[12px] text-ink">{{ exportTarget.id }}</div>
          <div class="mt-1 text-[11px] text-faint">{{ formatNumber(exportTarget.exchangeCount) }} 条请求 · {{ formatBytes(exportTarget.totalBytes) }}</div>
        </div>
        <div class="grid grid-cols-3 gap-2">
          <button
            v-for="f in [
              { key: 'har', label: 'HAR', desc: '浏览器/Charles 通用' },
              { key: 'fxzy', label: 'FXZY', desc: 'fluxzy 原生归档' },
              { key: 'saz', label: 'SAZ', desc: 'Fiddler 归档' },
            ]"
            :key="f.key"
            class="rounded-xl border p-3 text-left transition"
            :class="exportFormat === f.key ? 'border-accent/50 bg-accent/8' : 'border-line/60 bg-panel-strong/40 hover:border-accent/30'"
            @click="exportFormat = f.key"
          >
            <div class="text-[12px] font-semibold text-ink">{{ f.label }}</div>
            <div class="mt-1 text-[10px] leading-relaxed text-faint">{{ f.desc }}</div>
          </button>
        </div>
        <p class="text-[11px] leading-relaxed text-faint">导出由内置的 fluxzy CLI 完成，大文件可能需要几十秒。</p>
      </div>
      <template #footer>
        <Button variant="ghost" :disabled="exporting" @click="exportOpen = false">取消</Button>
        <Button variant="primary" :loading="exporting" @click="startExport">
          <template #icon><Download class="h-3.5 w-3.5" /></template>
          {{ exporting ? '正在导出…' : '开始导出' }}
        </Button>
      </template>
    </Modal>

    <Modal v-model:open="deleteOpen" title="删除会话" width="460px">
      <div v-if="deleteTarget" class="space-y-3">
        <div class="rounded-xl border border-bad/25 bg-bad/8 p-3 text-[12px] text-bad">
          将永久删除会话 <b class="font-mono">{{ deleteTarget.id }}</b> 的原始文件、索引与记录，且不可恢复。
        </div>
        <p class="text-[11.5px] text-muted">如果只是想释放空间，可以等待热数据保留策略自动清理；固定（pin）的会话不会被自动清理。</p>
      </div>
      <template #footer>
        <Button variant="ghost" @click="deleteOpen = false">取消</Button>
        <Button variant="danger" :loading="busy" @click="confirmDelete">
          <template #icon><Trash2 class="h-3.5 w-3.5" /></template>
          确认删除
        </Button>
      </template>
    </Modal>
  </div>
</template>

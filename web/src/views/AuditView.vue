<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ChevronLeft, ChevronRight, RefreshCw, ScrollText, Search } from 'lucide-vue-next'
import { api } from '@/api/client'
import type { AuditEntry } from '@/api/types'
import Button from '@/components/ui/Button.vue'
import Badge from '@/components/ui/Badge.vue'
import Modal from '@/components/ui/Modal.vue'
import CodeBlock from '@/components/ui/CodeBlock.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import { useToastStore } from '@/stores/toast'
import { formatDateTime, formatNumber, formatRelative } from '@/utils/format'

const toast = useToastStore()

const rows = ref<AuditEntry[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const totalPages = ref(0)
const actions = ref<string[]>([])
const loading = ref(false)
const search = ref('')
const actionFilter = ref('')
const selected = ref<AuditEntry | null>(null)
const detailOpen = ref(false)
let searchTimer: number | undefined
let pageRequest = 0

const actionLabels: Record<string, string> = {
  'auth.login': '登录成功',
  'auth.login_failed': '登录失败',
  'auth.logout': '退出登录',
  'auth.setup': '初始化管理员',
  'auth.password_change': '修改密码',
  'rules.save': '保存规则',
  'rules.apply': '应用规则',
  'rules.apply_failed': '应用规则失败',
  'rules.rollback': '回滚规则',
  'session.pin': '固定会话',
  'session.delete': '删除会话',
  'session.export': '导出会话',
  'user.create': '创建用户',
  'user.update': '更新用户',
  'user.delete': '删除用户',
  'system.ca_export': '导出 CA',
  'system.ca_upload': '上传 CA',
  'system.ca_regenerate': '重新生成 CA',
  'system.ca_reset': '恢复内置 CA',
  'settings.retention': '修改保留策略',
  'fluxzy.start': '启动 fluxzy',
  'fluxzy.stop': '停止 fluxzy',
  'fluxzy.restart': '重启 fluxzy',
  'fluxzy.rotate': '轮转会话',
  'fluxzy.config.update': '保存启动参数',
  'fluxzy.config.apply': '应用启动参数',
  'fluxzy.config.apply_failed': '应用启动参数失败',
  'fluxzy.rotation.update': '修改轮转策略',
}

const pageNumbers = computed(() => {
  const last = totalPages.value
  if (last <= 1) return [] as number[]
  const start = Math.max(1, Math.min(page.value - 2, last - 4))
  const end = Math.min(last, start + 4)
  return Array.from({ length: end - start + 1 }, (_, i) => start + i)
})

function label(action: string) {
  return actionLabels[action] || action
}

function tone(action: string): 'ok' | 'warn' | 'bad' | 'muted' | 'accent' {
  if (action.includes('failed') || action.includes('delete')) return 'bad'
  if (action.includes('login') || action.includes('setup') || action.includes('password')) return 'warn'
  if (action.startsWith('rules')) return 'accent'
  if (action.startsWith('session')) return 'ok'
  return 'muted'
}

async function load() {
  const request = ++pageRequest
  loading.value = true
  try {
    const params = new URLSearchParams({
      page: String(page.value),
      pageSize: String(pageSize.value),
    })
    const q = search.value.trim()
    if (q) params.set('search', q)
    if (actionFilter.value) params.set('action', actionFilter.value)
    const r = await api.get<{
      items: AuditEntry[]
      total: number
      page: number
      pageSize: number
      totalPages: number
      actions: string[]
    }>(`/audit?${params.toString()}`)
    if (request !== pageRequest) return
    rows.value = r.items || []
    total.value = r.total || 0
    totalPages.value = r.totalPages || 0
    actions.value = r.actions || []
    if (r.pageSize > 0) pageSize.value = r.pageSize
    if (rows.value.length === 0 && total.value > 0 && page.value > Math.max(1, totalPages.value)) {
      page.value = Math.max(1, totalPages.value)
      await load()
    }
  } catch (e: any) {
    if (request === pageRequest) toast.error('读取日志失败', e?.message)
  } finally {
    if (request === pageRequest) loading.value = false
  }
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

watch([actionFilter, pageSize], () => {
  page.value = 1
  void load()
})

onMounted(load)

onBeforeUnmount(() => window.clearTimeout(searchTimer))

function openDetail(item: AuditEntry) {
  selected.value = item
  detailOpen.value = true
}
</script>

<template>
  <div class="flex h-[calc(100dvh-6.375rem)] min-h-0 flex-col gap-4 lg:h-[calc(100dvh-6.875rem)]">
    <section class="panel flex shrink-0 flex-col gap-4 p-4 lg:flex-row lg:items-center lg:justify-between">
      <div class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-accent/10">
          <ScrollText class="h-5 w-5 text-accent" />
        </div>
        <div>
          <h2 class="text-[15px] font-semibold tracking-tight text-ink">日志</h2>
          <p class="mt-0.5 text-[11.5px] text-muted">登录、规则变更、会话操作与用户管理都会留痕。</p>
        </div>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <div class="relative">
          <Search class="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-faint" />
          <input v-model="search" class="input h-9 w-[220px] py-2 pl-9 text-[12px]" placeholder="搜索用户 / 操作 / 目标 / IP" />
        </div>
        <select v-model="actionFilter" class="select h-9 w-[160px] py-2 text-[12px]">
          <option value="">全部操作</option>
          <option v-for="a in actions" :key="a" :value="a">{{ label(a) }}</option>
        </select>
        <Button size="sm" :loading="loading" @click="load">
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
              <th>时间</th>
              <th>用户</th>
              <th>操作</th>
              <th>目标</th>
              <th>IP</th>
              <th>详情</th>
            </tr>
          </thead>
          <tbody v-if="rows.length">
            <tr v-for="item in rows" :key="item.id" class="cursor-pointer" @click="openDetail(item)">
              <td>
                <div class="text-[11.5px] text-muted">{{ formatDateTime(item.createdAt) }}</div>
                <div class="text-[10px] text-faint">{{ formatRelative(item.createdAt) }}</div>
              </td>
              <td class="font-mono text-[11.5px] text-ink">{{ item.username || 'anonymous' }}</td>
              <td><Badge :variant="tone(item.action)">{{ label(item.action) }}</Badge></td>
              <td class="max-w-[240px] truncate font-mono text-[11px] text-muted">{{ item.target || '—' }}</td>
              <td class="font-mono text-[11px] text-faint">{{ item.ip || '—' }}</td>
              <td class="text-[11px] text-faint">{{ item.detail ? '查看' : '—' }}</td>
            </tr>
          </tbody>
        </table>
        <EmptyState
          v-if="!rows.length && !loading"
          :title="search || actionFilter ? '没有匹配的日志' : '没有日志记录'"
          :description="search || actionFilter ? '尝试调整搜索关键字或操作筛选。' : '执行规则保存、登录等操作后，这里会出现记录。'"
        />
      </div>

      <footer class="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t border-line/50 bg-panel/60 px-4 py-3">
        <div class="text-[11.5px] text-muted">
          共 <b class="font-mono text-ink">{{ formatNumber(total) }}</b> 条日志
          <span v-if="totalPages > 1" class="text-faint">· 第 {{ page }} / {{ totalPages }} 页</span>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <select v-model.number="pageSize" class="select h-8 w-[92px] py-1 text-[11.5px]" aria-label="每页数量">
            <option :value="20">20 / 页</option>
            <option :value="50">50 / 页</option>
            <option :value="100">100 / 页</option>
          </select>
          <Button size="sm" variant="ghost" :disabled="page <= 1 || loading" @click="goPage(page - 1)">
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
          <Button size="sm" variant="ghost" :disabled="page >= totalPages || loading" @click="goPage(page + 1)">
            下一页
            <template #icon><ChevronRight class="h-3.5 w-3.5" /></template>
          </Button>
        </div>
      </footer>
    </section>

    <Modal v-model:open="detailOpen" title="日志详情" width="620px">
      <div v-if="selected" class="space-y-3">
        <div class="grid gap-2 sm:grid-cols-2">
          <div class="rounded-xl border border-line/50 bg-panel-strong/35 p-3">
            <div class="text-[10px] uppercase tracking-wider text-faint">Action</div>
            <div class="mt-1 text-[12px] text-ink">{{ label(selected.action) }} <span class="font-mono text-[10px] text-faint">({{ selected.action }})</span></div>
          </div>
          <div class="rounded-xl border border-line/50 bg-panel-strong/35 p-3">
            <div class="text-[10px] uppercase tracking-wider text-faint">User</div>
            <div class="mt-1 text-[12px] text-ink">{{ selected.username || 'anonymous' }} <span class="font-mono text-[10px] text-faint">{{ selected.ip }}</span></div>
          </div>
        </div>
        <div class="text-[11px] text-muted">{{ formatDateTime(selected.createdAt) }} · {{ selected.userAgent || 'unknown UA' }}</div>
        <div>
          <div class="mb-1.5 text-[11.5px] font-medium text-muted">Detail</div>
          <CodeBlock :code="selected.detail || '—'" language="json" max-height="300px" />
        </div>
      </div>
      <template #footer><Button variant="ghost" @click="detailOpen = false">关闭</Button></template>
    </Modal>
  </div>
</template>

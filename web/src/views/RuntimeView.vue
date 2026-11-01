<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useRuntimeStore } from '@/stores/runtime'
import { useAuthStore } from '@/stores/auth'
import { useToastStore } from '@/stores/toast'
import { formatBytes, formatDateTime, formatDuration, formatRelative } from '@/utils/format'
import { ChevronDown } from 'lucide-vue-next'
import type { RotationSettings, StartupConfig } from '@/api/types'
import Card from '@/components/ui/Card.vue'
import Button from '@/components/ui/Button.vue'
import Badge from '@/components/ui/Badge.vue'
import StatusDot from '@/components/ui/StatusDot.vue'
import Input from '@/components/ui/Input.vue'
import Select from '@/components/ui/Select.vue'
import Switch from '@/components/ui/Switch.vue'
import Modal from '@/components/ui/Modal.vue'

const runtime = useRuntimeStore()
const auth = useAuthStore()
const toast = useToastStore()

const startupForm = reactive<StartupConfig>({
  listenAddr: '0.0.0.0:443',
  mode: 'ReverseSecure',
  reversePort: 443,
  maxUpstreamConnections: 16,
  requestBuffer: 4096,
  maxCaptureCount: 0,
  serveH2: false,
  parseUA: false,
  noCertCache: false,
  skipSSLDecryption: false,
})
const BYTES_PER_GB = 1_000_000_000
const rotationForm = reactive<RotationSettings>({
  enabled: true,
  intervalHours: 0,
  maxExchanges: 0,
  maxSessionBytes: 0,
  minFreeBytes: 2 * BYTES_PER_GB,
  emergencyFreeBytes: 0.5 * BYTES_PER_GB,
  checkSeconds: 30,
})
const minFreeGB = ref(2)
const emergencyFreeGB = ref(0.5)

const loaded = ref(false)
const savingStartup = ref(false)
const savingRotation = ref(false)
const showLogs = ref(true)
const logsLoading = ref(false)
const logsError = ref('')
const confirm = ref<{
  action: () => Promise<void>
  title: string
  message: string
  confirmText: string
  danger?: boolean
} | null>(null)

const sup = computed(() => runtime.supervisor)
const state = computed(() => sup.value?.state || 'unknown')
const stateTone = computed(() => {
  switch (state.value) {
    case 'running': return 'ok'
    case 'starting':
    case 'backoff': return 'warn'
    case 'failed':
    case 'disk_full': return 'bad'
    default: return 'muted'
  }
})
const stateLabel = computed(() => {
  const map: Record<string, string> = {
    running: '运行中', starting: '启动中', stopping: '停止中', stopped: '已停止',
    backoff: '等待重启', failed: '失败', disk_full: '磁盘不足',
  }
  return map[state.value] || state.value
})
const pendingStartup = computed(() => !!runtime.startup?.pending)
const advancedEnabled = computed(
  () =>
    startupForm.serveH2 ||
    startupForm.parseUA ||
    startupForm.noCertCache ||
    startupForm.skipSSLDecryption,
)
const currentVersion = computed(() => sup.value?.binaryVersion || '-')

function syncStartup() {
  const s = runtime.startup?.settings
  if (s) Object.assign(startupForm, s)
}

function bytesToGB(bytes: number): number {
  return Math.round((bytes / BYTES_PER_GB) * 100) / 100
}

function syncRotation() {
  const r = runtime.rotation?.settings
  if (!r) return
  Object.assign(rotationForm, r)
  minFreeGB.value = bytesToGB(r.minFreeBytes)
  emergencyFreeGB.value = bytesToGB(r.emergencyFreeBytes)
}

async function load() {
  try {
    await runtime.fetchAll()
    syncStartup()
    syncRotation()
    loaded.value = true
  } catch (e: any) {
    loaded.value = false
    toast.error('读取运行时失败', e?.message)
  }
  await loadLogs()
}

onMounted(() => {
  runtime.startPolling()
  void load()
})
onUnmounted(() => runtime.stopPolling())

async function runAction(name: 'start' | 'stop' | 'restart' | 'rotate') {
  try {
    await runtime.action(name)
    toast.success('操作完成', `fluxzy ${name} 已完成`)
    if (name === 'rotate' || name === 'restart') {
      await runtime.fetchRotation()
      syncRotation()
    }
  } catch (e: any) {
    toast.error('操作失败', e?.message)
  }
}

function askRestart() {
  confirm.value = {
    title: '重启 fluxzy',
    message: '重启会中断当前连接并创建新会话。确定继续？',
    confirmText: '重启',
    action: () => runAction('restart'),
  }
}
function askStop() {
  confirm.value = {
    title: '停止 fluxzy',
    message: '停止后 443 端口将不再接收流量，直到手动启动。确定继续？',
    confirmText: '停止',
    danger: true,
    action: () => runAction('stop'),
  }
}
function askRotate() {
  confirm.value = {
    title: '立即轮转会话',
    message: '轮转会重启 fluxzy 并创建新的会话目录，旧会话将归档。确定继续？',
    confirmText: '轮转',
    action: () => runAction('rotate'),
  }
}

async function doConfirm() {
  const c = confirm.value
  if (!c) return
  confirm.value = null
  await c.action()
}

async function saveStartup() {
  if (!loaded.value) {
    toast.error('配置尚未加载', '请等待运行时配置读取完成后再保存')
    return
  }
  savingStartup.value = true
  try {
    await runtime.saveStartup({ ...startupForm })
    syncStartup()
    toast.success('启动配置已保存', '尚未应用；应用后会重启 fluxzy')
  } catch (e: any) {
    toast.error('保存失败', e?.message)
  } finally {
    savingStartup.value = false
  }
}

function askApplyStartup() {
  if (!runtime.startup?.hash) return
  confirm.value = {
    title: '应用启动配置',
    message: '应用会重启 fluxzy、中断在途请求并创建新会话。确定继续？',
    confirmText: '应用并重启',
    action: async () => {
      try {
        const r = await runtime.applyStartup(runtime.startup!.hash)
        syncStartup()
        toast.success('启动配置已应用', r.sessionId ? `新会话 ${r.sessionId}` : 'fluxzy 已重启')
      } catch (e: any) {
        toast.error('应用失败', e?.message)
      }
    },
  }
}

async function saveRotation() {
  if (!loaded.value) {
    toast.error('配置尚未加载', '请等待运行时配置读取完成后再保存')
    return
  }
  const minGB = Number(minFreeGB.value)
  const emergencyGB = Number(emergencyFreeGB.value)
  if (!Number.isFinite(minGB) || minGB < 0 || !Number.isFinite(emergencyGB) || emergencyGB < 0) {
    toast.error('保存失败', '磁盘阈值必须是非负数字')
    return
  }
  if (emergencyGB > minGB) {
    toast.error('保存失败', '紧急停止阈值不能高于最小剩余空间')
    return
  }
  savingRotation.value = true
  try {
    await runtime.saveRotation({
      ...rotationForm,
      minFreeBytes: Math.round(minGB * BYTES_PER_GB),
      emergencyFreeBytes: Math.round(emergencyGB * BYTES_PER_GB),
    })
    syncRotation()
    toast.success('轮转策略已保存', '下一次检查周期生效')
  } catch (e: any) {
    toast.error('保存失败', e?.message)
  } finally {
    savingRotation.value = false
  }
}

async function loadLogs() {
  logsLoading.value = true
  logsError.value = ''
  try {
    await runtime.fetchLogs(300, 0)
  } catch (e: any) {
    logsError.value = e?.message || '读取日志失败'
  } finally {
    logsLoading.value = false
  }
}

function toggleLogs() {
  showLogs.value = !showLogs.value
  if (showLogs.value) void loadLogs()
}
</script>

<template>
  <div class="space-y-5">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <h1 class="text-[22px] font-semibold tracking-tight text-ink">运行时</h1>
        <p class="mt-1 text-[12.5px] text-muted">fluxzy 进程、启动参数、会话轮转与 CLI 版本。</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <Badge :variant="stateTone"><StatusDot :tone="stateTone" :pulse="state === 'running'" class="mr-1.5" />{{ stateLabel }}</Badge>
        <Badge v-if="runtime.configPending" variant="warn">启动配置待应用</Badge>
        <Badge variant="accent">内置 fluxzy</Badge>
      </div>
    </div>

    <div v-if="runtime.error" class="rounded-2xl border border-bad/25 bg-bad/8 px-4 py-3 text-[12px] text-bad">
      运行时状态读取失败：{{ runtime.error }}
    </div>

    <div class="grid gap-5 xl:grid-cols-2">
      <Card title="进程状态" subtitle="console 直接监管 fluxzy 子进程">
        <template #actions>
          <Button size="sm" :loading="runtime.actionLoading === 'start'" :disabled="!auth.isOperator || state === 'running' || state === 'starting'" @click="runAction('start')">启动</Button>
          <Button size="sm" variant="danger" :loading="runtime.actionLoading === 'stop'" :disabled="!auth.isOperator || state === 'stopped' || state === 'failed'" @click="askStop">停止</Button>
          <Button size="sm" :loading="runtime.actionLoading === 'restart'" :disabled="!auth.isOperator || state !== 'running'" @click="askRestart">重启</Button>
          <Button size="sm" :loading="runtime.actionLoading === 'rotate'" :disabled="!auth.isOperator || state !== 'running'" @click="askRotate">轮转</Button>
        </template>
        <dl class="grid grid-cols-2 gap-x-4 gap-y-3 text-[12px]">
          <div><dt class="text-faint">PID</dt><dd class="mt-0.5 font-mono text-ink">{{ sup?.pid || '-' }}</dd></div>
          <div><dt class="text-faint">运行时长</dt><dd class="mt-0.5 text-ink">{{ sup?.uptimeMs ? formatDuration(sup.uptimeMs) : '-' }}</dd></div>
          <div><dt class="text-faint">当前会话</dt><dd class="mt-0.5 truncate font-mono text-ink">{{ sup?.activeSessionId || '-' }}</dd></div>
          <div><dt class="text-faint">重启次数</dt><dd class="mt-0.5 text-ink">{{ sup?.restartCount || 0 }}</dd></div>
          <div><dt class="text-faint">CLI 版本</dt><dd class="mt-0.5 text-ink">{{ currentVersion }}</dd></div>
          <div><dt class="text-faint">来源</dt><dd class="mt-0.5 text-ink">{{ sup?.binarySource === 'bundled' ? '镜像内置' : (sup?.binarySource || '-') }}</dd></div>
        </dl>
        <div v-if="sup?.lastError" class="mt-4 rounded-xl border border-bad/30 bg-bad/8 p-3 text-[11.5px] text-bad">
          {{ sup.lastError }}
        </div>
        <p class="mt-4 text-[11px] text-faint">启动/停止/重启会中断在途请求。</p>
      </Card>

      <Card title="fluxzy CLI" subtitle="内置版本">
        <template #actions>
          <Badge variant="accent">内置 fluxzy</Badge>
        </template>
        <div class="grid grid-cols-2 gap-3 text-[12px]">
          <div><div class="text-faint">当前版本</div><div class="mt-1 font-mono text-ink">{{ currentVersion }}</div></div>
          <div><div class="text-faint">来源</div><div class="mt-1 text-ink">{{ sup?.binarySource === 'bundled' ? '镜像内置' : (sup?.binarySource || '-') }}</div></div>
        </div>
        <div class="mt-4 rounded-xl border border-accent/25 bg-accent/8 p-3 text-[11.5px] leading-relaxed text-accent">
          内置 fluxzy 支持请求/响应体改写与规则热更新。
        </div>
        <p class="mt-3 text-[11.5px] leading-relaxed text-faint">CLI 随镜像版本发布。</p>
      </Card>
    </div>

    <Card title="启动参数" subtitle="白名单参数；保存后需应用才会重启 fluxzy">
      <template #actions>
        <Badge v-if="pendingStartup" variant="warn">待应用</Badge>
        <Button size="sm" :loading="savingStartup" :disabled="!auth.isAdmin || !loaded" @click="saveStartup">保存</Button>
        <Button size="sm" variant="primary" :disabled="!auth.isAdmin || !loaded || !pendingStartup" :loading="runtime.actionLoading === 'apply-startup'" @click="askApplyStartup">应用并重启</Button>
      </template>
      <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <Input v-model="startupForm.listenAddr" label="监听地址" hint="host:port；修改端口需同步 Docker 映射" :disabled="!auth.isAdmin" />
        <Select v-model="startupForm.mode" label="代理模式" :disabled="!auth.isAdmin" :options="[{ value: 'ReverseSecure', label: 'ReverseSecure' }, { value: 'ReversePlain', label: 'ReversePlain' }, { value: 'Regular', label: 'Regular' }]" />
        <Input v-model.number="startupForm.reversePort" type="number" label="反向代理端口" :disabled="!auth.isAdmin || startupForm.mode === 'Regular'" />
        <Input v-model.number="startupForm.maxUpstreamConnections" type="number" label="最大上游连接数" hint="每 host，1–1024" :disabled="!auth.isAdmin" />
        <Input v-model.number="startupForm.requestBuffer" type="number" label="请求缓冲区（字节）" hint="16–524288" :disabled="!auth.isAdmin" />
        <Input v-model.number="startupForm.maxCaptureCount" type="number" label="最大捕获条数" hint="0 表示不限制" :disabled="!auth.isAdmin" />
      </div>
      <details class="group mt-4 rounded-xl border border-line/60 bg-panel-strong/25 p-3">
        <summary class="flex cursor-pointer list-none items-center justify-between gap-3 [&::-webkit-details-marker]:hidden">
          <span class="flex flex-wrap items-center gap-2 text-[11.5px] font-medium text-muted">
            高级选项
            <Badge variant="muted">通常不需要修改</Badge>
            <Badge v-if="advancedEnabled" variant="warn">已启用自定义项</Badge>
          </span>
          <ChevronDown class="h-4 w-4 text-faint transition group-open:rotate-180" />
        </summary>
        <p class="mt-2 text-[11px] leading-relaxed text-faint">
          这些开关会影响 HTTP/2、性能或 HTTPS 审计能力。不确定时保持默认关闭。
        </p>
        <div class="mt-3 grid gap-3 md:grid-cols-2">
          <Switch v-model="startupForm.serveH2" label="Serve H2" hint="下行 HTTP/2" :disabled="!auth.isAdmin" />
          <Switch v-model="startupForm.parseUA" label="解析 UA" hint="增加每请求 CPU" :disabled="!auth.isAdmin" />
          <Switch v-model="startupForm.noCertCache" label="禁用证书缓存" hint="显著增加 CPU/内存开销" :disabled="!auth.isAdmin" />
          <Switch v-model="startupForm.skipSSLDecryption" label="跳过 SSL 解密" hint="会失去 HTTPS 审计" :disabled="!auth.isAdmin" />
        </div>
        <p class="mt-3 rounded-lg border border-warn/20 bg-warn/8 p-2 text-[11px] leading-relaxed text-warn">
          “跳过 SSL 解密”会让 HTTPS 请求以密文形式通过，无法审计 header/body；“禁用证书缓存”会显著增加资源占用。
        </p>
      </details>
      <div v-if="!auth.isAdmin" class="mt-3 text-[11.5px] text-faint">只有管理员可以修改启动参数。</div>
    </Card>

    <div class="grid gap-5 xl:grid-cols-2">
      <Card title="会话轮转" subtitle="活动会话无法直接清理，轮转后可进入保留流程">
        <template #actions>
          <Button size="sm" :loading="savingRotation" :disabled="!auth.isAdmin || !loaded" @click="saveRotation">保存</Button>
        </template>
        <div class="grid gap-4 sm:grid-cols-2">
          <Switch v-model="rotationForm.enabled" label="启用自动轮转" :disabled="!auth.isAdmin" />
          <Input v-model.number="rotationForm.intervalHours" type="number" min="0" label="按时间（小时）" hint="0 = 不限制" :disabled="!auth.isAdmin" />
          <Input v-model.number="rotationForm.maxExchanges" type="number" min="0" label="按 exchange 数" hint="0 = 不限制" :disabled="!auth.isAdmin" />
          <Input v-model.number="rotationForm.maxSessionBytes" type="number" min="0" label="按会话大小（字节）" hint="0 = 不限制" :disabled="!auth.isAdmin" />
          <Input v-model.number="minFreeGB" type="number" min="0" step="0.1" label="最小剩余空间（GB）" hint="启用自动轮转时生效；先清理过期数据，仍不足则轮转" :disabled="!auth.isAdmin" />
          <Input v-model.number="emergencyFreeGB" type="number" min="0" step="0.1" label="紧急停止阈值（GB）" hint="启用自动轮转时生效；低于此值停止 fluxzy，不能高于最小剩余空间" :disabled="!auth.isAdmin" />
        </div>
        <div class="mt-4 rounded-xl border border-line/60 bg-panel-strong/40 p-3 text-[11.5px] text-muted">
          磁盘剩余：<span class="font-medium text-ink">{{ formatBytes(runtime.rotation?.disk?.freeBytes) }}</span>
          · 当前会话：<span class="font-medium text-ink">{{ formatBytes(runtime.rotation?.sessionBytes) }}</span>
          · 上次轮转：{{ runtime.rotation?.lastRotatedAt ? formatRelative(runtime.rotation.lastRotatedAt) : '无' }}
          <span v-if="runtime.rotation?.lastReason">（{{ runtime.rotation.lastReason }}）</span>
        </div>
        <p class="mt-3 text-[11.5px] leading-relaxed text-faint">
          磁盘阈值仅在启用自动轮转时检查。最小剩余空间用于“先清理、再轮转”；低于紧急停止阈值时 fluxzy 会停止，剩余空间恢复到最小剩余空间以上后自动启动。
        </p>
        <p class="mt-2 text-[11.5px] leading-relaxed text-faint">
          轮转 = 重启 fluxzy + 新建 <code>sessions/&lt;UTC&gt;</code> 目录。旧会话归档后仍按热/冷保留策略处理；轮转本身不会立即释放磁盘。
        </p>
      </Card>

      <Card title="运行日志" subtitle="fluxzy stdout / stderr 与进程生命周期（最近 300 行）">
        <template #actions>
          <Button size="sm" variant="ghost" :loading="logsLoading" @click="loadLogs">刷新</Button>
          <Button size="sm" variant="ghost" @click="toggleLogs">{{ showLogs ? '隐藏' : '显示' }}</Button>
        </template>
        <div v-if="showLogs" class="max-h-[360px] min-h-[96px] overflow-auto rounded-xl border border-line/60 bg-slate-950/90 p-3 font-mono text-[11px] leading-relaxed text-slate-100">
          <div v-if="logsLoading && !runtime.logs.length" class="text-slate-500">正在读取日志…</div>
          <div v-else-if="logsError" class="text-amber-300">{{ logsError }}</div>
          <div v-else-if="!runtime.logs.length" class="text-slate-500">暂无日志。fluxzy 尚未启动或还没有输出。</div>
          <div v-for="line in runtime.logs" :key="line.seq" :class="line.stream === 'stderr' ? 'text-amber-300' : line.stream === 'console' ? 'text-cyan-300' : ''">
            <span class="text-slate-500">{{ formatDateTime(line.at) }}</span>
            <span class="ml-2 text-slate-500">{{ line.stream }}</span>
            <span class="ml-2">{{ line.text }}</span>
          </div>
        </div>
        <div v-else class="text-[12px] text-faint">日志已隐藏，点击“显示”重新加载。</div>
      </Card>
    </div>

    <Modal :open="!!confirm" :title="confirm?.title" @update:open="(v) => { if (!v) confirm = null }">
      <p class="text-[12.5px] leading-relaxed text-muted">{{ confirm?.message }}</p>
      <template #footer>
        <Button variant="ghost" @click="confirm = null">取消</Button>
        <Button :variant="confirm?.danger ? 'danger' : 'primary'" @click="doConfirm">{{ confirm?.confirmText }}</Button>
      </template>
    </Modal>
  </div>
</template>

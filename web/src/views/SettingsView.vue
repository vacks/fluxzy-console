<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  CheckCircle2,
  Download,
  ExternalLink,
  HardDrive,
  KeyRound,
  Monitor,
  Moon,
  RefreshCw,
  RotateCcw,
  Server,
  Settings2,
  ShieldCheck,
  Sun,
  Upload,
} from 'lucide-vue-next'
import { api, fetchDownload } from '@/api/client'
import type { CAInfo, RetentionSettings, SystemInfo } from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore } from '@/stores/theme'
import { useToastStore } from '@/stores/toast'
import Button from '@/components/ui/Button.vue'
import Card from '@/components/ui/Card.vue'
import Badge from '@/components/ui/Badge.vue'
import Input from '@/components/ui/Input.vue'
import Modal from '@/components/ui/Modal.vue'
import { formatDateTime } from '@/utils/format'

const auth = useAuthStore()
const theme = useThemeStore()
const toast = useToastStore()
const router = useRouter()

const system = ref<SystemInfo | null>(null)
const loading = ref(true)
const password = ref({ current: '', next: '', confirm: '' })
const changing = ref(false)
const retention = ref<RetentionSettings>({ hotHours: 24, coldDays: 30, samplePercent: 5 })
const retentionReady = ref(false)
const retentionSaving = ref(false)
const retentionError = ref('')
const isAdmin = computed(() => auth.user?.role === 'admin')
const canOperate = computed(() => auth.user?.role === 'admin' || auth.user?.role === 'operator')

const caInfo = ref<CAInfo | null>(null)
const caLoading = ref(true)
const caBusy = ref('')
const caCommonName = ref('fluxzy-console ROOT CA')
const caFileInput = ref<HTMLInputElement | null>(null)
const caConfirm = ref<{
  title: string
  message: string
  confirmText: string
  action: () => Promise<void>
} | null>(null)
const caCLIAvailable = computed(() => system.value?.cliAvailable ?? caInfo.value !== null)

function humanHours(hours: number): string {
  if (hours <= 0) return '不自动清理'
  if (hours % 24 === 0) return `${hours / 24} 天`
  return `${hours} 小时`
}

function humanDays(days: number): string {
  if (days <= 0) return '不保留冷索引'
  return `${days} 天`
}

async function load() {
  loading.value = true
  retentionError.value = ''
  retentionReady.value = false
  const [sysResult, retResult] = await Promise.allSettled([
    api.get<SystemInfo>('/system/info'),
    api.get<{ settings: RetentionSettings }>('/settings/retention'),
  ])
  if (sysResult.status === 'fulfilled') {
    system.value = sysResult.value
  } else {
    // System details are no longer displayed on this page, but cliAvailable
    // and the embedded retention fallback still come from /system/info.
    try {
      system.value = await api.get<SystemInfo>('/system/info')
    } catch {
      system.value = null
    }
  }
  if (retResult.status === 'fulfilled') {
    retention.value = retResult.value.settings
    retentionReady.value = true
  } else if (system.value?.retention) {
    retention.value = system.value.retention
    retentionReady.value = true
  } else {
    retentionError.value = retResult.reason?.message || '读取保留策略失败'
  }
  loading.value = false
  if (canOperate.value) {
    await loadCA()
  } else {
    caLoading.value = false
  }
}

async function loadCA() {
  caLoading.value = true
  try {
    const r = await api.get<{ info: CAInfo }>('/system/ca/status')
    caInfo.value = r.info
  } catch {
    caInfo.value = null
  } finally {
    caLoading.value = false
  }
}

function pickCAFile() {
  if (caBusy.value) return
  caFileInput.value?.click()
}

function onCAFileChange(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  if (!/\.(pfx|p12)$/i.test(file.name)) {
    toast.error('格式不支持', '请上传 PKCS#12 格式的 .pfx 或 .p12 文件')
    return
  }
  caConfirm.value = {
    title: '上传自定义 CA',
    message: '上传会替换当前 CA 并重启 fluxzy、创建新会话。所有通过 fluxzy 抓包的客户端都需要重新信任新 CA。确定继续？',
    confirmText: '上传并重启',
    action: async () => {
      caBusy.value = 'upload'
      try {
        const form = new FormData()
        form.append('file', file)
        const r = await api.upload<{ info: CAInfo; message: string }>('/system/ca', form, { timeoutMs: 120_000 })
        caInfo.value = r.info
        toast.success('CA 已更新', r.message)
      } catch (e: any) {
        toast.error('CA 上传失败', e?.message)
      } finally {
        caBusy.value = ''
      }
    },
  }
}

function askRegenerateCA() {
  if (caBusy.value) return
  const commonName = caCommonName.value.trim()
  if (!commonName) {
    toast.error('CA 名称不能为空')
    return
  }
  caConfirm.value = {
    title: '重新生成 CA',
    message: `将创建新的自签名根 CA「${commonName}」并替换当前 CA，fluxzy 会重启。旧 CA 签发的证书将不再受信任。确定继续？`,
    confirmText: '生成并重启',
    action: async () => {
      caBusy.value = 'regenerate'
      try {
        const r = await api.post<{ info: CAInfo; message: string }>(
          '/system/ca/regenerate',
          { commonName },
          { timeoutMs: 120_000 },
        )
        caInfo.value = r.info
        toast.success('CA 已生成', r.message)
      } catch (e: any) {
        toast.error('CA 生成失败', e?.message)
      } finally {
        caBusy.value = ''
      }
    },
  }
}

function askResetCA() {
  if (caBusy.value) return
  caConfirm.value = {
    title: '恢复内置 CA',
    message: '将删除自定义 CA，恢复 fluxzy 内置 CA 并重启。客户端需要重新信任 fluxzy 内置 CA。确定继续？',
    confirmText: '恢复内置 CA',
    action: async () => {
      caBusy.value = 'reset'
      try {
        const r = await api.del<{ info: CAInfo; message: string }>('/system/ca')
        caInfo.value = r.info
        toast.success('已恢复内置 CA', r.message)
      } catch (e: any) {
        toast.error('恢复失败', e?.message)
      } finally {
        caBusy.value = ''
      }
    },
  }
}

async function doCAConfirm() {
  const action = caConfirm.value?.action
  caConfirm.value = null
  if (action) await action()
}

onMounted(load)

async function saveRetention() {
  retentionError.value = ''
  const hot = Number(retention.value.hotHours)
  const cold = Number(retention.value.coldDays)
  const sample = Number(retention.value.samplePercent)
  if (!Number.isInteger(hot) || hot < 0 || hot > 8760) {
    retentionError.value = '热数据保留小时数必须在 0–8760 之间'
    return
  }
  if (!Number.isInteger(cold) || cold < 0 || cold > 3650) {
    retentionError.value = '冷数据保留天数必须在 0–3650 之间'
    return
  }
  if (!Number.isInteger(sample) || sample < 0 || sample > 100) {
    retentionError.value = '采样比例必须在 0–100 之间'
    return
  }
  if (hot > 0 && cold > 0 && cold * 24 < hot) {
    retentionError.value = '冷数据保留时间不能短于热数据保留时间'
    return
  }
  retentionSaving.value = true
  try {
    const r = await api.put<{ settings: RetentionSettings; message: string }>('/settings/retention', {
      hotHours: hot,
      coldDays: cold,
      samplePercent: sample,
    })
    retention.value = r.settings
    toast.success('保留策略已保存', r.message || '将在下一次清理周期生效')
    await load()
  } catch (e: any) {
    retentionError.value = e?.message || '保存保留策略失败'
    toast.error('保存失败', retentionError.value)
  } finally {
    retentionSaving.value = false
  }
}

async function downloadCA() {
  if (!canOperate.value) {
    toast.warning('权限不足', '只有管理员可以导出 CA')
    return
  }
  if (!system.value?.cliAvailable) return
  try {
    await fetchDownload('/system/ca', 'fluxzy-ca.cer')
    toast.success('CA 已导出', '用于让客户端信任 fluxzy 的 HTTPS 解密证书')
  } catch (e: any) {
    toast.error('导出 CA 失败', e?.message || '未知错误')
  }
}

async function changePassword() {
  if (password.value.next.length < 10) {
    toast.error('新密码至少 5 个字符')
    return
  }
  if (password.value.next !== password.value.confirm) {
    toast.error('两次输入的新密码不一致')
    return
  }
  changing.value = true
  try {
    await api.post('/auth/password', {
      currentPassword: password.value.current,
      newPassword: password.value.next,
    })
    toast.success('密码已修改', '请使用新密码重新登录')
    auth.reset()
    router.push({ name: 'login' })
  } catch (e: any) {
    toast.error('修改密码失败', e?.message)
  } finally {
    changing.value = false
  }
}

const modes = [
  { key: 'dark', label: '深色', icon: Moon },
  { key: 'light', label: '浅色', icon: Sun },
  { key: 'system', label: '跟随系统', icon: Monitor },
] as const
</script>

<template>
  <div class="space-y-4">
    <section class="panel flex flex-col gap-4 p-4 lg:flex-row lg:items-center lg:justify-between">
      <div class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-accent/10">
          <Settings2 class="h-5 w-5 text-accent" />
        </div>
        <div>
          <h2 class="text-[15px] font-semibold tracking-tight text-ink">设置</h2>
          <p class="mt-0.5 text-[11.5px] text-muted">保留策略、CA 证书、账号安全与外观。</p>
        </div>
      </div>
      <Button size="sm" :loading="loading" @click="load">
        <template #icon><RefreshCw class="h-3.5 w-3.5" /></template>
        刷新
      </Button>
    </section>

    <div class="space-y-4">
      <Card title="保留与采样" subtitle="控制原始数据、冷索引和磁盘增长">
        <div v-if="retentionReady" class="space-y-3">
          <div class="flex items-start gap-2 rounded-xl border border-line/50 bg-panel-strong/35 p-3">
            <HardDrive class="mt-0.5 h-4 w-4 shrink-0 text-accent" />
            <div class="min-w-0 flex-1">
              <div class="text-[11.5px] text-ink">热数据：原始文件 + 全量索引</div>
              <div class="mt-0.5 text-[10.5px] leading-relaxed text-faint">
                从采集时间起保留 {{ humanHours(retention.hotHours) }}。期间可查看完整 header/body；过期后原始文件与热索引删除，按冷数据配置保留采样索引（冷保留为 0 或采样比例为 0 时不保留），body/header 不可恢复。
              </div>
            </div>
            <Badge variant="accent">{{ humanHours(retention.hotHours) }}</Badge>
          </div>

          <div class="flex items-start gap-2 rounded-xl border border-line/50 bg-panel-strong/35 p-3">
            <Server class="mt-0.5 h-4 w-4 shrink-0 text-accent-2" />
            <div class="min-w-0 flex-1">
              <div class="text-[11.5px] text-ink">冷数据：采样索引</div>
              <div class="mt-0.5 text-[10.5px] leading-relaxed text-faint">
                从采集时间起保留 {{ humanDays(retention.coldDays) }}，按 {{ retention.samplePercent }}% 确定性采样保留请求索引（仅元数据，不含 body/header）。到期后冷索引删除；聚合统计另保留 90 天。
              </div>
            </div>
            <Badge variant="muted">{{ humanDays(retention.coldDays) }}</Badge>
          </div>

          <div class="rounded-xl border border-line/50 bg-panel-strong/25 p-3 text-[10.5px] leading-relaxed text-faint">
            活跃会话和固定（pinned）会话不会被自动清理。要限制活动会话大小，请在“运行时 → 会话轮转”中配置时间、条数、大小或磁盘阈值。采样比例只影响之后从热转冷的会话，不会重新采样已有冷索引；实时页面的推送限流与这里无关。
          </div>

          <div class="grid gap-3 sm:grid-cols-3">
            <label class="block">
              <span class="mb-1.5 block text-[11px] text-muted">热数据保留（小时）</span>
              <input
                v-model.number="retention.hotHours"
                type="number"
                min="0"
                max="8760"
                step="1"
                class="input h-9 w-full py-2 text-[12px]"
                :disabled="!isAdmin || retentionSaving"
              />
              <span class="mt-1 block text-[10px] text-faint">0 = 不自动清理原始数据</span>
            </label>
            <label class="block">
              <span class="mb-1.5 block text-[11px] text-muted">冷数据保留（天）</span>
              <input
                v-model.number="retention.coldDays"
                type="number"
                min="0"
                max="3650"
                step="1"
                class="input h-9 w-full py-2 text-[12px]"
                :disabled="!isAdmin || retentionSaving"
              />
              <span class="mt-1 block text-[10px] text-faint">从采集时间起计算；0 = 不保留冷索引</span>
            </label>
            <label class="block">
              <span class="mb-1.5 block text-[11px] text-muted">冷数据采样（%）</span>
              <input
                v-model.number="retention.samplePercent"
                type="number"
                min="0"
                max="100"
                step="1"
                class="input h-9 w-full py-2 text-[12px]"
                :disabled="!isAdmin || retentionSaving"
              />
              <span class="mt-1 block text-[10px] text-faint">0 = 不保留冷索引</span>
            </label>
          </div>

          <div v-if="retentionError" class="rounded-xl border border-bad/30 bg-bad/8 p-3 text-[11px] text-bad">
            {{ retentionError }}
          </div>

          <div class="flex flex-wrap items-center justify-between gap-2">
            <p class="text-[10.5px] leading-relaxed text-faint">
              保存后在下一次清理周期（每小时，启动时也会执行一次）生效。缩短保留时间会永久删除已有数据，无法恢复。
            </p>
            <Button
              size="sm"
              variant="primary"
              :loading="retentionSaving"
              :disabled="!isAdmin"
              @click="saveRetention"
            >
              <template #icon><CheckCircle2 class="h-3.5 w-3.5" /></template>
              保存保留策略
            </Button>
          </div>
          <p v-if="!isAdmin" class="text-[10.5px] text-faint">只有管理员可以修改保留策略。</p>
          <p class="text-[10px] leading-relaxed text-faint">
            默认值为热数据 24 小时、冷数据 30 天、冷采样 5%；在此保存后会写入数据库并覆盖默认值。
          </p>
        </div>
        <div v-else-if="loading" class="h-28 skeleton" />
        <div v-else class="rounded-xl border border-bad/25 bg-bad/8 p-3 text-[12px] text-bad">
          读取保留策略失败，请刷新重试。
        </div>
      </Card>

      <Card title="HTTPS 证书与 CA" subtitle="让客户端信任 fluxzy 的 MITM CA">
        <div class="space-y-3">
          <div class="rounded-xl border border-line/50 bg-panel-strong/35 p-3.5">
            <div class="flex items-start justify-between gap-3">
              <div class="flex min-w-0 items-start gap-2">
                <ShieldCheck class="mt-0.5 h-4 w-4 shrink-0 text-accent" />
                <div class="min-w-0 text-[11.5px] leading-relaxed text-muted">
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="font-medium text-ink">当前 CA</span>
                    <Badge v-if="caInfo" :variant="caInfo.source === 'custom' ? 'accent' : 'muted'">
                      {{ caInfo.source === 'custom' ? '自定义' : '内置' }}
                    </Badge>
                    <Badge v-if="caInfo && caInfo.daysRemaining <= 30" variant="warn">
                      {{ caInfo.daysRemaining }} 天后过期
                    </Badge>
                  </div>
                  <div v-if="caLoading" class="mt-1 text-[10.5px] text-faint">读取 CA 信息…</div>
                  <template v-else-if="caInfo">
                    <div class="mt-1 truncate font-mono text-[10.5px] text-faint" :title="caInfo.subject">{{ caInfo.subject }}</div>
                    <div class="mt-1 text-[10.5px] text-faint">
                      有效期至 {{ formatDateTime(caInfo.notAfter) }} · 指纹 {{ caInfo.fingerprintSha256.slice(0, 16) }}…
                    </div>
                  </template>
                  <div v-else class="mt-1 text-[10.5px]" :class="canOperate ? 'text-warn' : 'text-faint'">
                    {{ canOperate ? '无法读取 CA 状态，可能是 fluxzy CLI 不可用。' : '当前账号无权查看 CA 管理信息。' }}
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div class="rounded-xl border border-line/50 bg-panel-strong/25 p-3 text-[10.5px] leading-relaxed text-faint">
            更换 CA 会重启 fluxzy、创建新会话，并中断在途请求。所有通过 fluxzy 抓包的设备/浏览器都必须重新安装并信任新 CA。
            上传文件必须是未加密的 PKCS#12 格式 <code>.pfx</code> / <code>.p12</code>；不支持受密码保护的证书。
          </div>

          <div class="flex flex-wrap gap-2">
            <Button size="sm" variant="primary" :disabled="!caCLIAvailable || !auth.isOperator || !!caBusy" @click="downloadCA">
              <template #icon><Download class="h-4 w-4" /></template>
              下载当前 CA
            </Button>
            <Button size="sm" :loading="caBusy === 'upload'" :disabled="!auth.isAdmin || !caCLIAvailable || !!caBusy" @click="pickCAFile">
              <template #icon><Upload class="h-4 w-4" /></template>
              上传自定义 CA
            </Button>
            <input ref="caFileInput" type="file" accept=".pfx,.p12,application/x-pkcs12" class="hidden" @change="onCAFileChange" />
            <Button
              size="sm"
              variant="ghost"
              :loading="caBusy === 'reset'"
              :disabled="!auth.isAdmin || !!caBusy"
              @click="askResetCA"
            >
              <template #icon><RotateCcw class="h-4 w-4" /></template>
              恢复内置 CA
            </Button>
          </div>

          <div v-if="auth.isAdmin" class="rounded-xl border border-line/50 bg-panel-strong/25 p-3">
            <div class="mb-2 text-[11px] font-medium text-muted">生成新 CA</div>
            <div class="flex flex-col gap-2 sm:flex-row sm:items-end">
              <Input v-model="caCommonName" label="CA 名称（Common Name）" hint="例如：My Company Root CA" class="flex-1" />
              <Button size="sm" :loading="caBusy === 'regenerate'" :disabled="!caCLIAvailable || !!caBusy" @click="askRegenerateCA">
                <template #icon><RefreshCw class="h-4 w-4" /></template>
                重新生成
              </Button>
            </div>
          </div>

          <div v-if="canOperate && !caCLIAvailable" class="rounded-xl border border-warn/25 bg-warn/8 p-3 text-[11px] text-warn">
            未找到可用的 fluxzy CLI，无法读取、导出或生成 CA。请到“运行时”页面检查 CLI 版本和来源。
          </div>
        </div>
      </Card>

      <Card title="账号安全" subtitle="修改当前账号密码">
        <div class="space-y-3">
          <Input v-model="password.current" label="当前密码" type="password" autocomplete="current-password" />
          <Input v-model="password.next" label="新密码" type="password" autocomplete="new-password" hint="至少 5 个字符" />
          <Input v-model="password.confirm" label="确认新密码" type="password" autocomplete="new-password" />
          <Button variant="primary" :loading="changing" @click="changePassword">
            <template #icon><KeyRound class="h-4 w-4" /></template>
            修改密码并重新登录
          </Button>
        </div>
      </Card>

      <Card title="外观与语言" subtitle="保存在当前浏览器">
        <div class="space-y-4">
          <div>
            <div class="mb-2 text-[11.5px] font-medium text-muted">主题</div>
            <div class="grid grid-cols-3 gap-2">
              <button
                v-for="m in modes"
                :key="m.key"
                class="rounded-xl border p-3 text-center transition"
                :class="theme.mode === m.key ? 'border-accent/50 bg-accent/8 text-ink' : 'border-line/60 bg-panel-strong/35 text-muted hover:border-accent/30'"
                :aria-pressed="theme.mode === m.key"
                @click="theme.setMode(m.key)"
              >
                <component :is="m.icon" class="mx-auto h-4 w-4" />
                <div class="mt-1.5 text-[11.5px]">{{ m.label }}</div>
                <div v-if="theme.mode === m.key" class="mt-1 flex items-center justify-center gap-1 text-[10px] text-accent"><CheckCircle2 class="h-3 w-3" /> 当前</div>
              </button>
            </div>
          </div>
          <div>
            <div class="mb-2 text-[11.5px] font-medium text-muted">语言</div>
            <div class="flex items-center justify-between rounded-xl border border-line/60 bg-panel-strong/35 px-3.5 py-2.5">
              <span class="text-[12px] text-ink">简体中文</span>
              <Badge variant="ok">默认</Badge>
            </div>
            <div class="mt-2 flex items-center justify-between rounded-xl border border-line/60 bg-panel-strong/25 px-3.5 py-2.5 opacity-60">
              <span class="text-[12px] text-muted">English</span>
              <Badge variant="muted">预留</Badge>
            </div>
          </div>
        </div>
      </Card>
    </div>

    <Card title="关于" subtitle="版本与许可">
      <div class="flex flex-col gap-3 text-[11.5px] text-muted sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div class="font-mono text-[12px] text-ink">FLUXZY · CONSOLE v{{ system?.version || '-' }}</div>
          <div class="mt-1 text-[10.5px] text-faint">Go + Vue 3 · 用于 fluxzy 的流量观测与规则控制台</div>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <a class="btn-ghost text-[11px]" href="https://www.fluxzy.io" target="_blank" rel="noreferrer">
            fluxzy.io <ExternalLink class="h-3 w-3" />
          </a>
          <a class="btn-ghost text-[11px]" href="https://github.com/haga-rak/fluxzy.core" target="_blank" rel="noreferrer">
            fluxzy.core <ExternalLink class="h-3 w-3" />
          </a>
          <Badge variant="muted">fluxzy CLI: GPLv3</Badge>
        </div>
      </div>
    </Card>

    <Modal :open="!!caConfirm" :title="caConfirm?.title" @update:open="(v) => { if (!v) caConfirm = null }">
      <p class="text-[12.5px] leading-relaxed text-muted">{{ caConfirm?.message }}</p>
      <div class="mt-3 rounded-xl border border-warn/25 bg-warn/8 p-3 text-[11px] text-warn">
        CA 变更会重启 fluxzy 并中断在途请求；客户端必须重新信任新的根证书。
      </div>
      <template #footer>
        <Button variant="ghost" @click="caConfirm = null">取消</Button>
        <Button variant="primary" :loading="!!caBusy" @click="doCAConfirm">{{ caConfirm?.confirmText || '确定' }}</Button>
      </template>
    </Modal>
  </div>
</template>

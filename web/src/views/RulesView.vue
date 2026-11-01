<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  Code2,
  Copy,
  Eye,
  History,
  LayoutTemplate,
  Plus,
  RotateCcw,
  Save,
  ShieldCheck,
  Trash2,
  Wand2,
  XCircle,
} from 'lucide-vue-next'
import { api } from '@/api/client'
import type {
  ConfigVersion,
  RuleDraft,
  RuleModel,
  RuleSchema,
  RulesStatus,
  ValidationResult,
} from '@/api/types'
import { useAuthStore } from '@/stores/auth'
import { useToastStore } from '@/stores/toast'
import Button from '@/components/ui/Button.vue'
import Badge from '@/components/ui/Badge.vue'
import Card from '@/components/ui/Card.vue'
import Modal from '@/components/ui/Modal.vue'
import Input from '@/components/ui/Input.vue'
import Textarea from '@/components/ui/Textarea.vue'
import CodeBlock from '@/components/ui/CodeBlock.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import TypeForm from '@/components/rules/TypeForm.vue'
import FilterCollectionEditor from '@/components/rules/FilterCollectionEditor.vue'
import YamlEditor from '@/components/rules/YamlEditor.vue'
import { formatDateTime, formatRelative } from '@/utils/format'

const auth = useAuthStore()
const toast = useToastStore()

const status = ref<RulesStatus | null>(null)
const schema = ref<RuleSchema | null>(null)
const model = ref<RuleDraft[]>([])
const yamlText = ref('')
const mode = ref<'visual' | 'yaml' | 'preview'>('visual')
const selected = ref(0)
const loading = ref(true)
const loaded = ref(false)
const saving = ref(false)
const validating = ref(false)
const dirty = ref(false)
const validation = ref<ValidationResult | null>(null)
const previewYaml = ref('')
const versionsOpen = ref(false)
const templatesOpen = ref(false)
const saveOpen = ref(false)
const applyOpen = ref(false)
const applyMessage = ref('')
const applying = ref(false)
const applyResult = ref<any>(null)
const note = ref('')
const versionPreview = ref<ConfigVersion | null>(null)
const rollbackTarget = ref<ConfigVersion | null>(null)
const rollbackOpen = ref(false)

const filters = computed(() => schema.value?.filters || [])
const actions = computed(() => schema.value?.actions || [])
const selectedRule = computed(() => model.value[selected.value] || null)
const conditionCount = computed(() => {
  const f = selectedRule.value?.filter
  return f?.typeKind === 'FilterCollection' && Array.isArray(f.children) ? f.children.length : 1
})

function newRule(): RuleDraft {
  return {
    name: '新规则',
    enabled: true,
    filter: { typeKind: 'AnyFilter' },
    actions: [{ typeKind: 'NoOpAction' }],
  }
}

async function load() {
  loading.value = true
  try {
    const [s, sc] = await Promise.all([
      api.get<RulesStatus>('/rules'),
      api.get<RuleSchema>('/rules/schema'),
    ])
    status.value = s
    schema.value = sc
    yamlText.value = s.yaml
    model.value = (s.summary || []).map((r) => ({
      name: r.name || '',
      enabled: true,
      filter: { ...r.filter },
      actions: (r.actions || []).map((a) => ({ ...a })),
    }))
    if (!model.value.length) model.value = [newRule()]
    selected.value = Math.min(selected.value, model.value.length - 1)
    if (selected.value < 0) selected.value = 0
    mode.value = s.parseError ? 'yaml' : 'visual'
    dirty.value = false
    validation.value = null
    loaded.value = true
  } catch (e: any) {
    loaded.value = false
    toast.error('读取规则失败', e?.message)
  } finally {
    loading.value = false
  }
}

onMounted(load)

function typeLabel(kind: string, kindType: 'filter' | 'action') {
  const list = kindType === 'filter' ? filters.value : actions.value
  return list.find((t) => t.typeKind === kind)?.label || kind || '未选择'
}

function maskedName(r: RuleDraft, i: number) {
  return r.name?.trim() || `规则 #${i + 1}`
}

function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v))
}

function updateSelected(patch: Partial<RuleDraft>) {
  if (!selectedRule.value) return
  const next = [...model.value]
  next[selected.value] = { ...next[selected.value], ...patch }
  model.value = next
  dirty.value = true
  validation.value = null
}

function updateFilter(v: Record<string, any>) {
  updateSelected({ filter: v })
}

function isFilterCollection(filter: Record<string, any> | null | undefined): boolean {
  return filter?.typeKind === 'FilterCollection'
}

function newCondition() {
  return {
    typeKind: 'HostFilter',
    pattern: '',
    operation: 'Exact',
    caseSensitive: false,
    inverted: false,
  }
}

function enableMultiCondition() {
  if (!selectedRule.value || isFilterCollection(selectedRule.value.filter)) return
  const current = selectedRule.value.filter
  const children = current && current.typeKind && current.typeKind !== 'AnyFilter'
    ? [JSON.parse(JSON.stringify(current)), newCondition()]
    : [newCondition()]
  updateFilter({ typeKind: 'FilterCollection', operation: 'And', children })
}
function updateAction(i: number, v: Record<string, any>) {
  if (!selectedRule.value) return
  const list = [...selectedRule.value.actions]
  list[i] = v
  updateSelected({ actions: list })
}
function addRule() {
  const r = newRule()
  model.value = [...model.value, r]
  selected.value = model.value.length - 1
  dirty.value = true
  validation.value = null
}
function duplicateRule() {
  if (!selectedRule.value) return
  const r = clone(selectedRule.value)
  r.name = `${r.name || '规则'} 副本`
  model.value = [...model.value, r]
  selected.value = model.value.length - 1
  dirty.value = true
  validation.value = null
}
function deleteRule(i: number) {
  if (model.value.length <= 1) {
    toast.warning('至少保留一条规则', '如果不想拦截，可以保留 NoOpAction 占位规则')
    return
  }
  model.value = model.value.filter((_, idx) => idx !== i)
  selected.value = Math.max(0, Math.min(selected.value, model.value.length - 1))
  dirty.value = true
  validation.value = null
}
function moveRule(i: number, delta: number) {
  const j = i + delta
  if (j < 0 || j >= model.value.length) return
  const next = [...model.value]
  ;[next[i], next[j]] = [next[j], next[i]]
  model.value = next
  selected.value = j
  dirty.value = true
  validation.value = null
}
function addAction(kind: string) {
  if (!selectedRule.value || !kind) return
  const def = actions.value.find((a) => a.typeKind === kind)
  const a: Record<string, any> = { typeKind: kind }
  for (const f of def?.fields || []) {
    if (f.default !== undefined) setPath(a, f.path, f.default)
  }
  updateSelected({ actions: [...selectedRule.value.actions, a] })
}
function removeAction(i: number) {
  if (!selectedRule.value) return
  updateSelected({ actions: selectedRule.value.actions.filter((_, idx) => idx !== i) })
}
function moveAction(i: number, delta: number) {
  if (!selectedRule.value) return
  const j = i + delta
  if (j < 0 || j >= selectedRule.value.actions.length) return
  const list = [...selectedRule.value.actions]
  ;[list[i], list[j]] = [list[j], list[i]]
  updateSelected({ actions: list })
}
function setPath(obj: any, path: string, value: any) {
  const parts = path.split('.')
  let cur = obj
  for (let i = 0; i < parts.length - 1; i++) {
    if (cur[parts[i]] == null || typeof cur[parts[i]] !== 'object') cur[parts[i]] = {}
    cur = cur[parts[i]]
  }
  cur[parts[parts.length - 1]] = value
}

async function renderYaml(): Promise<string> {
  const r = await api.post<{ yaml: string }>('/rules/render', { rules: model.value })
  return r.yaml
}

async function preview() {
  try {
    previewYaml.value = mode.value === 'yaml' ? yamlText.value : await renderYaml()
    mode.value = 'preview'
  } catch (e: any) {
    toast.error('生成 YAML 失败', e?.message)
  }
}

async function validate() {
  validating.value = true
  validation.value = null
  try {
    const yaml =
      mode.value === 'yaml'
        ? yamlText.value
        : mode.value === 'preview'
          ? previewYaml.value
          : await renderYaml()
    const r = await api.post<ValidationResult>('/rules/validate', { yaml }, { timeoutMs: 30_000 })
    validation.value = r
    if (r.valid) toast.success(r.skipped ? '结构校验通过（已跳过运行时校验）' : '规则校验通过')
    else toast.error('规则校验未通过', r.errors?.[0] || '请查看校验结果')
  } catch (e: any) {
    toast.error('校验失败', e?.message)
  } finally {
    validating.value = false
  }
}

function openSave() {
  if (loading.value || !loaded.value) {
    toast.error('规则尚未加载', '请等待规则读取完成后再保存')
    return
  }
  if (mode.value === 'preview') {
    toast.info('请先切回可视化或 YAML 模式再保存')
    return
  }
  note.value = ''
  saveOpen.value = true
}

async function doSave(applyAfter = false) {
  if (loading.value || !loaded.value) {
    toast.error('规则尚未加载', '请等待规则读取完成后再保存')
    return
  }
  saving.value = true
  try {
    const yaml = mode.value === 'yaml' ? yamlText.value : await renderYaml()
    const r = await api.post<any>('/rules/save', { yaml, note: note.value }, { timeoutMs: 40_000 })
    validation.value = r.validation || null
    yamlText.value = yaml
    if (status.value) {
      status.value = {
        ...status.value,
        yaml,
        hash: r.version?.yamlHash || status.value.hash,
        pending: true,
        versions: r.version ? [r.version, ...status.value.versions] : status.value.versions,
      }
    }
    dirty.value = false
    saveOpen.value = false
    applyMessage.value = r.message || '规则已保存，尚未应用。'
    applyResult.value = null
    applyOpen.value = true
    if (applyAfter) {
      await applyRules(r.version?.yamlHash || status.value?.hash)
    }
  } catch (e: any) {
    const details = e?.details?.validation || e?.details
    if (details) validation.value = details
    toast.error('保存失败', e?.message)
  } finally {
    saving.value = false
  }
}

async function applyRules(hash?: string) {
  if (applying.value) return
  if (dirty.value && !window.confirm('当前有未保存的修改。应用会使用已保存的规则，并丢弃本地修改。继续？')) {
    return
  }
  applying.value = true
  applyMessage.value = '正在校验规则并热更新…'
  applyResult.value = null
  applyOpen.value = true
  try {
    const r = await api.post<any>('/rules/apply', { expectedHash: hash || status.value?.hash || '' }, { timeoutMs: 90_000 })
    applyResult.value = r
    applyMessage.value = r.message || '规则已应用。'
    toast.success('规则已应用', r.hotReloaded ? '规则已热更新' : r.unchanged ? '规则已是当前版本' : (r.activeSessionId ? `规则已应用，新会话 ${r.activeSessionId}` : '规则已应用，fluxzy 已重启'))
    await load()
  } catch (e: any) {
    applyResult.value = e?.details?.result || null
    applyMessage.value = e?.message || '应用失败'
    toast.error('应用失败', e?.message)
    await load()
  } finally {
    applying.value = false
  }
}

async function switchToVisual() {
  try {
    const r = await api.post<RuleModel>('/rules/parse', { yaml: yamlText.value })
    model.value = r.rules?.length ? r.rules : [newRule()]
    selected.value = 0
    mode.value = 'visual'
    dirty.value = true
  } catch (e: any) {
    toast.error('YAML 无法转换为可视化模型', e?.message)
  }
}

async function switchMode(next: 'visual' | 'yaml' | 'preview') {
  if (next === mode.value) return
  if (next === 'yaml' && mode.value === 'visual') {
    try {
      const yaml = await renderYaml()
      yamlText.value = yaml
    } catch (e: any) {
      toast.error('生成 YAML 失败', e?.message)
      return
    }
  }
  if (next === 'visual' && mode.value === 'preview') {
    try {
      const r = await api.post<RuleModel>('/rules/parse', { yaml: previewYaml.value || yamlText.value })
      model.value = r.rules?.length ? r.rules : [newRule()]
      selected.value = 0
      mode.value = 'visual'
      dirty.value = true
    } catch (e: any) {
      toast.error('预览 YAML 无法转换为可视化模型', e?.message)
    }
    return
  }
  if (next === 'visual' && mode.value === 'yaml') {
    await switchToVisual()
    return
  }
  if (next === 'yaml' && mode.value === 'preview' && previewYaml.value) {
    yamlText.value = previewYaml.value
  }
  if (next === 'preview') {
    await preview()
    return
  }
  mode.value = next
}

async function openVersion(v: ConfigVersion) {
  try {
    versionPreview.value = await api.get<ConfigVersion>(`/rules/versions/${v.id}`)
  } catch (e: any) {
    toast.error('读取版本失败', e?.message)
  }
}

async function rollback() {
  if (!rollbackTarget.value) return
  try {
    const r = await api.post<any>('/rules/rollback', { versionId: rollbackTarget.value.id }, { timeoutMs: 40_000 })
    toast.success('已写入回滚版本')
    rollbackOpen.value = false
    rollbackTarget.value = null
    versionsOpen.value = false
    await load()
    applyMessage.value = r.message || '回滚版本已保存，尚未应用。请在规则页点击“立即应用”。'
    applyOpen.value = true
  } catch (e: any) {
    const details = e?.details?.validation || e?.details
    if (details) validation.value = details
    toast.error('回滚失败', e?.message)
  }
}

interface Template {
  name: string
  description: string
  rule: RuleDraft
}
const templates: Template[] = [
  {
    name: '封禁指定域名',
    description: '匹配 Host 后直接中断连接，适合拦截广告/统计域名。',
    rule: {
      name: '封禁域名',
      enabled: true,
      filter: { typeKind: 'HostFilter', pattern: 'ads.example.com', operation: 'Exact' },
      actions: [{ typeKind: 'AbortAction' }],
    },
  },
  {
    name: '添加安全响应头',
    description: '给所有响应追加一组安全 Header。',
    rule: {
      name: '安全响应头',
      enabled: true,
      filter: { typeKind: 'AnyFilter' },
      actions: [
        { typeKind: 'AddResponseHeaderAction', headerName: 'x-content-type-options', headerValue: 'nosniff' },
        { typeKind: 'AddResponseHeaderAction', headerName: 'referrer-policy', headerValue: 'strict-origin-when-cross-origin' },
      ],
    },
  },
  {
    name: 'Mock API 响应',
    description: '匹配路径后直接返回 JSON，不访问上游。',
    rule: {
      name: 'Mock API',
      enabled: true,
      filter: { typeKind: 'PathFilter', pattern: '/api/mock', operation: 'Contains' },
      actions: [
        {
          typeKind: 'MockedResponseAction',
          response: {
            statusCode: 200,
            headers: [{ name: 'content-type', value: 'application/json' }],
            body: { origin: 'FromString', type: 'Json', text: '{"ok": true}' },
          },
        },
      ],
    },
  },
  {
    name: '转发到新地址',
    description: '把匹配的请求转发到另一个 URL。',
    rule: {
      name: '转发',
      enabled: true,
      filter: { typeKind: 'HostFilter', pattern: 'old.example.com', operation: 'Exact' },
      actions: [{ typeKind: 'ForwardAction', url: 'https://new.example.com' }],
    },
  },
  {
    name: '拦截指定路径',
    description: '命中路径后返回 403。',
    rule: {
      name: '拦截路径',
      enabled: true,
      filter: { typeKind: 'PathFilter', pattern: '/admin', operation: 'StartsWith' },
      actions: [{ typeKind: 'RejectWithStatusCodeAction', statusCode: 403 }],
    },
  },
  {
    name: '多条件组合',
    description: '域名 + 路径 + HTTP 方法同时匹配；可继续添加嵌套分组。',
    rule: {
      name: '多条件组合',
      enabled: true,
      filter: {
        typeKind: 'FilterCollection',
        operation: 'And',
        children: [
          { typeKind: 'HostFilter', pattern: 'api.example.com', operation: 'Exact', caseSensitive: false },
          { typeKind: 'PathFilter', pattern: '/v1', operation: 'StartsWith', caseSensitive: false },
          { typeKind: 'MethodFilter', pattern: 'POST', operation: 'Exact', caseSensitive: true },
        ],
      },
      actions: [{ typeKind: 'NoOpAction' }],
    },
  },
  {
    name: '改写指定路径响应体',
    description: '按 Host + Path 匹配后替换上游响应体。',
    rule: {
      name: '改写响应体',
      enabled: true,
      filter: {
        typeKind: 'FilterCollection',
        operation: 'And',
        children: [
          { typeKind: 'HostFilter', pattern: 'api.example.com', operation: 'Exact', caseSensitive: false },
          { typeKind: 'PathFilter', pattern: '/v1/users', operation: 'StartsWith', caseSensitive: false },
        ],
      },
      actions: [{
        typeKind: 'ConsoleTransformResponseBodyAction',
        operation: 'Replace',
        pattern: 'old-value',
        replacement: 'new-value',
        caseSensitive: true,
        maxBodyBytes: 8388608,
      }],
    },
  },
  {
    name: '改写指定路径请求体',
    description: '按路径匹配后替换请求体；超过大小限制时保持原 body。',
    rule: {
      name: '改写请求体',
      enabled: true,
      filter: { typeKind: 'PathFilter', pattern: '/v1/orders', operation: 'StartsWith' },
      actions: [{
        typeKind: 'ConsoleTransformRequestBodyAction',
        operation: 'RegexReplace',
        pattern: String.raw`"status"\s*:\s*"[^"]*"`,
        replacement: '"status":"debug"',
        maxBodyBytes: 8388608,
      }],
    },
  },
]

function applyTemplate(t: Template) {
  const r = clone(t.rule)
  model.value = [...model.value, r]
  selected.value = model.value.length - 1
  templatesOpen.value = false
  mode.value = 'visual'
  dirty.value = true
  validation.value = null
}

const activeSession = computed(() => status.value?.activeSessionId || '')
</script>

<template>
  <div class="flex h-[calc(100dvh-6.375rem)] min-h-0 flex-col gap-4 lg:h-[calc(100dvh-6.875rem)]">
    <!-- header -->
    <section class="panel shrink-0 p-4">
      <div class="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="text-[16px] font-semibold tracking-tight text-ink">规则管理</h2>
            <Badge v-if="status?.pending" variant="warn" dot>待应用</Badge>
            <Badge v-else-if="status" variant="ok" dot>已生效</Badge>
            <Badge v-if="dirty" variant="accent">未保存</Badge>
            <Badge v-if="status?.isDefault" variant="muted">默认规则</Badge>
          </div>
          <div class="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted">
            <span>{{ model.length }} 条规则</span>
            <span v-if="activeSession" class="font-mono">生效会话 {{ activeSession }}</span>
            <span v-if="status?.modifiedAt">修改于 {{ formatRelative(status.modifiedAt) }}</span>
          </div>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <div class="flex rounded-xl border border-line/70 bg-panel-strong/50 p-0.5">
            <button
              class="rounded-lg px-3 py-1.5 text-[11.5px] font-medium transition"
              :class="mode === 'visual' ? 'bg-panel text-ink shadow-sm' : 'text-muted hover:text-ink'"
              @click="switchMode('visual')"
            >
              <Wand2 class="mr-1 inline h-3.5 w-3.5" /> 可视化
            </button>
            <button
              class="rounded-lg px-3 py-1.5 text-[11.5px] font-medium transition"
              :class="mode === 'yaml' ? 'bg-panel text-ink shadow-sm' : 'text-muted hover:text-ink'"
              @click="switchMode('yaml')"
            >
              <Code2 class="mr-1 inline h-3.5 w-3.5" /> YAML
            </button>
            <button
              class="rounded-lg px-3 py-1.5 text-[11.5px] font-medium transition"
              :class="mode === 'preview' ? 'bg-panel text-ink shadow-sm' : 'text-muted hover:text-ink'"
              @click="switchMode('preview')"
            >
              <Eye class="mr-1 inline h-3.5 w-3.5" /> 预览
            </button>
          </div>
          <Button variant="ghost" @click="templatesOpen = true">
            <template #icon><LayoutTemplate class="h-4 w-4" /></template>
            模板
          </Button>
          <Button variant="ghost" @click="versionsOpen = true">
            <template #icon><History class="h-4 w-4" /></template>
            版本
          </Button>
          <Button :loading="validating" :disabled="!auth.isOperator" @click="validate">
            <template #icon><ShieldCheck class="h-4 w-4" /></template>
            校验
          </Button>
          <Button variant="primary" :disabled="mode === 'preview' || !auth.isAdmin || !loaded || loading" @click="openSave">
            <template #icon><Save class="h-4 w-4" /></template>
            保存
          </Button>
        </div>
      </div>
    </section>

    <div
      v-if="status?.pending"
      class="flex shrink-0 flex-wrap items-center gap-3 rounded-2xl border border-warn/25 bg-warn/8 px-4 py-3 text-[12px] text-warn"
    >
      <AlertTriangle class="h-4 w-4" />
      <span class="flex-1">规则文件已修改，尚未生效。应用后热更新到运行中的 fluxzy。</span>
      <Button size="sm" variant="primary" :loading="applying" :disabled="!auth.isAdmin" @click="applyRules()">立即应用</Button>
    </div>

    <!-- validation result -->
    <section v-if="validation" class="panel max-h-52 shrink-0 overflow-auto p-4">
      <div class="flex items-start gap-3">
        <component
          :is="validation.valid ? CheckCircle2 : XCircle"
          class="mt-0.5 h-5 w-5 shrink-0"
          :class="validation.valid ? 'text-ok' : 'text-bad'"
        />
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-[13px] font-semibold text-ink">{{ validation.valid ? '校验通过' : '校验未通过' }}</span>
            <Badge variant="muted">{{ validation.durationMs }} ms</Badge>
            <Badge v-if="validation.skipped" variant="warn">已跳过 CLI 校验</Badge>
          </div>
          <div v-if="validation.errors?.length" class="mt-2 space-y-1">
            <div v-for="(e, i) in validation.errors" :key="i" class="rounded-lg border border-bad/20 bg-bad/6 px-3 py-2 font-mono text-[11px] text-bad">{{ e }}</div>
          </div>
          <div v-if="validation.warnings?.length" class="mt-2 space-y-1">
            <div v-for="(w, i) in validation.warnings" :key="i" class="rounded-lg border border-warn/20 bg-warn/6 px-3 py-2 text-[11px] text-warn">{{ w }}</div>
          </div>
          <details v-if="validation.output" class="mt-2">
            <summary class="cursor-pointer text-[11px] text-muted hover:text-ink">查看 CLI 输出</summary>
            <pre class="mt-2 max-h-56 overflow-auto rounded-lg bg-bg-soft p-3 font-mono text-[10.5px] leading-relaxed text-muted">{{ validation.output }}</pre>
          </details>
        </div>
        <button class="btn-ghost h-7 px-2 text-[11px]" @click="validation = null">关闭</button>
      </div>
    </section>

    <div v-if="loading" class="panel flex flex-1 items-center justify-center p-10 text-center text-[12px] text-muted">加载规则中…</div>

    <template v-else>
      <!-- YAML mode -->
      <section v-if="mode === 'yaml'" class="panel flex min-h-0 flex-1 flex-col p-3">
        <div class="mb-2 flex shrink-0 items-center justify-between">
          <div class="text-[11.5px] text-muted">直接编辑 fluxzy 原生 YAML；保存前会做语法与 CLI 校验。</div>
          <div class="flex items-center gap-1">
            <Button size="sm" variant="ghost" @click="switchToVisual">
              <template #icon><Wand2 class="h-3.5 w-3.5" /></template>
              转为可视化
            </Button>
            <CopyButton :text="yamlText" small label="复制 YAML" />
          </div>
        </div>
        <YamlEditor v-model="yamlText" fill class="min-h-0 flex-1" @update:model-value="dirty = true" />
      </section>

      <!-- preview mode -->
      <section v-else-if="mode === 'preview'" class="panel flex min-h-0 flex-1 flex-col p-4">
        <div class="mb-3 flex shrink-0 items-center justify-between">
          <div>
            <div class="text-[13px] font-semibold text-ink">生成的 rules.yaml</div>
            <div class="mt-0.5 text-[11px] text-faint">保存后写入 /config/rules.yaml，应用时热更新。</div>
          </div>
          <CopyButton :text="previewYaml" label="复制" />
        </div>
        <CodeBlock :code="previewYaml" language="yaml" fill />
      </section>

      <!-- visual mode -->
      <section v-else class="grid min-h-0 flex-1 gap-4 overflow-y-auto xl:grid-cols-[330px_minmax(0,1fr)] xl:overflow-hidden">
        <div class="panel flex min-h-[320px] flex-col overflow-hidden xl:h-full xl:min-h-0">
          <div class="flex items-center justify-between border-b border-line/50 px-3 py-2.5">
            <span class="text-[11.5px] font-semibold uppercase tracking-wider text-faint">Rules</span>
            <div class="flex items-center gap-1">
              <button class="btn-ghost h-7 px-2 text-[11px]" @click="templatesOpen = true">
                <LayoutTemplate class="h-3.5 w-3.5" />
              </button>
              <Button size="sm" variant="ghost" @click="addRule">
                <template #icon><Plus class="h-3.5 w-3.5" /></template>
                新增
              </Button>
            </div>
          </div>
          <div class="min-h-0 flex-1 overflow-y-auto p-2">
            <div
              v-for="(r, i) in model"
              :key="i"
              class="mb-1.5 cursor-pointer rounded-xl border p-2.5 transition"
              :class="selected === i ? 'border-accent/40 bg-accent/8' : 'border-transparent hover:border-line/60 hover:bg-panel-strong/40'"
              @click="selected = i"
            >
              <div class="flex items-start gap-2">
                <span class="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-md bg-panel-strong font-mono text-[10px] text-faint">{{ i + 1 }}</span>
                <div class="min-w-0 flex-1">
                  <div class="truncate text-[12px] font-medium text-ink">{{ maskedName(r, i) }}</div>
                  <div class="mt-0.5 truncate text-[10px] text-faint">
                    {{ typeLabel(r.filter?.typeKind || '', 'filter') }} → {{ r.actions.length }} 个动作
                  </div>
                </div>
              </div>
            </div>
            <EmptyState v-if="!model.length" title="还没有规则" description="点击新增或从模板开始。" />
          </div>
        </div>

        <div v-if="selectedRule" class="min-h-0 space-y-4 xl:h-full xl:overflow-y-auto xl:pr-1">
          <Card>
            <div class="flex flex-col gap-3 sm:flex-row sm:items-end">
              <div class="flex-1">
                <Input
                  :model-value="selectedRule.name"
                  label="规则名称"
                  placeholder="例如：封禁广告域名"
                  @update:model-value="updateSelected({ name: String($event) })"
                />
              </div>
              <div class="flex items-center gap-2">
                <Button size="sm" variant="ghost" @click="moveRule(selected, -1)">
                  <template #icon><ArrowUp class="h-3.5 w-3.5" /></template>
                </Button>
                <Button size="sm" variant="ghost" @click="moveRule(selected, 1)">
                  <template #icon><ArrowDown class="h-3.5 w-3.5" /></template>
                </Button>
                <Button size="sm" variant="ghost" @click="duplicateRule">
                  <template #icon><Copy class="h-3.5 w-3.5" /></template>
                </Button>
                <Button size="sm" variant="ghost" class="text-bad hover:text-bad" @click="deleteRule(selected)">
                  <template #icon><Trash2 class="h-3.5 w-3.5" /></template>
                </Button>
              </div>
            </div>
          </Card>

          <Card title="匹配条件" subtitle="支持多个条件、AND/OR 和嵌套分组">
            <template #actions>
              <Badge variant="muted">{{ conditionCount }} 个条件</Badge>
              <Button
                v-if="!isFilterCollection(selectedRule.filter)"
                size="sm"
                variant="primary"
                @click="enableMultiCondition"
              >
                <template #icon><Plus class="h-3.5 w-3.5" /></template>
                添加多条件
              </Button>
            </template>
            <FilterCollectionEditor
              v-if="isFilterCollection(selectedRule.filter)"
              :model-value="selectedRule.filter"
              :schema="filters"
              @update:model-value="updateFilter"
            />
            <TypeForm
              v-else
              kind="filter"
              :model-value="selectedRule.filter"
              :schema="filters"
              label="Filter 类型"
              @update:model-value="updateFilter"
            />
          </Card>

          <Card title="执行动作" subtitle="按顺序执行；可添加多个动作">
            <template #actions>
              <span class="badge-muted">{{ selectedRule.actions.length }}</span>
            </template>
            <div class="space-y-3">
              <div
                v-for="(a, i) in selectedRule.actions"
                :key="i"
                class="rounded-xl border border-line/50 bg-panel-strong/30 p-3"
              >
                <div class="mb-2 flex items-center justify-between">
                  <span class="text-[11px] font-semibold uppercase tracking-wider text-faint">Action #{{ i + 1 }}</span>
                  <div class="flex items-center gap-1">
                    <button class="btn-ghost h-6 w-6 px-0" @click="moveAction(i, -1)"><ArrowUp class="h-3 w-3" /></button>
                    <button class="btn-ghost h-6 w-6 px-0" @click="moveAction(i, 1)"><ArrowDown class="h-3 w-3" /></button>
                    <button class="btn-ghost h-6 w-6 px-0 text-faint hover:text-bad" @click="removeAction(i)"><Trash2 class="h-3 w-3" /></button>
                  </div>
                </div>
                <TypeForm
                  kind="action"
                  :model-value="a"
                  :schema="actions"
                  @update:model-value="updateAction(i, $event)"
                />
              </div>

              <div class="flex items-center gap-2 rounded-xl border border-dashed border-line/70 p-2.5">
                <Plus class="h-4 w-4 text-faint" />
                <select
                  class="select h-8 flex-1 py-1 text-[11.5px]"
                  @change="addAction(($event.target as HTMLSelectElement).value); ($event.target as HTMLSelectElement).value = ''"
                >
                  <option value="">添加动作…</option>
                  <optgroup v-for="cat in [...new Set(actions.map((a) => a.category))]" :key="cat" :label="cat">
                    <option v-for="a in actions.filter((x) => x.category === cat)" :key="a.typeKind" :value="a.typeKind">{{ a.label }}</option>
                  </optgroup>
                </select>
              </div>
            </div>
          </Card>
        </div>

        <div v-else class="panel flex min-h-[240px] items-center justify-center p-8 xl:h-full xl:min-h-0">
          <EmptyState title="选择一条规则" description="从左侧选择规则进行编辑，或新增一条。" />
        </div>
      </section>
    </template>

    <!-- save modal -->
    <Modal v-model:open="saveOpen" title="保存规则" width="520px">
      <div class="space-y-4">
        <div class="rounded-xl border border-line/60 bg-panel-strong/40 p-3 text-[12px] text-muted">
          将写入 <code>/config/rules.yaml</code> 并生成一个新版本。保存后点击应用，规则会热更新到运行中的 fluxzy。
        </div>
        <div class="flex items-center gap-4 text-[11.5px] text-muted">
          <span>规则总数 <b class="font-mono text-ink">{{ model.length }}</b></span>
        </div>
        <Textarea v-model="note" label="变更说明（可选）" :rows="3" :monospace="false" placeholder="例如：封禁广告域名，给所有响应加安全头" />
      </div>
      <template #footer>
        <Button variant="ghost" @click="saveOpen = false">取消</Button>
        <Button :loading="saving" :disabled="!auth.isAdmin" @click="doSave(false)">仅保存</Button>
        <Button variant="primary" :loading="saving" :disabled="!auth.isAdmin" @click="doSave(true)">
          <template #icon><Save class="h-4 w-4" /></template>
          保存并应用
        </Button>
      </template>
    </Modal>

    <!-- apply modal -->
    <Modal v-model:open="applyOpen" title="应用规则" width="560px" :close-on-backdrop="!applying" :show-close="!applying">
      <div class="space-y-4">
        <div
          class="flex items-start gap-3 rounded-xl border p-3.5"
          :class="applyResult?.applied ? 'border-ok/25 bg-ok/8' : applyResult ? 'border-bad/25 bg-bad/8' : 'border-warn/25 bg-warn/8'"
        >
          <component :is="applyResult?.applied ? CheckCircle2 : applyResult ? XCircle : AlertTriangle" class="mt-0.5 h-5 w-5 shrink-0" :class="applyResult?.applied ? 'text-ok' : applyResult ? 'text-bad' : 'text-warn'" />
          <div class="min-w-0 text-[12px] leading-relaxed text-muted">
            <div class="font-medium text-ink">{{ applyMessage }}</div>
            <div v-if="applyResult?.hotReloaded" class="mt-1 text-[11px] text-ok">新规则已生效。</div>
            <div v-else-if="applyResult?.activeSessionId" class="mt-1 font-mono text-[11px] text-faint">新会话：{{ applyResult.activeSessionId }}</div>
            <div v-if="applyResult?.rolledBack" class="mt-1 text-[11px] text-warn">已自动恢复上一版规则并重启。</div>
            <div v-if="applyResult?.hotReloaded" class="mt-1 text-faint">热更新只影响应用之后开始的请求，在途请求保持原规则。</div>
            <div v-else-if="applyResult" class="mt-1 text-faint">fluxzy 已重启，在途请求已中断。</div>
          </div>
        </div>
        <div v-if="applying" class="h-2 overflow-hidden rounded-full bg-panel-strong">
          <div class="h-full w-1/2 animate-pulse rounded-full bg-accent" />
        </div>
        <p class="text-[11px] leading-relaxed text-faint">
          应用后规则会热更新到运行中的 fluxzy。
        </p>
      </div>
      <template #footer>
        <Button variant="ghost" :disabled="applying" @click="applyOpen = false">关闭</Button>
        <Button v-if="!applyResult && !applying" variant="primary" @click="applyRules()">应用规则</Button>
      </template>
    </Modal>

    <!-- templates modal -->
    <Modal v-model:open="templatesOpen" title="规则模板" width="720px">
      <div class="grid gap-2 sm:grid-cols-2">
        <button
          v-for="t in templates"
          :key="t.name"
          class="rounded-xl border border-line/60 bg-panel-strong/35 p-3.5 text-left transition hover:border-accent/40 hover:bg-accent/5"
          @click="applyTemplate(t)"
        >
          <div class="text-[12.5px] font-semibold text-ink">{{ t.name }}</div>
          <div class="mt-1 text-[11px] leading-relaxed text-muted">{{ t.description }}</div>
          <div class="mt-2 flex items-center gap-1 text-[10px] text-accent">
            <Plus class="h-3 w-3" /> 添加此模板
          </div>
        </button>
      </div>
    </Modal>

    <!-- versions modal -->
    <Modal v-model:open="versionsOpen" title="规则版本历史" width="880px">
      <div class="grid gap-3 lg:grid-cols-[1fr_1.2fr]">
        <div class="max-h-[480px] space-y-2 overflow-y-auto pr-1">
          <button
            v-for="v in status?.versions || []"
            :key="v.id"
            class="w-full rounded-xl border p-3 text-left transition"
            :class="versionPreview?.id === v.id ? 'border-accent/40 bg-accent/8' : 'border-line/60 bg-panel-strong/30 hover:border-accent/30'"
            @click="openVersion(v)"
          >
            <div class="flex items-center justify-between gap-2">
              <span class="font-mono text-[11.5px] text-ink">#{{ v.id }}</span>
              <Badge :variant="v.status === 'active' ? 'ok' : v.status === 'pending' ? 'warn' : 'muted'">{{ v.status }}</Badge>
            </div>
            <div class="mt-1 text-[10.5px] text-faint">{{ formatDateTime(v.createdAt) }} · {{ v.authorName || 'system' }}</div>
            <div v-if="v.note" class="mt-1 truncate text-[11px] text-muted">{{ v.note }}</div>
            <div class="mt-1 truncate font-mono text-[10px] text-faint">{{ v.yamlHash.slice(0, 16) }}…</div>
          </button>
          <EmptyState v-if="!status?.versions?.length" title="还没有版本记录" description="保存一次规则后会生成第一个版本。" />
        </div>
        <div class="min-h-[200px] rounded-xl border border-line/50 bg-bg-soft p-2">
          <template v-if="versionPreview">
            <div class="mb-2 flex items-center justify-between px-1">
              <span class="text-[11.5px] text-muted">版本 #{{ versionPreview.id }}</span>
              <Button
                v-if="auth.isAdmin"
                size="sm"
                variant="danger"
                @click="rollbackTarget = versionPreview; rollbackOpen = true"
              >
                <template #icon><RotateCcw class="h-3.5 w-3.5" /></template>
                回滚到此版本
              </Button>
            </div>
            <CodeBlock :code="versionPreview.yaml || ''" language="yaml" max-height="400px" />
          </template>
          <EmptyState v-else title="选择版本" description="点击左侧版本查看 YAML 内容。" />
        </div>
      </div>
    </Modal>

    <!-- rollback confirm -->
    <Modal v-model:open="rollbackOpen" title="确认回滚" width="460px">
      <div v-if="rollbackTarget" class="space-y-3 text-[12px] text-muted">
        <div class="rounded-xl border border-warn/25 bg-warn/8 p-3 text-warn">
          将把版本 <b class="font-mono">#{{ rollbackTarget.id }}</b> 的 YAML 写回 <code>/config/rules.yaml</code>，并生成一个新版本。
        </div>
        <p>回滚只会生成新版本；点击“立即应用”后热更新生效。</p>
      </div>
      <template #footer>
        <Button variant="ghost" @click="rollbackOpen = false">取消</Button>
        <Button variant="danger" @click="rollback">确认回滚</Button>
      </template>
    </Modal>
  </div>
</template>

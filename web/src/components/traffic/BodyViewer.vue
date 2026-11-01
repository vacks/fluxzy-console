<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { AlertCircle, Braces, Download, FileQuestion, Text } from 'lucide-vue-next'
import JsonTree from '@/components/ui/JsonTree.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import CopyButton from '@/components/ui/CopyButton.vue'
import Spinner from '@/components/ui/Spinner.vue'
import { formatBytes } from '@/utils/format'

const props = defineProps<{
  sessionId: string
  exchangeId: number
  kind: 'request' | 'response'
  contentType?: string
  exists: boolean
  size: number
}>()

const loading = ref(false)
const error = ref('')
const text = ref('')
const binary = ref(false)
const truncated = ref(false)
const tooLarge = ref(false)
const wasDecoded = ref(false)
const bodyEncoding = ref('')
const view = ref<'pretty' | 'raw'>('pretty')
const json = ref<unknown | null>(null)
const isImage = computed(() => (props.contentType || '').startsWith('image/'))
const hasJson = computed(() => json.value !== null)

const rawUrl = computed(
  () =>
    `/api/v1/capture/sessions/${encodeURIComponent(props.sessionId)}/exchanges/${props.exchangeId}/body?kind=${props.kind}&format=raw`,
)
const textUrl = computed(() => rawUrl.value.replace('format=raw', 'format=text'))

async function load() {
  if (!props.exists) return
  if (isImage.value) return
  loading.value = true
  error.value = ''
  text.value = ''
  binary.value = false
  tooLarge.value = false
  wasDecoded.value = false
  bodyEncoding.value = ''
  json.value = null
  try {
    const res = await fetch(textUrl.value, { credentials: 'include' })
    if (res.status === 415) {
      binary.value = true
      return
    }
    if (!res.ok) {
      let msg = `读取失败 (${res.status})`
      try {
        const j = await res.json()
        msg = j?.error?.message || msg
      } catch {
        /* ignore */
      }
      error.value = msg
      return
    }
    wasDecoded.value = res.headers.get('X-Body-Decoded') === 'true'
    bodyEncoding.value = res.headers.get('X-Body-Content-Encoding') || ''
    const maxPreview = 2 << 20
    const declared = Number(res.headers.get('Content-Length') || 0)
    const blob = await res.blob()
    if (declared > maxPreview || blob.size > maxPreview) {
      tooLarge.value = true
      return
    }
    text.value = await blob.text()
    truncated.value = text.value.includes('... 已截断，完整大小')
    const trimmed = text.value.trim()
    if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
      try {
        json.value = JSON.parse(trimmed.replace(/\n\n\.\.\. 已截断.*$/s, ''))
      } catch {
        json.value = null
      }
    }
  } catch (e: any) {
    error.value = e?.message || '读取失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => [props.exchangeId, props.kind], load)
</script>

<template>
  <div v-if="!exists" class="rounded-xl border border-line/50 bg-panel-strong/30">
    <EmptyState title="没有 body" description="该请求/响应没有记录到 body。" />
  </div>

  <div v-else class="space-y-2">
    <div class="flex flex-wrap items-center gap-2">
      <div class="badge-muted">{{ formatBytes(size) }}</div>
      <div class="badge-muted">{{ contentType || 'unknown' }}</div>
      <span v-if="wasDecoded && bodyEncoding" class="badge-muted">{{ bodyEncoding }} 已解压</span>
      <span v-if="truncated" class="badge-warn">预览已截断</span>
      <div class="ml-auto flex items-center gap-1">
        <button
          v-if="hasJson"
          class="btn h-7 px-2 text-[11px]"
          :class="view === 'pretty' ? 'border-accent/40 bg-accent/10 text-accent' : 'border-line/70 bg-panel-strong/60 text-muted'"
          @click="view = 'pretty'"
        >
          <Braces class="h-3.5 w-3.5" /> 结构化
        </button>
        <button
          v-if="hasJson"
          class="btn h-7 px-2 text-[11px]"
          :class="view === 'raw' ? 'border-accent/40 bg-accent/10 text-accent' : 'border-line/70 bg-panel-strong/60 text-muted'"
          @click="view = 'raw'"
        >
          <Text class="h-3.5 w-3.5" /> 原始
        </button>
        <a :href="rawUrl" class="btn-secondary h-7 px-2 text-[11px]" download>
          <Download class="h-3.5 w-3.5" /> 下载
        </a>
      </div>
    </div>

    <div v-if="loading" class="flex items-center justify-center rounded-xl border border-line/50 bg-[rgb(var(--bg-soft))] py-16 text-muted">
      <Spinner class="mr-2 h-4 w-4" /> 读取 body…
    </div>

    <div v-else-if="error" class="flex items-start gap-2 rounded-xl border border-bad/30 bg-bad/8 p-4 text-[12px] text-bad">
      <AlertCircle class="mt-0.5 h-4 w-4 shrink-0" /> {{ error }}
    </div>

    <div v-else-if="isImage" class="overflow-hidden rounded-xl border border-line/50 bg-[rgb(var(--bg-soft))] p-3">
      <img :src="rawUrl" class="mx-auto max-h-[480px] max-w-full rounded-lg object-contain" alt="response body" />
    </div>

    <div v-else-if="tooLarge" class="rounded-xl border border-line/50 bg-[rgb(var(--bg-soft))]">
      <EmptyState title="内容过大" description="为保持页面流畅，超过 2 MiB 的 body 不在浏览器内预览。请下载后查看。">
        <template #icon><FileQuestion class="h-5 w-5" /></template>
        <template #action>
          <a :href="rawUrl" class="btn-secondary" download>
            <Download class="h-4 w-4" /> 下载 {{ formatBytes(size) }}
          </a>
        </template>
      </EmptyState>
    </div>

    <div v-else-if="binary" class="rounded-xl border border-line/50 bg-[rgb(var(--bg-soft))]">
      <EmptyState title="二进制内容" description="该内容不是文本，无法在浏览器中直接预览。请下载后使用对应工具查看。">
        <template #icon><FileQuestion class="h-5 w-5" /></template>
        <template #action>
          <a :href="rawUrl" class="btn-secondary" download>
            <Download class="h-4 w-4" /> 下载 {{ formatBytes(size) }}
          </a>
        </template>
      </EmptyState>
    </div>

    <template v-else>
      <div class="relative">
        <div class="absolute right-2 top-2 z-10">
          <CopyButton :text="text" small label="复制" />
        </div>
        <JsonTree v-if="hasJson && view === 'pretty'" :value="json" :expand-depth="3" />
        <pre
          v-else
          class="max-h-[560px] overflow-auto whitespace-pre-wrap break-all rounded-xl border border-line/50 bg-[rgb(var(--bg-soft))] p-3 font-mono text-[11.5px] leading-relaxed text-ink"
        >{{ text }}</pre>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { Eye, EyeOff, Search, ShieldOff } from 'lucide-vue-next'
import type { HeaderField } from '@/api/types'
import EmptyState from '@/components/ui/EmptyState.vue'
import CopyButton from '@/components/ui/CopyButton.vue'

const props = withDefaults(
  defineProps<{ headers: HeaderField[]; title?: string; showSearch?: boolean }>(),
  { title: '', showSearch: true },
)

const query = ref('')
const revealed = ref(false)

const sensitive = /^(authorization|proxy-authorization|cookie|set-cookie|x-api-key|api-key)$/i
const hasSensitive = computed(() => props.headers.some((h) => sensitive.test(h.name)))
const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return props.headers
  return props.headers.filter(
    (h) => h.name.toLowerCase().includes(q) || h.value.toLowerCase().includes(q),
  )
})
const allText = computed(() => props.headers.map((h) => `${h.name}: ${h.value}`).join('\n'))

function displayValue(h: HeaderField) {
  if (!revealed.value && sensitive.test(h.name)) return '••••••••••••••••'
  return h.value
}
</script>

<template>
  <div class="space-y-2">
    <div class="flex items-center justify-between gap-2">
      <div class="flex items-center gap-2">
        <span v-if="title" class="text-[11.5px] font-medium text-muted">{{ title }}</span>
        <span class="badge-muted">{{ headers.length }}</span>
      </div>
      <div class="flex items-center gap-1">
        <div v-if="showSearch" class="relative">
          <Search class="pointer-events-none absolute left-2.5 top-1/2 h-3 w-3 -translate-y-1/2 text-faint" />
          <input
            v-model="query"
            class="input h-7 w-[150px] py-1 pl-7 text-[11px]"
            placeholder="筛选 header"
          />
        </div>
        <button
          v-if="hasSensitive"
          class="btn-ghost h-7 px-2 text-[11px]"
          :title="revealed ? '隐藏敏感 header' : '显示敏感 header'"
          @click="revealed = !revealed"
        >
          <component :is="revealed ? EyeOff : Eye" class="h-3.5 w-3.5" />
          {{ revealed ? '隐藏敏感' : '显示敏感' }}
        </button>
        <CopyButton :text="allText" small label="复制全部" />
      </div>
    </div>

    <div v-if="filtered.length" class="overflow-hidden rounded-xl border border-line/50">
      <table class="w-full table-fixed border-collapse text-left">
        <colgroup>
          <col style="width: 30%" />
          <col style="width: 70%" />
        </colgroup>
        <tbody>
          <tr v-for="(h, i) in filtered" :key="`${h.name}-${i}`" class="border-b border-line/35 last:border-0">
            <td class="bg-panel-strong/45 px-3 py-2 align-top font-mono text-[11px] text-muted">
              <span class="flex items-start gap-1.5">
                <ShieldOff v-if="sensitive.test(h.name)" class="mt-0.5 h-3 w-3 shrink-0 text-warn" />
                <span class="break-all">{{ h.name }}</span>
              </span>
            </td>
            <td class="px-3 py-2 align-top">
              <div class="break-all font-mono text-[11.5px] text-ink">{{ displayValue(h) }}</div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <EmptyState v-else title="没有 header" description="该请求没有可显示的 header。" />
  </div>
</template>

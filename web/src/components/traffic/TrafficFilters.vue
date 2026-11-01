<script setup lang="ts">
import { Search, X } from 'lucide-vue-next'
import type { TrafficFilter } from './filters'

const model = defineModel<TrafficFilter>({ required: true })
const emit = defineEmits<{ search: [] }>()

function patch(part: Partial<TrafficFilter>) {
  model.value = { ...model.value, ...part }
}
function clear() {
  model.value = { q: '', method: '', statusClass: '', pending: false, errors: false }
  emit('search')
}
</script>

<template>
  <div class="flex flex-wrap items-center gap-2">
    <div class="relative min-w-[200px] flex-1">
      <Search class="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-faint" />
      <input
        :value="model.q"
        class="input py-2 pl-9 pr-8 text-[12.5px]"
        placeholder="搜索 URL / 域名 / 路径…"
        @input="patch({ q: ($event.target as HTMLInputElement).value })"
        @keyup.enter="emit('search')"
      />
      <button
        v-if="model.q"
        class="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-faint hover:text-ink"
        @click="patch({ q: '' }); emit('search')"
      >
        <X class="h-3.5 w-3.5" />
      </button>
    </div>

    <select
      :value="model.method"
      class="select w-[116px] py-2 text-[12px]"
      @change="patch({ method: ($event.target as HTMLSelectElement).value }); emit('search')"
    >
      <option value="">全部方法</option>
      <option v-for="m in ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS']" :key="m" :value="m">{{ m }}</option>
    </select>

    <select
      :value="model.statusClass"
      class="select w-[124px] py-2 text-[12px]"
      @change="patch({ statusClass: ($event.target as HTMLSelectElement).value }); emit('search')"
    >
      <option value="">全部状态</option>
      <option value="2xx">2xx 成功</option>
      <option value="3xx">3xx 重定向</option>
      <option value="4xx">4xx 客户端</option>
      <option value="5xx">5xx 服务端</option>
    </select>

    <button
      class="btn px-2.5 py-2 text-[11.5px]"
      :class="model.errors ? 'border-bad/40 bg-bad/10 text-bad' : 'border-line/70 bg-panel-strong/60 text-muted hover:text-ink'"
      @click="patch({ errors: !model.errors }); emit('search')"
    >
      仅错误
    </button>
    <button
      class="btn px-2.5 py-2 text-[11.5px]"
      :class="model.pending ? 'border-accent/40 bg-accent/10 text-accent' : 'border-line/70 bg-panel-strong/60 text-muted hover:text-ink'"
      @click="patch({ pending: !model.pending }); emit('search')"
    >
      进行中
    </button>
    <button class="btn-ghost px-2 py-2 text-[11.5px]" @click="clear">重置</button>
  </div>
</template>

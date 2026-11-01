<script setup lang="ts">
import { Plus, Trash2 } from 'lucide-vue-next'

const props = defineProps<{ modelValue: { name: string; value: string }[] | undefined }>()
const emit = defineEmits<{ 'update:modelValue': [{ name: string; value: string }[]] }>()

function rows() {
  return Array.isArray(props.modelValue) ? props.modelValue : []
}
function update(next: { name: string; value: string }[]) {
  emit('update:modelValue', next)
}
function set(i: number, key: 'name' | 'value', v: string) {
  const next = rows().map((r, idx) => (idx === i ? { ...r, [key]: v } : r))
  update(next)
}
function add() {
  update([...rows(), { name: '', value: '' }])
}
function remove(i: number) {
  update(rows().filter((_, idx) => idx !== i))
}
</script>

<template>
  <div class="space-y-2">
    <div v-for="(row, i) in rows()" :key="i" class="flex items-center gap-2">
      <input
        :value="row.name"
        class="input h-8 w-[38%] py-1 font-mono text-[11.5px]"
        placeholder="Header 名称"
        @input="set(i, 'name', ($event.target as HTMLInputElement).value)"
      />
      <input
        :value="row.value"
        class="input h-8 flex-1 py-1 font-mono text-[11.5px]"
        placeholder="Header 值"
        @input="set(i, 'value', ($event.target as HTMLInputElement).value)"
      />
      <button class="btn-ghost h-7 w-7 shrink-0 px-0 text-faint hover:text-bad" @click="remove(i)">
        <Trash2 class="h-3.5 w-3.5" />
      </button>
    </div>
    <button class="btn-ghost h-7 px-2 text-[11px]" @click="add">
      <Plus class="h-3.5 w-3.5" /> 添加 Header
    </button>
  </div>
</template>

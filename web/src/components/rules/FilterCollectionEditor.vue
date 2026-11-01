<script setup lang="ts">
import { computed } from 'vue'
import { FolderPlus, Plus, Trash2 } from 'lucide-vue-next'
import type { TypeDef } from '@/api/types'
import TypeForm from './TypeForm.vue'
import Button from '@/components/ui/Button.vue'
import Badge from '@/components/ui/Badge.vue'

const props = withDefaults(
  defineProps<{
    modelValue: Record<string, any>
    schema: TypeDef[]
    depth?: number
  }>(),
  { depth: 0 },
)
const emit = defineEmits<{ 'update:modelValue': [Record<string, any>] }>()

const operation = computed(() => (props.modelValue?.operation === 'Or' ? 'Or' : 'And'))
const children = computed<Record<string, any>[]>(() =>
  Array.isArray(props.modelValue?.children) ? props.modelValue.children : [],
)

function isGroup(child: Record<string, any>): boolean {
  return child?.typeKind === 'FilterCollection'
}

function defFor(kind: string): TypeDef | undefined {
  return props.schema.find((s) => s.typeKind === kind)
}

function emitGroup(nextChildren: Record<string, any>[], nextOperation = operation.value) {
  emit('update:modelValue', {
    typeKind: 'FilterCollection',
    operation: nextOperation,
    children: nextChildren,
  })
}

function setOperation(event: Event) {
  emitGroup(children.value, (event.target as HTMLSelectElement).value === 'Or' ? 'Or' : 'And')
}

function updateChild(index: number, child: Record<string, any>) {
  const next = [...children.value]
  next[index] = child
  emitGroup(next)
}

function removeChild(index: number) {
  emitGroup(children.value.filter((_, i) => i !== index))
}

function addCondition() {
  emitGroup([
    ...children.value,
    {
      typeKind: 'HostFilter',
      pattern: '',
      operation: 'Exact',
      caseSensitive: false,
      inverted: false,
    },
  ])
}

function addGroup() {
  emitGroup([...children.value, { typeKind: 'FilterCollection', operation: 'And', children: [] }])
}
</script>

<template>
  <div
    class="rounded-xl border border-line/60 bg-panel-strong/20 p-3"
    :class="depth > 0 ? 'border-accent/20 bg-accent/5' : ''"
  >
    <div class="flex flex-wrap items-center justify-between gap-2">
      <div class="flex flex-wrap items-center gap-2">
        <Badge variant="accent">{{ depth > 0 ? '子条件组' : '条件组合' }}</Badge>
        <span class="text-[11px] text-faint">{{ children.length }} 个条件</span>
      </div>
      <div class="flex flex-wrap items-center gap-1.5">
        <select
          class="select h-8 w-[132px] py-1 text-[11.5px]"
          :value="operation"
          aria-label="条件组合方式"
          @change="setOperation"
        >
          <option value="And">全部满足 (AND)</option>
          <option value="Or">任一满足 (OR)</option>
        </select>
        <Button size="sm" variant="ghost" @click="addCondition">
          <template #icon><Plus class="h-3.5 w-3.5" /></template>
          条件
        </Button>
        <Button size="sm" variant="ghost" @click="addGroup">
          <template #icon><FolderPlus class="h-3.5 w-3.5" /></template>
          分组
        </Button>
      </div>
    </div>

    <div v-if="children.length" class="mt-3 space-y-2">
      <div
        v-for="(child, index) in children"
        :key="index"
        class="rounded-xl border border-line/50 bg-panel-strong/35 p-3"
      >
        <div class="mb-2 flex items-center justify-between gap-2">
          <div class="min-w-0 text-[10px] uppercase tracking-wider text-faint">
            <span>条件 {{ index + 1 }}</span>
            <span v-if="defFor(child.typeKind)" class="ml-2 normal-case tracking-normal">
              {{ defFor(child.typeKind)?.category }} · {{ defFor(child.typeKind)?.scope }}
            </span>
          </div>
          <button
            type="button"
            class="btn-ghost h-6 w-6 shrink-0 px-0 text-faint hover:text-bad"
            title="删除条件"
            @click="removeChild(index)"
          >
            <Trash2 class="h-3.5 w-3.5" />
          </button>
        </div>

        <FilterCollectionEditor
          v-if="isGroup(child)"
          :model-value="child"
          :schema="schema"
          :depth="depth + 1"
          @update:model-value="updateChild(index, $event)"
        />
        <TypeForm
          v-else
          kind="filter"
          :model-value="child"
          :schema="schema"
          label="条件类型"
          @update:model-value="updateChild(index, $event)"
        />
      </div>
    </div>

    <div v-else class="mt-3 rounded-lg border border-dashed border-line/70 p-3 text-[11px] leading-relaxed text-faint">
      还没有子条件。至少添加一个条件；空 AND 组合会匹配所有请求。
    </div>

    <p class="mt-2 text-[10px] leading-relaxed text-faint">
      子条件按上方的 AND / OR 组合；可以继续添加嵌套分组。最终作用域由所有子条件中最大的作用域决定。
    </p>
  </div>
</template>

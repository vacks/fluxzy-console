<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { FieldDef, TypeDef } from '@/api/types'
import Input from '@/components/ui/Input.vue'
import Select from '@/components/ui/Select.vue'
import Switch from '@/components/ui/Switch.vue'
import Textarea from '@/components/ui/Textarea.vue'
import HeadersEditor from './HeadersEditor.vue'

const props = defineProps<{
  kind: 'filter' | 'action'
  modelValue: Record<string, any>
  schema: TypeDef[]
  label?: string
}>()
const emit = defineEmits<{ 'update:modelValue': [Record<string, any>] }>()

const currentKind = computed(() => String(props.modelValue?.typeKind || ''))
const def = computed(() => props.schema.find((s) => s.typeKind === currentKind.value))
const grouped = computed(() => {
  const map = new Map<string, TypeDef[]>()
  for (const t of props.schema) {
    if (!map.has(t.category)) map.set(t.category, [])
    map.get(t.category)!.push(t)
  }
  return [...map.entries()]
})

const jsonTexts = ref<Record<string, string>>({})
const jsonErrors = ref<Record<string, string>>({})
const focusedJson = ref('')

watch(
  () => [props.modelValue, props.schema],
  () => {
    for (const f of def.value?.fields || []) {
      if (f.type !== 'json') continue
      const v = getPath(props.modelValue, f.path)
      const text = v === undefined ? '' : JSON.stringify(v, null, 2)
      // Do not overwrite in-progress invalid JSON while the user is editing.
      if (focusedJson.value !== f.path || jsonTexts.value[f.path] === undefined) {
        jsonTexts.value[f.path] = text
      }
    }
  },
  { immediate: true, deep: true },
)

function getPath(obj: any, path: string): any {
  return path.split('.').reduce((acc, part) => (acc == null ? undefined : acc[part]), obj)
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

function updateField(field: FieldDef, value: any) {
  const next = JSON.parse(JSON.stringify(props.modelValue || {}))
  if (value === undefined || value === '' || (Array.isArray(value) && value.length === 0)) {
    // still set empty arrays for list fields, remove empty scalars
    if (Array.isArray(value)) setPath(next, field.path, value)
    else deletePath(next, field.path)
  } else {
    setPath(next, field.path, value)
  }
  next.typeKind = currentKind.value
  emit('update:modelValue', next)
}
function deletePath(obj: any, path: string) {
  const parts = path.split('.')
  let cur = obj
  for (let i = 0; i < parts.length - 1; i++) {
    if (!cur[parts[i]]) return
    cur = cur[parts[i]]
  }
  delete cur[parts[parts.length - 1]]
}

function changeType(kind: string) {
  const next: Record<string, any> = { typeKind: kind }
  const target = props.schema.find((s) => s.typeKind === kind)
  for (const f of target?.fields || []) {
    if (f.default !== undefined) setPath(next, f.path, f.default)
  }
  emit('update:modelValue', next)
  jsonTexts.value = {}
  jsonErrors.value = {}
}

function fieldValue(f: FieldDef) {
  return getPath(props.modelValue, f.path)
}
function listText(f: FieldDef) {
  const v = fieldValue(f)
  return Array.isArray(v) ? v.join(', ') : v ?? ''
}
function setList(f: FieldDef, text: string) {
  const parts = text.split(',').map((s) => s.trim()).filter(Boolean)
  if (f.type === 'intlist') {
    updateField(f, parts.map((p) => Number(p)).filter((n) => Number.isFinite(n)))
  } else {
    updateField(f, parts)
  }
}
function setJson(f: FieldDef, text: string) {
  jsonTexts.value[f.path] = text
  if (!text.trim()) {
    jsonErrors.value[f.path] = ''
    updateField(f, undefined)
    return
  }
  try {
    const parsed = JSON.parse(text)
    jsonErrors.value[f.path] = ''
    updateField(f, parsed)
  } catch (e: any) {
    jsonErrors.value[f.path] = e?.message || 'JSON 格式错误'
  }
}
</script>

<template>
  <div class="space-y-3">
    <label class="block">
      <span class="label">{{ label || (kind === 'filter' ? '条件类型' : '动作类型') }}</span>
      <select class="select" :value="currentKind" @change="changeType(($event.target as HTMLSelectElement).value)">
        <option value="" disabled>请选择</option>
        <optgroup v-for="[cat, list] in grouped" :key="cat" :label="cat">
          <option v-for="t in list" :key="t.typeKind" :value="t.typeKind">{{ t.label }} · {{ t.typeKind }}</option>
        </optgroup>
      </select>
    </label>

    <p v-if="def?.description" class="text-[11px] leading-relaxed text-faint">{{ def.description }}</p>

    <div v-if="def" class="space-y-3">
      <template v-for="f in def.fields" :key="f.path">
        <Switch
          v-if="f.type === 'bool'"
          :model-value="!!fieldValue(f)"
          :label="f.label"
          :hint="f.help"
          @update:model-value="updateField(f, $event)"
        />
        <Select
          v-else-if="f.type === 'select'"
          :label="f.label"
          :options="f.options || []"
          :model-value="String(fieldValue(f) ?? f.default ?? '')"
          :hint="f.help"
          @update:model-value="updateField(f, $event)"
        />
        <Input
          v-else-if="f.type === 'int'"
          :label="f.label"
          type="number"
          :placeholder="f.placeholder"
          :hint="f.help"
          :model-value="fieldValue(f) ?? ''"
          @update:model-value="updateField(f, $event === '' ? undefined : Number($event))"
        />
        <Textarea
          v-else-if="f.type === 'textarea'"
          :label="f.label"
          :placeholder="f.placeholder"
          :hint="f.help"
          :rows="5"
          :model-value="String(fieldValue(f) ?? '')"
          @update:model-value="updateField(f, $event)"
        />
        <div v-else-if="f.type === 'intlist' || f.type === 'stringlist'">
          <label class="block">
            <span class="label">{{ f.label }}</span>
            <input
              class="input font-mono text-[12px]"
              :value="listText(f)"
              :placeholder="f.placeholder"
              @change="setList(f, ($event.target as HTMLInputElement).value)"
            />
            <span class="hint">{{ f.help || '多个值用英文逗号分隔' }}</span>
          </label>
        </div>
        <div v-else-if="f.type === 'headers'">
          <div class="label">{{ f.label }}</div>
          <HeadersEditor
            :model-value="Array.isArray(fieldValue(f)) ? fieldValue(f) : []"
            @update:model-value="updateField(f, $event)"
          />
          <div v-if="f.help" class="hint">{{ f.help }}</div>
        </div>
        <div v-else-if="f.type === 'json'">
          <label class="block">
            <span class="label">{{ f.label }}</span>
            <textarea
              class="input min-h-[110px] font-mono text-[11.5px] leading-relaxed"
              :value="jsonTexts[f.path] ?? ''"
              :placeholder="f.placeholder"
              @focus="focusedJson = f.path"
              @blur="focusedJson = ''"
              @input="setJson(f, ($event.target as HTMLTextAreaElement).value)"
            />
            <span v-if="jsonErrors[f.path]" class="mt-1.5 block text-[11.5px] text-bad">{{ jsonErrors[f.path] }}</span>
            <span v-else-if="f.help" class="hint">{{ f.help }}</span>
          </label>
        </div>
        <Input
          v-else
          :label="f.label"
          :placeholder="f.placeholder"
          :hint="f.help"
          :monospace="f.path.includes('pattern') || f.path.includes('Name') || f.path.includes('e')"
          :model-value="String(fieldValue(f) ?? '')"
          @update:model-value="updateField(f, $event)"
        />
      </template>
    </div>
  </div>
</template>

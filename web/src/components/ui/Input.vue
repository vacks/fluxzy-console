<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    label?: string
    hint?: string
    error?: string
    placeholder?: string
    type?: string
    disabled?: boolean
    autocomplete?: string
    monospace?: boolean
    required?: boolean
    min?: number | string
    max?: number | string
    step?: number | string
    modelModifiers?: Record<string, boolean>
  }>(),
  { type: 'text', disabled: false, monospace: false, required: false },
)
const model = defineModel<string | number>()

function onInput(event: Event) {
  const raw = (event.target as HTMLInputElement).value
  if (!props.modelModifiers?.number) {
    model.value = raw
    return
  }
  if (raw === '') {
    model.value = ''
    return
  }
  const n = Number(raw)
  model.value = Number.isNaN(n) ? raw : n
}
</script>

<template>
  <label class="block">
    <span v-if="label" class="label">
      {{ label }}<span v-if="required" class="ml-0.5 text-bad">*</span>
    </span>
    <input
      :value="model ?? ''"
      :type="type"
      @input="onInput"
      :placeholder="placeholder"
      :disabled="disabled"
      :autocomplete="autocomplete"
      :min="min"
      :max="max"
      :step="step"
      :class="['input', monospace ? 'font-mono text-[12.5px]' : '', error ? 'border-bad/60 focus:border-bad' : '']"
    />
    <span v-if="error" class="mt-1.5 block text-[11.5px] text-bad">{{ error }}</span>
    <span v-else-if="hint" class="hint">{{ hint }}</span>
  </label>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    label?: string
    hint?: string
    error?: string
    placeholder?: string
    rows?: number
    disabled?: boolean
    monospace?: boolean
  }>(),
  { rows: 4, disabled: false, monospace: true },
)
const model = defineModel<string>()
</script>

<template>
  <label class="block">
    <span v-if="label" class="label">{{ label }}</span>
    <textarea
      v-model="model"
      :rows="rows"
      :placeholder="placeholder"
      :disabled="disabled"
      :class="['input', monospace ? 'font-mono text-[12.5px] leading-relaxed' : '', error ? 'border-bad/60' : '']"
    />
    <span v-if="error" class="mt-1.5 block text-[11.5px] text-bad">{{ error }}</span>
    <span v-else-if="hint" class="hint">{{ hint }}</span>
  </label>
</template>

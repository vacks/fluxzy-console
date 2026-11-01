<script setup lang="ts">
withDefaults(
  defineProps<{
    label?: string
    hint?: string
    error?: string
    placeholder?: string
    options: { value: string; label: string }[]
    disabled?: boolean
  }>(),
  { disabled: false },
)
const model = defineModel<string>()
</script>

<template>
  <label class="block">
    <span v-if="label" class="label">{{ label }}</span>
    <select v-model="model" :disabled="disabled" :class="['select', error ? 'border-bad/60' : '']">
      <option v-if="placeholder" value="">{{ placeholder }}</option>
      <option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option>
    </select>
    <span v-if="error" class="mt-1.5 block text-[11.5px] text-bad">{{ error }}</span>
    <span v-else-if="hint" class="hint">{{ hint }}</span>
  </label>
</template>

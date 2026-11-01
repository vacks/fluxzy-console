<script setup lang="ts">
withDefaults(
  defineProps<{ label?: string; hint?: string; disabled?: boolean; size?: 'sm' | 'md' }>(),
  { disabled: false, size: 'md' },
)
const model = defineModel<boolean>()
</script>

<template>
  <div class="flex items-center justify-between gap-3">
    <div v-if="label" class="min-w-0">
      <div class="text-[12.5px] font-medium text-ink">{{ label }}</div>
      <div v-if="hint" class="mt-0.5 text-[11.5px] text-faint">{{ hint }}</div>
    </div>
    <button
      type="button"
      role="switch"
      :aria-label="label"
      :aria-checked="!!model"
      :disabled="disabled"
      :class="[
        'relative inline-flex shrink-0 items-center rounded-full border transition-colors duration-200',
        size === 'sm' ? 'h-5 w-9' : 'h-6 w-11',
        model ? 'border-accent/50 bg-accent/80' : 'border-line/80 bg-line/60',
        disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer',
      ]"
      @click="model = !model"
    >
      <span
        :class="[
          'inline-block rounded-full bg-white shadow transition-transform duration-200',
          size === 'sm' ? 'h-3.5 w-3.5' : 'h-4 w-4',
          model ? (size === 'sm' ? 'translate-x-[18px]' : 'translate-x-[24px]') : 'translate-x-[3px]',
        ]"
      />
    </button>
  </div>
</template>

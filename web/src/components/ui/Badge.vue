<script setup lang="ts">
withDefaults(
  defineProps<{
    variant?: 'muted' | 'accent' | 'ok' | 'warn' | 'bad'
    dot?: boolean
  }>(),
  { variant: 'muted', dot: false },
)
</script>

<template>
  <span :class="`badge-${variant}`">
    <span v-if="dot" class="h-1.5 w-1.5 rounded-full bg-current" />
    <slot />
  </span>
</template>

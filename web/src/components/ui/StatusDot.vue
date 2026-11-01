<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{ tone?: 'ok' | 'warn' | 'bad' | 'muted' | 'accent'; pulse?: boolean; size?: 'sm' | 'md' }>(),
  { tone: 'muted', pulse: false, size: 'sm' },
)

const color = computed(
  () =>
    ({
      ok: 'bg-ok text-ok',
      warn: 'bg-warn text-warn',
      bad: 'bg-bad text-bad',
      muted: 'bg-faint text-faint',
      accent: 'bg-accent text-accent',
    })[props.tone],
)
</script>

<template>
  <span class="relative inline-flex" :class="size === 'md' ? 'h-2.5 w-2.5' : 'h-2 w-2'">
    <span v-if="pulse" class="absolute inline-flex h-full w-full animate-ping rounded-full opacity-40" :class="color" />
    <span class="relative inline-flex rounded-full" :class="[color, size === 'md' ? 'h-2.5 w-2.5' : 'h-2 w-2']" />
  </span>
</template>

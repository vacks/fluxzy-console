<script setup lang="ts">
import { Waypoints } from 'lucide-vue-next'
withDefaults(defineProps<{ size?: 'sm' | 'md' | 'lg' }>(), { size: 'md' })
</script>

<template>
  <div
    :class="[
      'relative flex items-center justify-center overflow-hidden rounded-2xl bg-gradient-to-br from-accent via-accent to-accent-2 shadow-glow',
      size === 'lg' ? 'h-12 w-12' : size === 'sm' ? 'h-8 w-8 rounded-xl' : 'h-10 w-10',
    ]"
  >
    <Waypoints :class="size === 'lg' ? 'h-6 w-6' : size === 'sm' ? 'h-4 w-4' : 'h-5 w-5'" class="text-white" />
    <div class="absolute inset-0 bg-gradient-to-tr from-white/0 via-white/20 to-white/0" />
  </div>
</template>

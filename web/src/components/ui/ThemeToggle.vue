<script setup lang="ts">
import { computed } from 'vue'
import { Monitor, Moon, Sun } from 'lucide-vue-next'
import { useThemeStore } from '@/stores/theme'

const theme = useThemeStore()
const icon = computed(() => (theme.mode === 'system' ? Monitor : theme.mode === 'dark' ? Moon : Sun))
</script>

<template>
  <button class="btn-icon text-muted hover:text-ink" :title="`主题：${theme.resolvedLabel}`" :aria-label="`主题：${theme.resolvedLabel}`" @click="theme.cycle()">
    <component :is="icon" class="h-4 w-4" />
  </button>
</template>

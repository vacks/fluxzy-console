<script setup lang="ts">
import { computed } from 'vue'
import JsonNode from './JsonNode.vue'

const props = defineProps<{ value: unknown; expandDepth?: number }>()

const parsed = computed<unknown>(() => {
  if (typeof props.value !== 'string') return props.value
  try {
    return JSON.parse(props.value)
  } catch {
    return props.value
  }
})
</script>

<template>
  <div class="overflow-x-auto rounded-xl border border-line/50 bg-[rgb(var(--bg-soft))] p-3">
    <JsonNode :value="parsed" :expand-depth="expandDepth ?? 2" />
  </div>
</template>

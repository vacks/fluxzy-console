<script setup lang="ts">
defineProps<{
  tabs: { key: string; label: string; count?: number }[]
}>()
const model = defineModel<string>()
</script>

<template>
  <div class="flex items-center gap-1 overflow-x-auto">
    <button
      v-for="tab in tabs"
      :key="tab.key"
      type="button"
      :class="[
        'relative shrink-0 rounded-lg px-3 py-1.5 text-[12.5px] font-medium transition',
        model === tab.key ? 'bg-panel-strong text-ink shadow-sm' : 'text-muted hover:bg-panel-strong/60 hover:text-ink',
      ]"
      @click="model = tab.key"
    >
      {{ tab.label }}
      <span v-if="tab.count !== undefined" class="ml-1 font-mono text-[10.5px] text-faint">{{ tab.count }}</span>
    </button>
  </div>
</template>

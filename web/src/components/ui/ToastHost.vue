<script setup lang="ts">
import { CheckCircle2, AlertCircle, Info, AlertTriangle, X } from 'lucide-vue-next'
import { useToastStore } from '@/stores/toast'

const toast = useToastStore()

const icon = {
  success: CheckCircle2,
  error: AlertCircle,
  info: Info,
  warning: AlertTriangle,
}
const tone = {
  success: 'text-ok',
  error: 'text-bad',
  info: 'text-accent-2',
  warning: 'text-warn',
}
</script>

<template>
  <div class="pointer-events-none fixed right-4 top-4 z-[120] flex w-[min(92vw,380px)] flex-col gap-2">
    <TransitionGroup name="fade">
      <div
        v-for="item in toast.items"
        :key="item.id"
        class="pointer-events-auto flex items-start gap-3 rounded-xl border border-line/70 bg-panel/95 p-3.5 shadow-panel backdrop-blur-xl"
      >
        <component :is="icon[item.kind]" class="mt-0.5 h-4.5 w-4.5 shrink-0" :class="tone[item.kind]" />
        <div class="min-w-0 flex-1">
          <div class="text-[13px] font-semibold text-ink">{{ item.title }}</div>
          <div v-if="item.message" class="mt-0.5 break-words text-[12px] leading-relaxed text-muted">
            {{ item.message }}
          </div>
        </div>
        <button class="btn-icon -mr-1 -mt-1 h-7 w-7 text-faint hover:text-ink" @click="toast.remove(item.id)">
          <X class="h-3.5 w-3.5" />
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>

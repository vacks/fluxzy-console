<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'
import { X } from 'lucide-vue-next'

const props = withDefaults(
  defineProps<{ title?: string; width?: string; closeOnBackdrop?: boolean; showClose?: boolean }>(),
  { width: '560px', closeOnBackdrop: true, showClose: true },
)
const open = defineModel<boolean>('open')

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && open.value && props.showClose) open.value = false
}

watch(
  open,
  (v) => {
    if (v) {
      document.body.style.overflow = 'hidden'
      window.addEventListener('keydown', onKey)
    } else {
      document.body.style.overflow = ''
      window.removeEventListener('keydown', onKey)
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  document.body.style.overflow = ''
  window.removeEventListener('keydown', onKey)
})
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="open" class="fixed inset-0 z-[100] flex items-start justify-center overflow-y-auto p-4 sm:items-center">
        <div
          class="fixed inset-0 bg-[rgba(2,6,23,.62)] backdrop-blur-sm"
          @click="closeOnBackdrop && (open = false)"
        />
        <div
          class="relative z-10 w-full animate-fade-up overflow-hidden rounded-2xl border border-line/70 bg-panel shadow-panel"
          :style="{ maxWidth: width }"
        >
          <header v-if="title || $slots.header || showClose" class="flex items-center justify-between gap-4 border-b border-line/60 px-5 py-4">
            <div class="min-w-0">
              <slot name="header">
                <h2 class="truncate text-[15px] font-semibold text-ink">{{ title }}</h2>
              </slot>
            </div>
            <button v-if="showClose" class="btn-icon h-8 w-8 text-muted hover:text-ink" @click="open = false">
              <X class="h-4 w-4" />
            </button>
          </header>
          <div class="max-h-[calc(100vh-14rem)] overflow-y-auto p-5">
            <slot />
          </div>
          <footer v-if="$slots.footer" class="flex items-center justify-end gap-2 border-t border-line/60 bg-panel-strong/40 px-5 py-3.5">
            <slot name="footer" />
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

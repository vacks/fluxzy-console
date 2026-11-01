<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'
import { X } from 'lucide-vue-next'

const props = withDefaults(defineProps<{ title?: string; width?: string }>(), {
  width: 'min(94vw, 840px)',
})
const open = defineModel<boolean>()

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && open.value) open.value = false
}

watch(
  open,
  (v) => {
    if (v) window.addEventListener('keydown', onKey)
    else window.removeEventListener('keydown', onKey)
  },
  { immediate: true },
)
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="open" class="fixed inset-0 z-[100]">
        <div class="absolute inset-0 bg-[rgba(2,6,23,.55)] backdrop-blur-[2px]" @click="open = false" />
        <Transition name="slide" appear>
          <aside
            class="absolute inset-y-0 right-0 flex w-full flex-col border-l border-line/70 bg-panel shadow-panel"
            :style="{ maxWidth: width }"
          >
            <header class="flex items-center justify-between gap-4 border-b border-line/60 px-5 py-4">
              <div class="min-w-0">
                <slot name="header">
                  <h2 class="truncate text-[15px] font-semibold text-ink">{{ title }}</h2>
                </slot>
              </div>
              <div class="flex items-center gap-2">
                <slot name="actions" />
                <button class="btn-icon h-8 w-8 text-muted hover:text-ink" @click="open = false">
                  <X class="h-4 w-4" />
                </button>
              </div>
            </header>
            <div class="min-h-0 flex-1 overflow-y-auto">
              <slot />
            </div>
            <footer v-if="$slots.footer" class="border-t border-line/60 bg-panel-strong/40 px-5 py-3">
              <slot name="footer" />
            </footer>
          </aside>
        </Transition>
      </div>
    </Transition>
  </Teleport>
</template>

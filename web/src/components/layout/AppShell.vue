<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Sidebar from './Sidebar.vue'
import Topbar from './Topbar.vue'
import { useCaptureStore } from '@/stores/capture'
import { useLiveStore } from '@/stores/live'
import { useAuthStore } from '@/stores/auth'

const mobileOpen = ref(false)
const capture = useCaptureStore()
const live = useLiveStore()
const auth = useAuthStore()

onMounted(() => {
  capture.startPolling()
  live.connect()
})

onBeforeUnmount(() => {
  capture.stopPolling()
  live.disconnect()
})

watch(
  () => auth.user,
  (u) => {
    if (!u) live.disconnect()
  },
)
</script>

<template>
  <div class="relative z-10 flex min-h-screen">
    <div class="sticky top-0 hidden h-screen lg:block">
      <Sidebar />
    </div>

    <Transition name="fade">
      <div v-if="mobileOpen" class="fixed inset-0 z-[90] lg:hidden">
        <div class="absolute inset-0 bg-[rgba(2,6,23,.6)] backdrop-blur-sm" @click="mobileOpen = false" />
        <div class="absolute inset-y-0 left-0 shadow-panel">
          <Sidebar @close="mobileOpen = false" />
        </div>
      </div>
    </Transition>

    <div class="flex min-w-0 flex-1 flex-col">
      <Topbar @toggle-menu="mobileOpen = true" />
      <main class="mx-auto w-full max-w-[1680px] flex-1 px-4 py-5 lg:px-7 lg:py-6">
        <RouterView v-slot="{ Component }">
          <Transition name="fade" mode="out-in">
            <component :is="Component" />
          </Transition>
        </RouterView>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Activity,
  Cpu,
  History,
  LayoutDashboard,
  ScrollText,
  Settings,
  SlidersHorizontal,
  Users,
  Waypoints,
  X,
} from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useCaptureStore } from '@/stores/capture'
import StatusDot from '@/components/ui/StatusDot.vue'

const emit = defineEmits<{ close: [] }>()
const auth = useAuthStore()
const capture = useCaptureStore()
const route = useRoute()
const router = useRouter()

const items = computed(() => {
  const list = [
    { name: 'overview', label: '概览', icon: LayoutDashboard },
    { name: 'live', label: '实时流量', icon: Activity },
    { name: 'sessions', label: '会话历史', icon: History },
    { name: 'rules', label: '规则管理', icon: SlidersHorizontal },
    { name: 'runtime', label: '运行时', icon: Cpu },
  ]
  if (auth.isAdmin) {
    list.push({ name: 'audit', label: '日志', icon: ScrollText })
    list.push({ name: 'users', label: '用户', icon: Users })
  }
  list.push({ name: 'settings', label: '设置', icon: Settings })
  return list
})

function isActive(name: string) {
  if (name === 'sessions') return route.name === 'sessions' || route.name === 'session'
  return route.name === name
}

function go(name: string) {
  router.push({ name })
  emit('close')
}
</script>

<template>
  <aside class="flex h-full w-[248px] shrink-0 flex-col border-r border-line/50 bg-panel/55 backdrop-blur-2xl">
    <div class="flex items-center justify-between px-5 py-5">
      <RouterLink to="/" class="group flex items-center gap-3" @click="emit('close')">
        <div class="relative flex h-9 w-9 items-center justify-center overflow-hidden rounded-xl bg-gradient-to-br from-accent to-accent-2 shadow-glow">
          <Waypoints class="h-5 w-5 text-white" />
          <div class="absolute inset-0 bg-gradient-to-tr from-white/0 via-white/20 to-white/0 opacity-0 transition group-hover:opacity-100" />
        </div>
        <div class="min-w-0">
          <div class="truncate text-[13.5px] font-semibold tracking-tight text-ink">FLUXZY <span class="text-accent">·</span> CONSOLE</div>
          <div class="mt-0.5 text-[10.5px] uppercase tracking-[0.16em] text-faint">Traffic Observatory</div>
        </div>
      </RouterLink>
      <button class="btn-icon h-8 w-8 text-muted lg:hidden" @click="emit('close')">
        <X class="h-4 w-4" />
      </button>
    </div>

    <nav class="mt-1 flex-1 space-y-0.5 overflow-y-auto px-3">
      <button
        v-for="item in items"
        :key="item.name"
        type="button"
        :class="['nav-item w-full text-left', isActive(item.name) ? 'nav-item-active' : '']"
        @click="go(item.name)"
      >
        <component :is="item.icon" class="h-4 w-4 shrink-0" :class="isActive(item.name) ? 'text-accent' : ''" />
        <span class="flex-1">{{ item.label }}</span>
        <span v-if="item.name === 'live' && capture.online" class="h-1.5 w-1.5 rounded-full bg-ok shadow-[0_0_8px_rgb(var(--ok))]" />
      </button>
    </nav>

    <div class="m-3 rounded-xl border border-line/60 bg-panel-strong/55 p-3">
      <div class="flex items-center justify-between">
        <span class="flex items-center gap-2 text-[11px] font-medium text-muted">
          <StatusDot :tone="capture.online ? 'ok' : 'muted'" :pulse="capture.online" />
          {{ capture.online ? '采集在线' : '采集离线' }}
        </span>
        <span class="font-mono text-[10.5px] text-faint">{{ Math.round(capture.stats.rps) }} rps</span>
      </div>
      <div class="mt-2.5 space-y-1 text-[10.5px]">
        <div class="flex items-center justify-between gap-2">
          <span class="text-faint">当前会话</span>
          <span class="truncate font-mono text-muted">{{ capture.activeSessionId || '—' }}</span>
        </div>
        <div class="flex items-center justify-between gap-2">
          <span class="text-faint">fluxzy</span>
          <span class="truncate font-mono text-muted">{{ capture.status?.proxyAddress || '—' }}</span>
        </div>
      </div>
    </div>
  </aside>
</template>

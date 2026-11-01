<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ChevronDown, LogOut, Menu, Settings, ShieldCheck, UserRound } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useCaptureStore } from '@/stores/capture'
import ThemeToggle from '@/components/ui/ThemeToggle.vue'
import StatusDot from '@/components/ui/StatusDot.vue'

defineEmits<{ 'toggle-menu': [] }>()
const auth = useAuthStore()
const capture = useCaptureStore()
const route = useRoute()
const router = useRouter()

const menuOpen = ref(false)
const title = computed(() => (route.meta.title as string) || '控制台')
const initial = computed(() => (auth.user?.username || '?').slice(0, 1).toUpperCase())
const roleLabel = computed(() => (auth.user ? '管理员' : ''))

function closeMenu(e: MouseEvent) {
  const el = e.target as HTMLElement
  if (!el.closest('[data-user-menu]')) menuOpen.value = false
}
onMounted(() => document.addEventListener('click', closeMenu))
onBeforeUnmount(() => document.removeEventListener('click', closeMenu))

async function logout() {
  menuOpen.value = false
  try {
    await auth.logout()
  } catch {
    // Local auth state is already cleared; still leave the protected page.
  } finally {
    router.push({ name: 'login' })
  }
}
</script>

<template>
  <header class="sticky top-0 z-40 flex h-[62px] items-center gap-3 border-b border-line/50 bg-bg/70 px-4 backdrop-blur-2xl lg:px-7">
    <button class="btn-icon h-9 w-9 text-muted lg:hidden" @click="$emit('toggle-menu')">
      <Menu class="h-4.5 w-4.5" />
    </button>

    <div class="min-w-0">
      <div class="flex items-center gap-1.5 text-[10.5px] uppercase tracking-[0.14em] text-faint">
        <span>Console</span>
        <span>/</span>
        <span class="text-muted">{{ title }}</span>
      </div>
      <h1 class="truncate text-[15px] font-semibold tracking-tight text-ink">{{ title }}</h1>
    </div>

    <div class="ml-auto flex items-center gap-1.5">
      <div class="hidden items-center gap-2 rounded-full border border-line/60 bg-panel/60 px-3 py-1.5 sm:flex">
        <StatusDot :tone="capture.online ? 'ok' : 'muted'" :pulse="capture.online" />
        <span class="text-[11.5px] font-medium" :class="capture.online ? 'text-ok' : 'text-muted'">
          {{ capture.online ? '在线' : '离线' }}
        </span>
        <span class="h-3 w-px bg-line/70" />
        <span class="font-mono text-[11px] text-muted">{{ Math.round(capture.stats.rps) }} rps</span>
      </div>

      <ThemeToggle />

      <div class="relative" data-user-menu>
        <button
          class="flex items-center gap-2 rounded-xl border border-transparent px-1.5 py-1.5 transition hover:border-line/60 hover:bg-panel-strong/60"
          @click.stop="menuOpen = !menuOpen"
        >
          <span class="flex h-7 w-7 items-center justify-center rounded-lg bg-gradient-to-br from-accent-strong to-accent-2-strong text-[12px] font-semibold text-white">
            {{ initial }}
          </span>
          <span class="hidden text-left sm:block">
            <span class="block text-[12px] font-medium leading-tight text-ink">{{ auth.user?.username }}</span>
            <span class="block text-[10.5px] leading-tight text-faint">{{ roleLabel }}</span>
          </span>
          <ChevronDown class="h-3.5 w-3.5 text-faint" />
        </button>

        <Transition name="fade">
          <div
            v-if="menuOpen"
            class="absolute right-0 top-[calc(100%+6px)] z-50 w-56 overflow-hidden rounded-xl border border-line/70 bg-panel/95 shadow-panel backdrop-blur-xl"
          >
            <div class="border-b border-line/50 px-3.5 py-3">
              <div class="flex items-center gap-2 text-[12.5px] font-medium text-ink">
                <UserRound class="h-3.5 w-3.5 text-accent" />
                {{ auth.user?.username }}
              </div>
              <div class="mt-1 flex items-center gap-1.5 text-[10.5px] text-faint">
                <ShieldCheck class="h-3 w-3" />
                {{ roleLabel }}
              </div>
            </div>
            <div class="p-1.5">
              <button class="nav-item w-full py-2 text-left text-[12px]" @click="menuOpen = false; router.push({ name: 'settings' })">
                <Settings class="h-3.5 w-3.5" /> 账号与设置
              </button>
              <button class="nav-item w-full py-2 text-left text-[12px] text-bad hover:text-bad" @click="logout">
                <LogOut class="h-3.5 w-3.5" /> 退出登录
              </button>
            </div>
          </div>
        </Transition>
      </div>
    </div>
  </header>
</template>

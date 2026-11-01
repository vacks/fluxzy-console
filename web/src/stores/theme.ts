import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import { usePreferredDark } from '@vueuse/core'

export type ThemeMode = 'dark' | 'light' | 'system'

const STORAGE_KEY = 'fc.theme'

export const useThemeStore = defineStore('theme', () => {
  const preferredDark = usePreferredDark()
  let raw = ''
  try {
    raw = localStorage.getItem(STORAGE_KEY) || ''
  } catch {
    /* storage can be unavailable in hardened/privacy contexts */
  }
  const stored: ThemeMode = raw === 'light' || raw === 'system' || raw === 'dark' ? raw : 'dark'
  const mode = ref<ThemeMode>(stored)
  const isDark = computed(() => (mode.value === 'system' ? preferredDark.value : mode.value === 'dark'))
  const resolvedLabel = computed(() => (mode.value === 'system' ? '跟随系统' : mode.value === 'dark' ? '深色' : '浅色'))

  function apply() {
    const root = document.documentElement
    root.classList.toggle('dark', isDark.value)
    root.style.colorScheme = isDark.value ? 'dark' : 'light'
    const meta = document.querySelector('meta[name="theme-color"]')
    if (meta) meta.setAttribute('content', isDark.value ? '#0b1020' : '#f6f8fc')
  }

  function setMode(next: ThemeMode) {
    mode.value = next
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      /* theme still applies for this session */
    }
  }

  function cycle() {
    const order: ThemeMode[] = ['dark', 'light', 'system']
    setMode(order[(order.indexOf(mode.value) + 1) % order.length])
  }

  watch([isDark, mode], apply, { immediate: true })

  return { mode, isDark, resolvedLabel, setMode, cycle }
})

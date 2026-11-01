import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, setCSRF } from '@/api/client'
import type { AuthStatus, LoginResponse, Role, User } from '@/api/types'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const csrf = ref('')
  const setupRequired = ref(false)
  const loaded = ref(false)
  const loading = ref(false)

  const isAdmin = computed(() => user.value?.role === 'admin')
  const isOperator = computed(() => user.value?.role === 'admin' || user.value?.role === 'operator')

  function applyAuth(u: User | null, token = '') {
    user.value = u
    csrf.value = token
    setCSRF(token)
  }

  async function load(force = false) {
    if (loaded.value && !force) return
    loading.value = true
    try {
      const s = await api.get<AuthStatus>('/auth/status', { skipUnauthorizedEvent: true })
      setupRequired.value = !!s.setupRequired
      if (s.authenticated && s.user) {
        applyAuth(s.user, s.csrfToken || '')
      } else {
        applyAuth(null)
      }
      loaded.value = true
    } finally {
      loading.value = false
    }
  }

  async function login(username: string, password: string) {
    const r = await api.post<LoginResponse>(
      '/auth/login',
      { username, password },
      { skipUnauthorizedEvent: true },
    )
    applyAuth(r.user, r.csrfToken)
    setupRequired.value = false
    loaded.value = true
    return r.user
  }

  async function setup(username: string, password: string) {
    const r = await api.post<LoginResponse>(
      '/auth/setup',
      { username, password },
      { skipUnauthorizedEvent: true },
    )
    applyAuth(r.user, r.csrfToken)
    setupRequired.value = false
    loaded.value = true
    return r.user
  }

  async function logout() {
    try {
      await api.post('/auth/logout')
    } finally {
      reset()
    }
  }

  function reset() {
    applyAuth(null)
  }

  function can(role: Role) {
    if (!user.value) return false
    if (user.value.role === 'admin') return true
    if (user.value.role === 'operator') return role !== 'admin'
    return role === 'viewer'
  }

  return {
    user,
    csrf,
    setupRequired,
    loaded,
    loading,
    isAdmin,
    isOperator,
    load,
    login,
    setup,
    logout,
    reset,
    can,
  }
})

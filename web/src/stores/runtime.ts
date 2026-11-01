import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '@/api/client'
import type {
  LogLine,
  RotationSettings,
  RotationStatus,
  RuntimeStatus,
  StartupConfig,
} from '@/api/types'

export const useRuntimeStore = defineStore('runtime', () => {
  const status = ref<RuntimeStatus | null>(null)
  const startup = ref<{ settings: StartupConfig; defaults: StartupConfig; hash: string; appliedHash: string; pending: boolean } | null>(null)
  const rotation = ref<RotationStatus | null>(null)
  const logs = ref<LogLine[]>([])
  const loading = ref(false)
  const actionLoading = ref('')
  const error = ref('')

  let timer: number | undefined
  let statusInFlight = false

  const supervisor = computed(() => status.value?.supervisor || null)
  const state = computed(() => supervisor.value?.state || 'unknown')
  const online = computed(() => !!status.value?.capture?.online)
  const activeSessionId = computed(() => supervisor.value?.activeSessionId || status.value?.capture?.activeSessionId || '')
  const configPending = computed(() => !!status.value?.config?.pending)

  async function fetchStatus() {
    if (statusInFlight) return
    statusInFlight = true
    try {
      status.value = await api.get<RuntimeStatus>('/system/fluxzy')
      error.value = ''
    } catch (e: any) {
      error.value = e?.message || '无法读取运行时状态'
    } finally {
      statusInFlight = false
    }
  }

  async function fetchStartup() {
    startup.value = await api.get('/system/fluxzy/startup')
  }

  async function fetchRotation() {
    rotation.value = await api.get<RotationStatus>('/system/fluxzy/rotation')
  }

  async function fetchAll() {
    loading.value = true
    try {
      await Promise.all([fetchStatus(), fetchStartup(), fetchRotation()])
    } finally {
      loading.value = false
    }
  }

  async function action(name: 'start' | 'stop' | 'restart' | 'rotate') {
    actionLoading.value = name
    try {
      await api.post(`/system/fluxzy/${name}`, undefined, { timeoutMs: 60_000 })
      await fetchStatus()
    } finally {
      actionLoading.value = ''
    }
  }

  async function saveStartup(next: StartupConfig) {
    const r = await api.put<{ settings: StartupConfig; hash: string; pending: boolean }>(
      '/system/fluxzy/startup',
      next,
    )
    await fetchStartup()
    return r
  }

  async function applyStartup(expectedHash: string) {
    actionLoading.value = 'apply-startup'
    try {
      const r = await api.post<{ applied: boolean; hash: string; sessionId: string }>(
        '/system/fluxzy/startup/apply',
        { expectedHash },
        { timeoutMs: 90_000 },
      )
      await fetchStartup()
      await fetchStatus()
      return r
    } finally {
      actionLoading.value = ''
    }
  }

  async function saveRotation(next: RotationSettings) {
    await api.put('/system/fluxzy/rotation', next)
    await fetchRotation()
  }

  async function fetchLogs(tail = 200, since = 0) {
    const r = await api.get<{ lines: LogLine[]; nextSeq: number }>(
      `/system/fluxzy/logs?tail=${tail}${since ? `&since=${since}` : ''}`,
    )
    if (since) {
      logs.value = [...logs.value, ...(r.lines || [])].slice(-2000)
    } else {
      logs.value = r.lines || []
    }
    return r
  }

  function startPolling() {
    if (timer) return
    // The view's initial load calls fetchAll(), which already includes
    // fetchStatus(); do not issue a duplicate request here.
    timer = window.setInterval(() => void fetchStatus(), 3000)
  }

  function stopPolling() {
    if (timer) {
      window.clearInterval(timer)
      timer = undefined
    }
  }

  return {
    status,
    startup,
    rotation,
    logs,
    loading,
    actionLoading,
    error,
    supervisor,
    state,
    online,
    activeSessionId,
    configPending,
    fetchStatus,
    fetchStartup,
    fetchRotation,
    fetchAll,
    action,
    saveStartup,
    applyStartup,
    saveRotation,
    fetchLogs,
    startPolling,
    stopPolling,
  }
})

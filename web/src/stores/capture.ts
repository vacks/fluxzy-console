import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '@/api/client'
import type { CaptureSession, CaptureStatus, Stats } from '@/api/types'

const EMPTY_STATS: Stats = {
  sessionId: '',
  total: 0,
  pending: 0,
  errors: 0,
  reqBytes: 0,
  respBytes: 0,
  status2xx: 0,
  status3xx: 0,
  status4xx: 0,
  status5xx: 0,
  rps: 0,
  bytesPerSec: 0,
  lastEventAt: 0,
}

export const useCaptureStore = defineStore('capture', () => {
  const status = ref<CaptureStatus | null>(null)
  const sessions = ref<CaptureSession[]>([])
  const loading = ref(false)
  const sessionsLoading = ref(false)
  const error = ref('')
  const lastUpdated = ref(0)

  let timer: number | undefined
  let sessionsRequest = 0
  let statusInFlight = false

  const online = computed(() => !!status.value?.online && !error.value)
  const activeSession = computed(() => status.value?.activeSession || null)
  const activeSessionId = computed(() => status.value?.activeSessionId || '')
  const stats = computed<Stats>(() => status.value?.stats || EMPTY_STATS)

  async function fetchStatus() {
    if (statusInFlight) return
    statusInFlight = true
    try {
      const s = await api.get<CaptureStatus>('/capture/status')
      status.value = s
      error.value = ''
      lastUpdated.value = Date.now()
    } catch (e: any) {
      error.value = e?.message || '无法读取采集状态'
    } finally {
      statusInFlight = false
    }
  }

  async function fetchSessions(limit = 100) {
    const request = ++sessionsRequest
    sessionsLoading.value = true
    try {
      const r = await api.get<{ items: CaptureSession[]; activeSessionId: string; online: boolean }>(
        `/capture/sessions?limit=${limit}`,
      )
      // Overview asks for 6 rows while Sessions asks for 200. Do not let the
      // slower, smaller response overwrite the richer list.
      if (request !== sessionsRequest) return
      sessions.value = r.items || []
    } finally {
      if (request === sessionsRequest) sessionsLoading.value = false
    }
  }

  function startPolling() {
    if (timer) return
    void fetchStatus()
    timer = window.setInterval(() => void fetchStatus(), 5000)
  }

  function stopPolling() {
    if (timer) {
      window.clearInterval(timer)
      timer = undefined
    }
  }

  return {
    status,
    sessions,
    loading,
    sessionsLoading,
    error,
    lastUpdated,
    online,
    activeSession,
    activeSessionId,
    stats,
    fetchStatus,
    fetchSessions,
    startPolling,
    stopPolling,
  }
})

import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '@/api/client'
import type { ExchangeList, ExchangeRow, Stats } from '@/api/types'
import { useRuntimeStore } from './runtime'

const MAX_ROWS = 5000

let socket: WebSocket | null = null
let reconnectTimer: number | undefined
let manualClose = false
let attempt = 0

export const useLiveStore = defineStore('live', () => {
  const connected = ref(false)
  const paused = ref(false)
  const events = ref<ExchangeRow[]>([])
  const newCount = ref(0)
  const lastId = ref(0)
  const activeSessionId = ref('')
  const online = ref(false)
  const indexing = ref(false)
  const maxPerSec = ref(20)
  const sent = ref(0)
  const dropped = ref(0)
  const recovered = ref(0)
  const stats = ref<Stats | null>(null)
  const loadedSessionId = ref('')
  // Highest exchange id seen by the recent-window refresh. The WebSocket is
  // intentionally sampled; the Live page refreshes only a small recent window
  // and never tries to download every rate-limited row over REST.
  const syncedThroughId = ref(0)
  const clearedThroughId = ref(0)
  const backfillEnabled = ref(false)
  let syncing = false
  let initialLoading = false
  let lastBackfillAt = 0

  const sampled = computed(() => dropped.value > 0 && (stats.value?.rps || 0) > maxPerSec.value)

  function enableBackfill() {
    backfillEnabled.value = true
  }

  function disableBackfill() {
    backfillEnabled.value = false
  }

  function applySession(next: string) {
    if (!next || next === activeSessionId.value) return
    activeSessionId.value = next
    events.value = []
    lastId.value = 0
    syncedThroughId.value = 0
    clearedThroughId.value = 0
    recovered.value = 0
    loadedSessionId.value = ''
    newCount.value = 0
    void loadInitial(next)
  }

  function upsert(row: ExchangeRow, countNew = true): boolean {
    if (row.id <= clearedThroughId.value) return false
    if (row.id > lastId.value) lastId.value = row.id
    const idx = events.value.findIndex((e) => e.id === row.id)
    if (idx >= 0) {
      events.value[idx] = row
      return false
    }
    events.value.unshift(row)
    if (events.value.length > MAX_ROWS) events.value.length = MAX_ROWS
    if (countNew) newCount.value++
    return true
  }

  async function syncNew(sessionId = activeSessionId.value) {
    if (!backfillEnabled.value || !sessionId || paused.value || syncing || loadedSessionId.value !== sessionId) return
    const now = Date.now()
    if (now - lastBackfillAt < 5000) return
    lastBackfillAt = now
    syncing = true
    try {
      // Refresh only the recent window. The WebSocket stream is intentionally
      // sampled at maxPerSec; pulling every missed row over REST defeated that
      // sampling and generated thousands of rows/second on busy proxies.
      const recent = await api.get<ExchangeList>(
        `/capture/sessions/${encodeURIComponent(sessionId)}/exchanges?limit=250`,
      )
      if (activeSessionId.value !== sessionId || loadedSessionId.value !== sessionId) return
      for (const row of recent.items || []) {
        if (upsert(row, false)) recovered.value++
      }
      syncedThroughId.value = lastId.value
    } catch {
      // The next stats tick retries; the index remains the source of truth.
    } finally {
      syncing = false
    }
  }

  async function loadInitial(sessionId = activeSessionId.value) {
    if (!sessionId || initialLoading) return
    if (!activeSessionId.value) activeSessionId.value = sessionId
    initialLoading = true
    try {
      const r = await api.get<ExchangeList>(
        `/capture/sessions/${encodeURIComponent(sessionId)}/exchanges?limit=250`,
      )
      // A session switch may have happened while the request was in flight.
      if (activeSessionId.value !== sessionId) return
      events.value = r.items || []
      clearedThroughId.value = 0
      lastId.value = events.value.reduce((max, row) => (row.id > max ? row.id : max), 0)
      syncedThroughId.value = lastId.value
      recovered.value = 0
      loadedSessionId.value = sessionId
      newCount.value = 0
    } catch {
      // The session may not be indexed yet.
    } finally {
      initialLoading = false
    }
  }

  async function loadAfter(afterId: number, sessionId = activeSessionId.value) {
    if (!sessionId || afterId <= 0) return
    try {
      const r = await api.get<ExchangeList>(
        `/capture/sessions/${encodeURIComponent(sessionId)}/exchanges?after=${afterId}&limit=500`,
      )
      if (activeSessionId.value !== sessionId) return
      for (const row of r.items || []) upsert(row)
    } catch {
      // Ignore; the next live event will catch up.
    }
  }

  function handleMessage(raw: MessageEvent) {
    let msg: any
    try {
      msg = JSON.parse(raw.data)
    } catch {
      return
    }
    switch (msg.type) {
      case 'hello': {
        connected.value = true
        attempt = 0
        const d = msg.data || {}
        online.value = !!d.online
        indexing.value = !!d.indexing
        stats.value = d.stats || null
        maxPerSec.value = d.maxEventsPerSec || 20
        if (d.activeSessionId) applySession(d.activeSessionId)
        break
      }
      case 'exchange': {
        if (msg.sessionId && activeSessionId.value && msg.sessionId !== activeSessionId.value) break
        if (paused.value) break
        if (msg.data) upsert(msg.data)
        break
      }
      case 'stats': {
        const d = msg.data || {}
        if (d.stats) stats.value = d.stats
        if (typeof d.online === 'boolean') online.value = d.online
        if (typeof d.indexing === 'boolean') indexing.value = d.indexing
        if (d.activeSessionId) applySession(d.activeSessionId)
        if (activeSessionId.value && loadedSessionId.value !== activeSessionId.value) {
          void loadInitial(activeSessionId.value)
        }
        if (d.subscriber) {
          sent.value = d.subscriber.sent || 0
          dropped.value = d.subscriber.dropped || 0
          if (d.subscriber.paused !== undefined) paused.value = !!d.subscriber.paused
        }
        if (backfillEnabled.value) void syncNew()
        break
      }
      case 'session': {
        const d = msg.data || {}
        if (d.activeSessionId) {
          online.value = !!d.online
          applySession(d.activeSessionId)
        }
        break
      }
      case 'paused':
        paused.value = true
        break
      case 'resync': {
        const d = msg.data || {}
        void loadAfter(d.lastId || lastId.value, d.activeSessionId || activeSessionId.value)
        break
      }
      case 'capture.status': {
        const d = msg.data || {}
        online.value = !!d.online
        if (d.activeSessionId) applySession(d.activeSessionId)
        break
      }
      case 'runtime.status':
        // The event carries supervisor.Status, while the runtime store holds
        // the composite /system/fluxzy response. Refresh it instead of
        // assigning an incompatible shape.
        void useRuntimeStore().fetchStatus()
        break
      case 'rotation':
        void useRuntimeStore().fetchRotation()
        break
    }
  }

  function connect() {
    window.clearTimeout(reconnectTimer)
    if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING || socket.readyState === WebSocket.CLOSING)) return
    manualClose = false
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${proto}//${location.host}/api/v1/live`)
    socket = ws

    ws.onopen = () => {
      if (socket !== ws) return
      connected.value = true
      attempt = 0
      if (paused.value) ws.send(JSON.stringify({ type: 'pause' }))
    }
    ws.onmessage = (ev) => {
      if (socket === ws) handleMessage(ev)
    }
    ws.onclose = () => {
      if (socket !== ws) return
      connected.value = false
      socket = null
      if (manualClose) return
      attempt++
      const delay = Math.min(10000, 800 * Math.pow(1.6, Math.min(attempt, 6)))
      window.clearTimeout(reconnectTimer)
      reconnectTimer = window.setTimeout(() => connect(), delay)
    }
    ws.onerror = () => {
      // onclose handles reconnection.
    }
  }

  function disconnect() {
    manualClose = true
    window.clearTimeout(reconnectTimer)
    attempt = 0
    if (socket) {
      socket.close()
      socket = null
    }
    connected.value = false
  }

  function pause() {
    paused.value = true
    if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'pause' }))
  }

  function resume() {
    paused.value = false
    newCount.value = 0
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: 'resume', lastId: lastId.value }))
    } else {
      connect()
      if (activeSessionId.value && loadedSessionId.value !== activeSessionId.value) {
        void loadInitial()
      }
    }
    void syncNew()
  }

  function clear() {
    clearedThroughId.value = lastId.value
    events.value = []
    newCount.value = 0
    syncedThroughId.value = lastId.value
    recovered.value = 0
  }

  function markSeen() {
    newCount.value = 0
  }

  return {
    connected,
    paused,
    events,
    newCount,
    lastId,
    activeSessionId,
    online,
    indexing,
    maxPerSec,
    sent,
    dropped,
    recovered,
    stats,
    sampled,
    loadedSessionId,
    connect,
    disconnect,
    pause,
    resume,
    clear,
    markSeen,
    loadInitial,
    syncNew,
    enableBackfill,
    disableBackfill,
  }
})

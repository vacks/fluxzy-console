export function formatNumber(n: number | undefined | null): string {
  if (n === undefined || n === null || Number.isNaN(n)) return '0'
  return new Intl.NumberFormat('zh-CN').format(n)
}

export function formatBytes(bytes: number | undefined | null, digits = 1): string {
  if (!bytes || bytes < 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = bytes
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : digits)} ${units[i]}`
}

export function formatRate(bytesPerSec: number | undefined | null): string {
  return `${formatBytes(bytesPerSec || 0, 1)}/s`
}

export function formatDuration(ms: number | undefined | null): string {
  if (ms === undefined || ms === null || ms < 0) return '-'
  if (ms < 1) return '<1 ms'
  if (ms < 1000) return `${Math.round(ms)} ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)} s`
  const m = Math.floor(ms / 60_000)
  const s = Math.round((ms % 60_000) / 1000)
  return `${m}m ${s}s`
}

export function formatTime(ms: number | undefined | null, withMs = true): string {
  if (!ms) return '-'
  const d = new Date(ms)
  const time = d.toLocaleTimeString('zh-CN', { hour12: false })
  if (!withMs) return time
  return `${time}.${String(d.getMilliseconds()).padStart(3, '0')}`
}

export function formatDateTime(ms: number | undefined | null): string {
  if (!ms) return '-'
  return new Date(ms).toLocaleString('zh-CN', { hour12: false })
}

export function formatDate(ms: number | undefined | null): string {
  if (!ms) return '-'
  return new Date(ms).toLocaleDateString('zh-CN')
}

export function formatRelative(ms: number | undefined | null): string {
  if (!ms) return '-'
  const diff = Date.now() - ms
  if (diff < 10_000) return '刚刚'
  if (diff < 60_000) return `${Math.floor(diff / 1000)} 秒前`
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`
  if (diff < 30 * 86_400_000) return `${Math.floor(diff / 86_400_000)} 天前`
  return formatDate(ms)
}

export function formatISO(iso?: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return `${d.toLocaleTimeString('zh-CN', { hour12: false })}.${String(d.getMilliseconds()).padStart(3, '0')}`
}

export function statusTone(status: number): 'ok' | 'warn' | 'bad' | 'muted' | 'accent' {
  if (status >= 500) return 'bad'
  if (status >= 400) return 'warn'
  if (status >= 300) return 'accent'
  if (status >= 200) return 'ok'
  return 'muted'
}

export function statusText(status: number, pending?: boolean): string {
  if (pending && !status) return '进行中'
  if (!status) return '无响应'
  return String(status)
}

export function methodTone(method: string): string {
  switch ((method || '').toUpperCase()) {
    case 'GET':
      return 'text-emerald-400 bg-emerald-400/10'
    case 'POST':
      return 'text-sky-400 bg-sky-400/10'
    case 'PUT':
      return 'text-amber-400 bg-amber-400/10'
    case 'PATCH':
      return 'text-violet-400 bg-violet-400/10'
    case 'DELETE':
      return 'text-rose-400 bg-rose-400/10'
    default:
      return 'text-slate-400 bg-slate-400/10'
  }
}

export function contentTypeLabel(ct?: string): string {
  if (!ct) return '-'
  const v = ct.toLowerCase()
  if (v.includes('json')) return 'JSON'
  if (v.includes('html')) return 'HTML'
  if (v.includes('css')) return 'CSS'
  if (v.includes('javascript')) return 'JS'
  if (v.includes('xml')) return 'XML'
  if (v.startsWith('image/')) return 'IMG'
  if (v.startsWith('font/') || v.includes('woff')) return 'FONT'
  if (v.startsWith('audio/')) return 'AUDIO'
  if (v.startsWith('video/')) return 'VIDEO'
  if (v.includes('pdf')) return 'PDF'
  if (v.startsWith('text/')) return 'TEXT'
  if (v.includes('protobuf') || v.includes('grpc')) return 'PROTO'
  return 'BIN'
}

export function shortUrl(url?: string, max = 96): string {
  if (!url) return '-'
  return url.length > max ? url.slice(0, max - 1) + '…' : url
}

export function percent(part: number, total: number): number {
  if (!total) return 0
  return Math.round((part / total) * 1000) / 10
}

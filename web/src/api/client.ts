const BASE = '/api/v1'

let csrfToken = ''

export function setCSRF(token: string) {
  csrfToken = token || ''
}

export class ApiError extends Error {
  status: number
  code: string
  details?: unknown

  constructor(status: number, code: string, message: string, details?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
  }
}

type RequestOptions = {
  timeoutMs?: number
  skipUnauthorizedEvent?: boolean
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  opts: RequestOptions = {},
): Promise<T> {
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), opts.timeoutMs ?? 90_000)
  const headers: Record<string, string> = { Accept: 'application/json' }
  const isForm = typeof FormData !== 'undefined' && body instanceof FormData
  if (body !== undefined && !isForm) headers['Content-Type'] = 'application/json'
  if (method !== 'GET' && method !== 'HEAD') headers['X-CSRF-Token'] = csrfToken

  try {
    const res = await fetch(BASE + path, {
      method,
      credentials: 'include',
      headers,
      body: body === undefined ? undefined : isForm ? (body as FormData) : JSON.stringify(body),
      signal: controller.signal,
    })

    if (res.status === 401 && !opts.skipUnauthorizedEvent) {
      window.dispatchEvent(new CustomEvent('fc:unauthorized'))
    }

    const text = await res.text()
    let data: any = null
    if (text) {
      try {
        data = JSON.parse(text)
      } catch {
        data = text
      }
    }

    if (!res.ok) {
      const err = data?.error
      throw new ApiError(
        res.status,
        err?.code || 'http_error',
        err?.message || (typeof data === 'string' ? data : `请求失败 (${res.status})`),
        err?.details ?? data,
      )
    }
    return data as T
  } catch (e) {
    if (e instanceof ApiError) throw e
    if ((e as Error).name === 'AbortError') {
      throw new ApiError(0, 'timeout', '请求超时，请稍后重试')
    }
    throw new ApiError(0, 'network_error', (e as Error).message || '网络错误')
  } finally {
    window.clearTimeout(timeout)
  }
}

export const api = {
  get: <T>(path: string, opts?: RequestOptions) => request<T>('GET', path, undefined, opts),
  post: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>('POST', path, body, opts),
  upload: <T>(path: string, form: FormData, opts?: RequestOptions) =>
    request<T>('POST', path, form, opts),
  put: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>('PUT', path, body, opts),
  patch: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>('PATCH', path, body, opts),
  del: <T>(path: string, opts?: RequestOptions) => request<T>('DELETE', path, undefined, opts),
}

export function download(path: string) {
  const a = document.createElement('a')
  a.href = BASE + path
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  a.remove()
}

// fetchDownload waits for the server response so export failures (for
// example an expired session or a CLI packing error) can be shown as a toast
// instead of silently navigating to a JSON error page.
export async function fetchDownload(path: string, fallbackName = 'download'): Promise<string> {
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 10 * 60_000)
  const res = await fetch(BASE + path, { credentials: 'include', signal: controller.signal }).finally(() => {
    window.clearTimeout(timeout)
  })
  if (!res.ok) {
    const text = await res.text()
    let message = `下载失败 (${res.status})`
    try {
      const data = JSON.parse(text)
      message = data?.error?.message || message
    } catch {
      if (text) message = text
    }
    throw new ApiError(res.status, 'download_failed', message)
  }
  const length = Number(res.headers.get('Content-Length') || 0)
  if (length > 16 << 20) {
    // Large exports must not be buffered in the tab. Let the browser stream
    // the response to disk using the normal authenticated navigation.
    void res.body?.cancel()
    download(path)
    return fallbackName
  }
  const blob = await res.blob()
  const disposition = res.headers.get('Content-Disposition') || ''
  const match = /filename="?([^";]+)"?/i.exec(disposition)
  const filename = match?.[1] || fallbackName
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  return filename
}

export function qs(params: Record<string, string | number | boolean | undefined | null>): string {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '') continue
    p.set(k, String(v))
  }
  const s = p.toString()
  return s ? `?${s}` : ''
}

export class ApiError extends Error {
  constructor(public code: string, public status: number) { super(code) }
}

export async function api<T = any>(path: string, opts: { method?: string; body?: unknown; raw?: BodyInit; headers?: Record<string, string> } = {}): Promise<T> {
  const headers: Record<string, string> = { 'X-GP': '1', ...(opts.headers || {}) }
  let body: BodyInit | undefined = opts.raw
  if (opts.body !== undefined) { headers['Content-Type'] = 'application/json'; body = JSON.stringify(opts.body) }
  const res = await fetch('/api' + path, { method: opts.method || (body ? 'POST' : 'GET'), headers, body, credentials: 'same-origin' })
  const text = await res.text()
  let data: any = text
  try { data = JSON.parse(text) } catch { /* plain text (file contents) */ }
  if (!res.ok) {
    if (res.status === 401) {
      // Session expired or invalid - redirect to login
      if (location.hash !== '/' && location.hash !== '') {
        location.hash = '/'
      }
      window.dispatchEvent(new Event('gp:auth-expired'))
    }
    throw new ApiError((data && data.error) || 'error', res.status)
  }
  return data as T
}

export function wsUrl(path: string) {
  return (location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + path
}

export type Instance = {
  id: string; name: string; slug: string; type: string; version: string; java: number; ramMb: number
  port: number; network: string; status: string; message: string; createdAt: number; modKind: string
}
export type Me = { id: number; username: string; role: string; perms: string[]; totpEnabled?: boolean; viaToken?: boolean }
export type Live = { cpu: number; memMb: number; players: string[]; status: string }
export type Host = { cpu?: number; memUsedMb?: number; memTotalMb?: number; diskUsedGb?: number; diskTotalGb?: number }

export const can = (me: Me, p: string) => me.perms.includes('*') || me.perms.includes(p)

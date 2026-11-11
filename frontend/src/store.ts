import { useEffect, useState, useSyncExternalStore } from 'react'
import { Host, Instance, Live, api, wsUrl } from './api'

// ---- hash router ----
const subs = new Set<() => void>()
window.addEventListener('hashchange', () => subs.forEach((f) => f()))
export const useRoute = () => useSyncExternalStore((f) => { subs.add(f); return () => subs.delete(f) }, () => location.hash.slice(1) || '/')
export const go = (p: string) => { location.hash = p }

// ---- preferences (sidebar position + collapsed) ----
export type SidebarPos = 'left' | 'right' | 'top'
const prefSubs = new Set<() => void>()
const read = () => (localStorage.getItem('gp_sidebar') as SidebarPos) || 'left'
let sidebar: SidebarPos = read()
let collapsed = localStorage.getItem('gp_collapsed') === '1'
export const setSidebar = (p: SidebarPos) => { sidebar = p; localStorage.setItem('gp_sidebar', p); prefSubs.forEach((f) => f()) }
export const setCollapsed = (c: boolean) => { collapsed = c; localStorage.setItem('gp_collapsed', c ? '1' : '0'); prefSubs.forEach((f) => f()) }
const subPref = (f: () => void) => { prefSubs.add(f); return () => prefSubs.delete(f) }
export const useSidebar = () => useSyncExternalStore(subPref, () => sidebar)
export const useCollapsed = () => useSyncExternalStore(subPref, () => collapsed)

// ---- live stats over one shared websocket ----
export type LiveState = { host: Host; live: Record<string, Live>; history: { cpu: number[]; mem: number[] } }
const empty: LiveState = { host: {}, live: {}, history: { cpu: [], mem: [] } }
let state: LiveState = empty
const liveSubs = new Set<() => void>()
let ws: WebSocket | null = null
let retry: number | undefined
let backoff = 1000 // initial backoff in ms
const MAX_BACKOFF = 30000

function connect() {
  if (ws || liveSubs.size === 0) return
  ws = new WebSocket(wsUrl('/ws/stats'))
  ws.onmessage = (e) => {
    const m = JSON.parse(e.data)
    const live: Record<string, Live> = {}
    for (const r of m.instances) live[r.id] = { cpu: r.cpu, memMb: r.memMb, players: r.players || [], status: r.status }
    const h: Host = m.host
    const cpu = [...state.history.cpu, h.cpu ?? 0].slice(-40)
    const mem = [...state.history.mem, h.memTotalMb ? ((h.memUsedMb ?? 0) / h.memTotalMb) * 100 : 0].slice(-40)
    state = { host: h, live, history: { cpu, mem } }
    backoff = 1000 // reset backoff on successful connection
    liveSubs.forEach((f) => f())
  }
  ws.onclose = () => {
    ws = null
    if (liveSubs.size) {
      // Exponential backoff with jitter
      const delay = backoff + Math.random() * 1000
      backoff = Math.min(backoff * 2, MAX_BACKOFF)
      retry = window.setTimeout(connect, delay)
    }
  }
  ws.onerror = () => {
    // onclose will be called after onerror
  }
}
export function useLive(): LiveState {
  return useSyncExternalStore((f) => {
    liveSubs.add(f); connect()
    return () => { liveSubs.delete(f); if (!liveSubs.size) { clearTimeout(retry); ws?.close(); ws = null } }
  }, () => state)
}

// ---- instances list ----
export function useInstances() {
  const [list, setList] = useState<Instance[] | null>(null)
  const reload = () => api<Instance[]>('/instances').then(setList).catch(() => setList([]))
  useEffect(() => {
    reload()
    const i = window.setInterval(reload, 4000)
    return () => window.clearInterval(i)
  }, [])
  return { list, reload }
}

// ---- toasts ----
export type Toast = { id: number; text: string; kind: 'ok' | 'err' }
let toasts: Toast[] = []
const toastSubs = new Set<() => void>()
export const toast = (text: string, kind: 'ok' | 'err' = 'ok') => {
  const id = Date.now() + Math.random()
  toasts = [...toasts, { id, text, kind }]
  toastSubs.forEach((f) => f())
  setTimeout(() => { toasts = toasts.filter((x) => x.id !== id); toastSubs.forEach((f) => f()) }, 4000)
}
export const useToasts = () => useSyncExternalStore((f) => { toastSubs.add(f); return () => toastSubs.delete(f) }, () => toasts)

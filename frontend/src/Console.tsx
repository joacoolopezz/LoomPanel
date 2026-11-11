import { FormEvent, useEffect, useRef, useState } from 'react'
import { SendHorizontal } from 'lucide-react'
import { wsUrl } from './api'
import { useT } from './i18n'

const ansi = /\x1b\[[0-9;]*[A-Za-z]/g
const tone = (l: string) => (/\bERROR\b|Exception|FATAL/.test(l) ? 'text-danger' : /\bWARN\b/.test(l) ? 'text-warn' : /joined the game|Done \(/.test(l) ? 'text-olive-light' : 'text-ink/90')

export function Console({ id, status, canSend, height = 'h-[60vh]' }: { id: string; status: string; canSend: boolean; height?: string }) {
  const t = useT()
  const [lines, setLines] = useState<string[]>([])
  const [cmd, setCmd] = useState('')
  const wsRef = useRef<WebSocket | null>(null)
  const box = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const hist = useRef<string[]>([]); const hi = useRef(-1)

  useEffect(() => {
    setLines([])
    let closed = false, timer: number | undefined
    const open = () => {
      const ws = new WebSocket(wsUrl(`/ws/instances/${id}/console`))
      wsRef.current = ws
      ws.onmessage = (e) => { const m = JSON.parse(e.data); if (m.t === 'log') setLines((p) => [...p, ...(m.lines as string[]).map((l) => l.replace(ansi, ''))].slice(-1000)) }
      ws.onclose = () => { if (!closed) timer = window.setTimeout(open, 2000) }
    }
    open()
    return () => { closed = true; clearTimeout(timer); wsRef.current?.close() }
  }, [id])

  useEffect(() => { if (stick.current && box.current) box.current.scrollTop = box.current.scrollHeight }, [lines])

  const send = (e: FormEvent) => {
    e.preventDefault()
    if (!cmd.trim() || wsRef.current?.readyState !== 1) return
    wsRef.current.send(JSON.stringify({ t: 'cmd', d: cmd }))
    hist.current = [cmd, ...hist.current].slice(0, 50); hi.current = -1; setCmd('')
  }
  const key = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowUp' && hist.current.length) { e.preventDefault(); hi.current = Math.min(hi.current + 1, hist.current.length - 1); setCmd(hist.current[hi.current]) }
    if (e.key === 'ArrowDown') { e.preventDefault(); hi.current = Math.max(hi.current - 1, -1); setCmd(hi.current < 0 ? '' : hist.current[hi.current]) }
  }
  const active = status === 'running' || status === 'starting'

  return (
    <div className="card overflow-hidden flex flex-col">
      <div ref={box} onScroll={(e) => { const el = e.currentTarget; stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40 }}
        className={`${height} overflow-auto bg-bg px-4 py-3 font-mono text-[12.5px] leading-relaxed`} role="log" aria-live="off">
        {lines.map((l, i) => <div key={i} className={`whitespace-pre-wrap break-words ${tone(l)}`}>{l}</div>)}
      </div>
      {canSend && (
        <form onSubmit={send} className="flex items-center gap-2 border-t border-line p-2.5">
          <span className="pl-2 font-mono text-accent-hover select-none">&gt;</span>
          <input className="flex-1 bg-transparent font-mono text-sm outline-none placeholder:text-ink-mute/60" value={cmd} onChange={(e) => setCmd(e.target.value)} onKeyDown={key}
            placeholder={active ? t('console.placeholder') : t('console.offline')} disabled={!active} autoComplete="off" autoCapitalize="off" spellCheck={false} />
          <button className="btn-primary btn-sm" disabled={!active || !cmd.trim()} aria-label={t('console.send')}><SendHorizontal size={15} /></button>
        </form>
      )}
    </div>
  )
}

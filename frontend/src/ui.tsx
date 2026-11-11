import { ReactNode, useEffect } from 'react'
import { X } from 'lucide-react'
import { useT } from './i18n'
import { useToasts } from './store'

export function StatusDot({ status }: { status: string }) {
  const t = useT()
  const color = { running: 'bg-olive-light', starting: 'bg-warn pulse', stopping: 'bg-warn pulse', offline: 'bg-ink-mute/50', installing: 'bg-warn pulse', error: 'bg-danger' }[status] || 'bg-ink-mute/50'
  return (
    <span className="inline-flex items-center gap-2 text-sm text-ink-mute">
      <span className={`h-2 w-2 rounded-full ${color}`} />
      {t('status.' + status)}
    </span>
  )
}

export function Bar({ value, tone = 'olive' }: { value: number; tone?: 'olive' | 'accent' }) {
  const v = Math.max(0, Math.min(100, value))
  return (
    <div className="h-1.5 w-full rounded-full bg-raised overflow-hidden" role="progressbar" aria-valuenow={Math.round(v)} aria-valuemin={0} aria-valuemax={100}>
      <div className={`h-full rounded-full transition-[width] duration-500 ${v > 90 ? 'bg-danger' : tone === 'olive' ? 'bg-olive-light' : 'bg-accent'}`} style={{ width: `${v}%` }} />
    </div>
  )
}

export function Spark({ data }: { data: number[] }) {
  if (data.length < 2) return <div className="h-8" />
  const pts = data.map((v, i) => `${(i / (data.length - 1)) * 100},${30 - Math.min(100, v) * 0.28}`).join(' ')
  return (
    <svg viewBox="0 0 100 32" preserveAspectRatio="none" className="h-8 w-full" aria-hidden>
      <polyline points={pts} fill="none" stroke="#8C8B4A" strokeWidth="1.5" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
    </svg>
  )
}

export function Modal({ title, onClose, children, wide }: { title: string; onClose: () => void; children: ReactNode; wide?: boolean }) {
  const t = useT()
  useEffect(() => { const f = (e: KeyboardEvent) => e.key === 'Escape' && onClose(); window.addEventListener('keydown', f); return () => window.removeEventListener('keydown', f) }, [onClose])
  return (
    <div className="fixed inset-0 z-50 flex items-end sm:items-center justify-center bg-black/70 p-0 sm:p-4" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div role="dialog" aria-modal="true" aria-label={title} className={`pop card w-full ${wide ? 'sm:max-w-3xl' : 'sm:max-w-md'} max-h-[92vh] overflow-auto rounded-b-none sm:rounded-b-2xl p-5`}>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button className="p-1.5 rounded-lg text-ink-mute hover:bg-raised" onClick={onClose} aria-label={t('common.close')}><X size={18} /></button>
        </div>
        {children}
      </div>
    </div>
  )
}

export function Toasts() {
  const list = useToasts()
  return (
    <div className="fixed z-[60] bottom-4 left-1/2 -translate-x-1/2 flex flex-col gap-2 w-[92vw] max-w-sm pointer-events-none" aria-live="polite">
      {list.map((x) => (
        <div key={x.id} className={`pop rounded-xl px-4 py-3 text-sm border ${x.kind === 'ok' ? 'bg-card border-line' : 'bg-card border-danger/50 text-danger'}`}>{x.text}</div>
      ))}
    </div>
  )
}

export const Field = ({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) => (
  <label className="block">
    <span className="label">{label}</span>
    {children}
    {hint && <span className="block mt-1.5 text-xs text-ink-mute">{hint}</span>}
  </label>
)

export const fmtBytes = (n: number) => (n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : n < 1073741824 ? `${(n / 1048576).toFixed(1)} MB` : `${(n / 1073741824).toFixed(2)} GB`)
export const fmtMB = (mb: number) => (mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${Math.round(mb)} MB`)

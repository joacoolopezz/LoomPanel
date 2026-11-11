import { useCallback, useEffect, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { ApiError, api } from './api'
import { errMsg, t as tr, useT } from './i18n'
import { toast } from './store'
import { Field, Modal } from './ui'

type S = { id: string; name: string; cron: string; action: string; payload: string; enabled: boolean; lastRun: number }

function describe(cron: string): string {
  let m = cron.match(/^(\d+) (\d+) \* \* \*$/)
  if (m) return `${tr('sched.dailyAt')} ${m[2].padStart(2, '0')}:${m[1].padStart(2, '0')}`
  m = cron.match(/^0 \*\/(\d+) \* \* \*$/)
  if (m) return tr('sched.everyHours').replace('{n}', m[1])
  return cron
}

export function SchedulesTab({ id }: { id: string }) {
  const t = useT()
  const base = `/instances/${id}/schedules`
  const [list, setList] = useState<S[] | null>(null)
  const [adding, setAdding] = useState(false)
  const [tz, setTz] = useState('')
  const err = (e: unknown) => toast(errMsg((e as ApiError).code), 'err')
  const load = useCallback(() => api<S[]>(base).then(setList).catch(() => {}), [base])
  useEffect(() => { load(); api<{ tz: string; serverTime: string }>('/system').then((s) => setTz(`${s.tz} (${s.serverTime})`)) }, [load])

  const toggle = async (s: S) => { try { await api(`${base}/${s.id}`, { method: 'PATCH', body: { enabled: !s.enabled } }); load() } catch (e) { err(e) } }
  const del = async (s: S) => { if (!confirm(s.name + '?')) return; try { await api(`${base}/${s.id}`, { method: 'DELETE' }); load() } catch (e) { err(e) } }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-ink-mute">{t('sched.tz')}: <span className="font-mono">{tz}</span></p>
        <button className="btn-primary btn-sm" onClick={() => setAdding(true)}><Plus size={15} />{t('sched.new')}</button>
      </div>
      <div className="card divide-y divide-line">
        {list && list.length === 0 && <p className="px-4 py-6 text-sm text-ink-mute text-center">{t('sched.empty')}</p>}
        {list?.map((s) => (
          <div key={s.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-sm">
            <div className={`min-w-0 ${s.enabled ? '' : 'opacity-50'}`}>
              <div className="font-medium truncate">{s.name}</div>
              <div className="text-xs text-ink-mute">{t('sched.' + s.action)}{s.action === 'command' ? `: ${s.payload}` : s.action === 'backup' ? ` (${s.payload})` : ''} · {describe(s.cron)}</div>
              <div className="text-xs text-ink-mute">{t('sched.last')}: {s.lastRun ? new Date(s.lastRun * 1000).toLocaleString() : t('sched.never')}</div>
            </div>
            <div className="flex items-center gap-2">
              <button role="switch" aria-checked={s.enabled} onClick={() => toggle(s)} className={`relative h-6 w-11 rounded-full transition-colors ${s.enabled ? 'bg-accent' : 'bg-raised'}`}>
                <span className={`absolute top-0.5 h-5 w-5 rounded-full bg-white transition-all ${s.enabled ? 'left-[22px]' : 'left-0.5'}`} /></button>
              <button className="btn-danger btn-sm" onClick={() => del(s)} aria-label={t('files.delete')}><Trash2 size={14} /></button>
            </div>
          </div>
        ))}
      </div>
      {adding && <AddModal base={base} onClose={() => setAdding(false)} onDone={() => { setAdding(false); load() }} />}
    </div>
  )
}

function AddModal({ base, onClose, onDone }: { base: string; onClose: () => void; onDone: () => void }) {
  const t = useT()
  const [f, setF] = useState({ name: '', action: 'restart', mode: 'daily', time: '04:00', hours: 6, cron: '0 4 * * *', command: '', keep: 7 })
  const [busy, setBusy] = useState(false)
  const cron = f.mode === 'daily' ? `${+f.time.split(':')[1]} ${+f.time.split(':')[0]} * * *` : f.mode === 'hours' ? `0 */${f.hours} * * *` : f.cron
  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { await api(base, { body: { name: f.name, cron, action: f.action, payload: f.action === 'command' ? f.command : f.action === 'backup' ? String(f.keep) : '' } }); onDone() }
    catch (x) { toast(errMsg((x as ApiError).code), 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={t('sched.new')} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label={t('sched.name')}><input className="input" value={f.name} maxLength={60} onChange={(e) => setF({ ...f, name: e.target.value })} required autoFocus /></Field>
        <Field label={t('sched.action')}>
          <select className="input" value={f.action} onChange={(e) => setF({ ...f, action: e.target.value })}>
            {['restart', 'backup', 'command', 'start', 'stop'].map((a) => <option key={a} value={a}>{t('sched.' + a)}</option>)}
          </select>
        </Field>
        {f.action === 'command' && <Field label={t('sched.cmd')}><input className="input font-mono" value={f.command} onChange={(e) => setF({ ...f, command: e.target.value })} placeholder="say Reinicio en 5 minutos" required /></Field>}
        {f.action === 'backup' && <Field label={t('sched.keep')}><input className="input" type="number" min={1} max={100} value={f.keep} onChange={(e) => setF({ ...f, keep: +e.target.value })} /></Field>}
        <Field label={t('sched.when')}>
          <select className="input" value={f.mode} onChange={(e) => setF({ ...f, mode: e.target.value })}>
            <option value="daily">{t('sched.daily')}</option><option value="hours">{t('sched.hours')}</option><option value="custom">{t('sched.custom')}</option>
          </select>
        </Field>
        {f.mode === 'daily' && <Field label={t('sched.time')}><input className="input" type="time" value={f.time} onChange={(e) => setF({ ...f, time: e.target.value })} required /></Field>}
        {f.mode === 'hours' && <Field label={t('sched.everyN')}><input className="input" type="number" min={1} max={23} value={f.hours} onChange={(e) => setF({ ...f, hours: +e.target.value })} /></Field>}
        {f.mode === 'custom' && <Field label="Cron" hint={t('sched.cronHint')}><input className="input font-mono" value={f.cron} onChange={(e) => setF({ ...f, cron: e.target.value })} required /></Field>}
        <div className="flex justify-end gap-2"><button type="button" className="btn-neutral" onClick={onClose}>{t('common.cancel')}</button><button className="btn-primary" disabled={busy}>{t('common.create')}</button></div>
      </form>
    </Modal>
  )
}

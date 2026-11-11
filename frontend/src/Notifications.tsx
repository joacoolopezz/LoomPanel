import { FormEvent, useCallback, useEffect, useState } from 'react'
import { Bell, Cloud, Plus, Send, Trash2 } from 'lucide-react'
import { ApiError, api } from './api'
import { errMsg, useT } from './i18n'
import { toast } from './store'
import { Field, Modal } from './ui'

type Ch = { id: string; type: string; name: string; summary: string; events: string[]; enabled: boolean }
const types = ['discord', 'telegram', 'webhook', 'email']

export function Notifications() {
  const t = useT()
  const [data, setData] = useState<{ channels: Ch[]; events: string[] } | null>(null)
  const [adding, setAdding] = useState(false)
  const load = useCallback(() => api<{ channels: Ch[]; events: string[] }>('/notifications').then(setData).catch(() => {}), [])
  useEffect(() => { load() }, [load])
  const err = (e: unknown) => toast(errMsg((e as ApiError).code), 'err')

  const toggle = async (c: Ch) => { try { await api('/notifications/' + c.id, { method: 'PATCH', body: { enabled: !c.enabled } }); load() } catch (e) { err(e) } }
  const del = async (c: Ch) => { if (!confirm(c.name + '?')) return; await api('/notifications/' + c.id, { method: 'DELETE' }); load() }
  const test = async (c: Ch) => { try { await api(`/notifications/${c.id}/test`, { body: {} }); toast(t('notif.testOk')) } catch (e) { err(e) } }

  return (
    <div className="card p-5 space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div><h2 className="font-semibold flex items-center gap-2"><Bell size={18} className="text-olive-light" />{t('notif.title')}</h2><p className="text-sm text-ink-mute mt-1">{t('notif.desc')}</p></div>
        <button className="btn-primary btn-sm shrink-0" onClick={() => setAdding(true)}><Plus size={14} />{t('notif.add')}</button>
      </div>
      <div className="divide-y divide-line rounded-xl border border-line">
        {data && data.channels.length === 0 && <p className="px-4 py-3 text-sm text-ink-mute">{t('notif.none')}</p>}
        {data?.channels.map((c) => (
          <div key={c.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-sm">
            <div className={`min-w-0 ${c.enabled ? '' : 'opacity-50'}`}>
              <div className="truncate">{c.name} <span className="text-xs text-ink-mute capitalize">· {c.type} · {c.summary}</span></div>
              <div className="flex flex-wrap gap-1.5 mt-1.5">{c.events.map((e) => <span key={e} className="rounded-md bg-raised px-2 py-0.5 text-xs text-ink-mute">{t('notif.event.' + e)}</span>)}</div>
            </div>
            <div className="flex items-center gap-2">
              <button className="btn-neutral btn-sm" onClick={() => test(c)}><Send size={13} />{t('notif.test')}</button>
              <button role="switch" aria-checked={c.enabled} onClick={() => toggle(c)} className={`relative h-6 w-11 rounded-full transition-colors ${c.enabled ? 'bg-accent' : 'bg-raised'}`}>
                <span className={`absolute top-0.5 h-5 w-5 rounded-full bg-white transition-all ${c.enabled ? 'left-[22px]' : 'left-0.5'}`} /></button>
              <button className="btn-danger btn-sm" onClick={() => del(c)} aria-label={t('files.delete')}><Trash2 size={14} /></button>
            </div>
          </div>
        ))}
      </div>
      {adding && data && <AddChannel events={data.events} onClose={() => setAdding(false)} onDone={() => { setAdding(false); load() }} />}
    </div>
  )
}

function AddChannel({ events, onClose, onDone }: { events: string[]; onClose: () => void; onDone: () => void }) {
  const t = useT()
  const [type, setType] = useState('discord')
  const [name, setName] = useState('')
  const [cf, setCf] = useState<Record<string, string>>({ port: '587' })
  const [sel, setSel] = useState<string[]>(['server.crashed', 'backup.failed'])
  const set = (k: string) => (e: React.ChangeEvent<HTMLInputElement>) => setCf({ ...cf, [k]: e.target.value })
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    try { await api('/notifications', { body: { type, name, events: sel, config: cf } }); onDone() } catch (x) { toast(errMsg((x as ApiError).code), 'err') }
  }
  return (
    <Modal title={t('notif.add')} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label={t('notif.type')}><select className="input capitalize" value={type} onChange={(e) => setType(e.target.value)}>{types.map((x) => <option key={x} value={x}>{x}</option>)}</select></Field>
        <Field label={t('sec.tokens.name')}><input className="input" value={name} maxLength={40} onChange={(e) => setName(e.target.value)} required /></Field>
        {type === 'discord' && <Field label={t('notif.discordUrl')} hint={t('notif.discordHint')}><input className="input font-mono text-xs" value={cf.url || ''} onChange={set('url')} required /></Field>}
        {type === 'telegram' && <>
          <Field label={t('notif.tgToken')} hint={t('notif.tgHint')}><input className="input font-mono text-xs" value={cf.token || ''} onChange={set('token')} required /></Field>
          <Field label={t('notif.tgChat')}><input className="input font-mono" value={cf.chatId || ''} onChange={set('chatId')} required /></Field></>}
        {type === 'webhook' && <>
          <Field label={t('notif.url')}><input className="input font-mono text-xs" type="url" value={cf.url || ''} onChange={set('url')} required /></Field>
          <Field label={t('notif.secret')}><input className="input" value={cf.secret || ''} onChange={set('secret')} /></Field></>}
        {type === 'email' && <>
          <div className="grid grid-cols-3 gap-3"><div className="col-span-2"><Field label={t('notif.host')}><input className="input" value={cf.host || ''} onChange={set('host')} required /></Field></div>
            <Field label={t('notif.port')}><input className="input" value={cf.port || ''} onChange={set('port')} required /></Field></div>
          <p className="text-xs text-ink-mute -mt-2">{t('notif.emailHint')}</p>
          <div className="grid grid-cols-2 gap-3"><Field label={t('notif.user')}><input className="input" value={cf.user || ''} onChange={set('user')} autoComplete="off" /></Field>
            <Field label={t('notif.pass')}><input className="input" type="password" value={cf.pass || ''} onChange={set('pass')} autoComplete="new-password" /></Field></div>
          <div className="grid grid-cols-2 gap-3"><Field label={t('notif.from')}><input className="input" type="email" value={cf.from || ''} onChange={set('from')} required /></Field>
            <Field label={t('notif.to')}><input className="input" type="email" value={cf.to || ''} onChange={set('to')} required /></Field></div></>}
        <div><span className="label">{t('notif.events')}</span>
          <div className="space-y-2">{events.map((ev) => (
            <label key={ev} className="flex items-center gap-3 text-sm"><input type="checkbox" className="h-4 w-4 accent-[#8C2B32]" checked={sel.includes(ev)} onChange={() => setSel(sel.includes(ev) ? sel.filter((x) => x !== ev) : [...sel, ev])} />{t('notif.event.' + ev)}</label>))}</div></div>
        <div className="flex justify-end gap-2"><button type="button" className="btn-neutral" onClick={onClose}>{t('common.cancel')}</button><button className="btn-primary" disabled={sel.length === 0}>{t('common.create')}</button></div>
      </form>
    </Modal>
  )
}

export function Integrations() {
  const t = useT()
  const [on, setOn] = useState<boolean | null>(null)
  const [token, setToken] = useState('')
  const load = () => api<{ cloudflare: boolean }>('/integrations').then((r) => setOn(r.cloudflare)).catch(() => {})
  useEffect(() => { load() }, [])
  const connect = async (e: FormEvent) => { e.preventDefault(); try { await api('/integrations/cloudflare', { body: { token } }); setToken(''); load() } catch (x) { toast(errMsg((x as ApiError).code), 'err') } }
  const disconnect = async () => { await api('/integrations/cloudflare', { method: 'DELETE' }); load() }
  return (
    <div className="card p-5 space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div><h2 className="font-semibold flex items-center gap-2"><Cloud size={18} className="text-olive-light" />{t('int.cf')}</h2><p className="text-sm text-ink-mute mt-1">{t('int.cf.desc')}</p></div>
        {on && <button className="btn-neutral btn-sm shrink-0" onClick={disconnect}>{t('int.cf.disconnect')}</button>}
      </div>
      {on ? <p className="text-sm text-olive-light">✓ {t('int.cf.connected')}</p> : on === false && (
        <form onSubmit={connect} className="space-y-3">
          <Field label={t('int.cf.token')} hint={t('int.cf.hint')}><input className="input font-mono text-xs" type="password" autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)} required /></Field>
          <button className="btn-primary btn-sm">{t('int.cf.connect')}</button>
        </form>
      )}
    </div>
  )
}

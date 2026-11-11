import { FormEvent, useEffect, useState } from 'react'
import { ApiError, api } from './api'
import { errMsg, useT } from './i18n'
import { toast } from './store'

type P = { online: string[]; whitelist: string[]; ops: string[]; banned: string[] }

export function PlayersTab({ id, running, canManage }: { id: string; running: boolean; canManage: boolean }) {
  const t = useT()
  const [p, setP] = useState<P | null>(null)
  const [name, setName] = useState('')
  const load = () => api<P>(`/instances/${id}/players`).then(setP).catch(() => {})
  useEffect(() => { load(); const i = setInterval(load, 4000); return () => clearInterval(i) }, [id])

  const act = async (action: string, n: string) => {
    try { await api(`/instances/${id}/players`, { body: { action, name: n } }); setTimeout(load, 700) } catch (e) { toast(errMsg((e as ApiError).code), 'err') }
  }
  const add = (e: FormEvent) => { e.preventDefault(); if (name.trim()) { act('whitelist-add', name.trim()); setName('') } }
  const Row = ({ n, actions }: { n: string; actions: [string, string][] }) => (
    <div className="flex items-center justify-between gap-2 px-4 py-2.5 text-sm">
      <span className="truncate">{n}</span>
      {canManage && running && <div className="flex gap-1.5 shrink-0">{actions.map(([a, label]) => <button key={a} className="btn-neutral btn-sm" onClick={() => act(a, n)}>{label}</button>)}</div>}
    </div>
  )
  const List = ({ title, items, actions }: { title: string; items: string[]; actions: [string, string][] }) => (
    <section className="space-y-2">
      <h3 className="font-medium">{title} <span className="text-ink-mute font-normal">{items.length}</span></h3>
      <div className="card divide-y divide-line">{items.length === 0 ? <p className="px-4 py-3 text-sm text-ink-mute">{t('players.none')}</p> : items.map((n) => <Row key={n} n={n} actions={actions} />)}</div>
    </section>
  )
  if (!p) return null
  return (
    <div className="space-y-6">
      {canManage && (
        <form onSubmit={add} className="flex gap-2 max-w-md">
          <input className="input" placeholder={t('players.add')} value={name} onChange={(e) => setName(e.target.value)} maxLength={16} />
          <button className="btn-primary shrink-0" disabled={!running || !name.trim()}>{t('players.wlAdd')}</button>
        </form>
      )}
      {canManage && !running && <p className="text-sm text-ink-mute">{t('players.needRunning')}</p>}
      <div className="grid gap-6 md:grid-cols-2">
        <List title={t('players.online')} items={p.online} actions={[['kick', t('players.kick')], ['ban', t('players.ban')], ['op', t('players.op')]]} />
        <List title={t('players.whitelist')} items={p.whitelist} actions={[['whitelist-remove', t('files.delete')]]} />
        <List title={t('players.ops')} items={p.ops} actions={[['deop', t('players.deop')]]} />
        <List title={t('players.banned')} items={p.banned} actions={[['pardon', t('players.pardon')]]} />
      </div>
    </div>
  )
}

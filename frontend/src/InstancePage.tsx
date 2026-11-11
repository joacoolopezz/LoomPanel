import { useEffect, useState } from 'react'
import { Play, RotateCw, Skull, Square } from 'lucide-react'
import { ApiError, Instance, Me, api, can } from './api'
import { Console } from './Console'
import { FilesTab } from './Files'
import { PlayersTab } from './Players'
import { ModsTab } from './Mods'
import { BackupsTab } from './Backups'
import { SchedulesTab } from './Schedules'
import { DomainCard } from './Domain'
import { errMsg, useT } from './i18n'
import { go, toast, useLive } from './store'
import { Bar, Field, StatusDot, fmtMB } from './ui'

export default function InstancePage({ id, tab, me }: { id: string; tab: string; me: Me }) {
  const t = useT()
  const [inst, setInst] = useState<Instance | null | false>(null)
  const { live } = useLive()
  const load = () => api<Instance>('/instances/' + id).then(setInst).catch(() => setInst(false))
  useEffect(() => { setInst(null); load(); const i = setInterval(load, 3000); return () => clearInterval(i) }, [id])

  if (inst === false) return <p className="text-ink-mute">404</p>
  if (!inst) return null
  const l = live[id]
  const status = l?.status || inst.status
  const on = status === 'running' || status === 'starting'
  const power = async (action: string) => { try { await api(`/instances/${id}/power`, { body: { action } }); load() } catch (e) { toast(errMsg((e as ApiError).code), 'err') } }
  const tabs = [
    { k: 'console', label: t('tab.console'), show: true },
    { k: 'files', label: t('tab.files'), show: can(me, 'instance.files') },
    { k: 'players', label: t('tab.players'), show: true },
    { k: 'mods', label: inst.modKind === 'plugins' ? t('tab.plugins') : t('tab.mods'), show: !!inst.modKind && can(me, 'instance.mods') },
    { k: 'backups', label: t('tab.backups'), show: can(me, 'instance.backups') },
    { k: 'schedules', label: t('tab.schedules'), show: can(me, 'instance.schedules') },
    { k: 'settings', label: t('tab.settings'), show: can(me, 'instance.create') },
  ].filter((x) => x.show)

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight truncate">{inst.name}</h1>
          <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-ink-mute">
            <StatusDot status={status} /><span className="capitalize">{inst.type} {inst.version}</span><span>Java {inst.java}</span>
          </div>
        </div>
        {can(me, 'instance.power') && (
          <div className="flex gap-2">
            {!on && <button className="btn-primary" disabled={status !== 'offline'} onClick={() => power('start')}><Play size={16} />{t('srv.start')}</button>}
            {on && <>
              <button className="btn-neutral" onClick={() => power('restart')}><RotateCw size={16} />{t('srv.restart')}</button>
              <button className="btn-neutral" onClick={() => power('stop')}><Square size={15} />{t('srv.stop')}</button>
              <button className="btn-danger" onClick={() => power('kill')} title={t('srv.kill')} aria-label={t('srv.kill')}><Skull size={16} /></button>
            </>}
          </div>
        )}
      </div>

      {(inst.status === 'error') && <div className="card border-danger/50 p-4 text-sm text-danger break-words">{inst.message || t('status.error')}</div>}
      {on && l && (
        <div className="grid grid-cols-2 gap-3">
          <div className="card p-3.5 space-y-2"><div className="flex justify-between text-sm"><span className="text-ink-mute">{t('dash.cpu')}</span><span className="tabular-nums">{l.cpu.toFixed(0)}%</span></div><Bar value={l.cpu} /></div>
          <div className="card p-3.5 space-y-2"><div className="flex justify-between text-sm"><span className="text-ink-mute">{t('dash.ram')}</span><span className="tabular-nums">{fmtMB(l.memMb)} / {fmtMB(inst.ramMb)}</span></div><Bar value={(l.memMb / inst.ramMb) * 100} /></div>
        </div>
      )}

      <div className="flex gap-1 border-b border-line overflow-x-auto" role="tablist">
        {tabs.map((x) => (
          <a key={x.k} href={`#/servers/${id}/${x.k}`} role="tab" aria-selected={tab === x.k}
            className={`px-4 h-10 inline-flex items-center text-sm whitespace-nowrap border-b-2 -mb-px transition-colors ${tab === x.k ? 'border-accent text-ink' : 'border-transparent text-ink-mute hover:text-ink'}`}>{x.label}</a>
        ))}
      </div>

      {tab === 'console' && (inst.status === 'installing' ? <p className="text-ink-mute">{t('status.installing')}…</p> : <Console key={id} id={id} status={status} canSend={can(me, 'instance.console')} />)}
      {tab === 'files' && can(me, 'instance.files') && <FilesTab id={id} />}
      {tab === 'players' && <PlayersTab id={id} running={status === 'running'} canManage={can(me, 'instance.players')} />}
      {tab === 'mods' && !!inst.modKind && can(me, 'instance.mods') && <ModsTab id={id} />}
      {tab === 'backups' && can(me, 'instance.backups') && <BackupsTab id={id} offline={status === 'offline'} />}
      {tab === 'schedules' && can(me, 'instance.schedules') && <SchedulesTab id={id} />}
      {tab === 'settings' && can(me, 'instance.create') && <SettingsTab inst={inst} offline={status === 'offline' || status === 'error'} canDelete={can(me, 'instance.delete')} me={me} onSaved={load} />}
    </div>
  )
}

function SettingsTab({ inst, offline, canDelete, me, onSaved }: { inst: Instance; offline: boolean; canDelete: boolean; me: Me; onSaved: () => void }) {
  const t = useT()
  const [f, setF] = useState({ name: inst.name, ramMb: inst.ramMb, port: inst.port, network: inst.network })
  const [confirm, setConfirm] = useState('')
  const save = async () => { try { await api('/instances/' + inst.id, { method: 'PATCH', body: f }); toast(t('files.saved')); onSaved() } catch (e) { toast(errMsg((e as ApiError).code), 'err') } }
  const del = async () => { try { await api('/instances/' + inst.id, { method: 'DELETE' }); go('/') } catch (e) { toast(errMsg((e as ApiError).code), 'err') } }
  return (
    <div className="max-w-xl space-y-5">
      <div className="card p-5 space-y-4">
        <h2 className="font-semibold">{t('set.general')}</h2>
        {!offline && <p className="text-sm text-warn">{t('set.offlineOnly')}</p>}
        <Field label={t('new.name')}><input className="input" value={f.name} disabled={!offline} onChange={(e) => setF({ ...f, name: e.target.value })} /></Field>
        <div className="grid sm:grid-cols-3 gap-4">
          <Field label={t('new.ram') + ' (MB)'}><input className="input" type="number" min={512} step={256} value={f.ramMb} disabled={!offline} onChange={(e) => setF({ ...f, ramMb: +e.target.value })} /></Field>
          <Field label={t('new.port')}><input className="input font-mono" type="number" value={f.port} disabled={!offline} onChange={(e) => setF({ ...f, port: +e.target.value })} /></Field>
          <Field label={t('new.network')}><input className="input" value={f.network} disabled={!offline} onChange={(e) => setF({ ...f, network: e.target.value.toLowerCase() })} /></Field>
        </div>
        <p className="text-sm text-ink-mute">{t('set.address')}: <span className="font-mono text-ink">{location.hostname}:{inst.port}</span>{inst.network && <> · <span className="font-mono text-ink">{inst.slug}:25565</span> ({inst.network})</>}</p>
        <button className="btn-primary" disabled={!offline} onClick={save}>{t('set.save')}</button>
      </div>
      {can(me, 'instance.domain') && <DomainCard id={inst.id} port={inst.port} />}
      {canDelete && (
        <div className="card border-danger/30 p-5 space-y-3">
          <h2 className="font-semibold text-danger">{t('set.danger')}</h2>
          <p className="text-sm text-ink-mute">{t('set.deleteConfirm')}</p>
          <input className="input" value={confirm} onChange={(e) => setConfirm(e.target.value)} placeholder={inst.name} />
          <button className="btn-danger" disabled={confirm !== inst.name || !offline} onClick={del}>{t('set.delete')}</button>
        </div>
      )}
    </div>
  )
}

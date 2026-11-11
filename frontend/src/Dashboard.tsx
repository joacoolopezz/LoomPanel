import { useState } from 'react'
import { Play, Plus, Square, Trash2, AlertTriangle } from 'lucide-react'
import { Instance, Me, api, can } from './api'
import { Console } from './Console'
import { ApiError } from './api'
import { errMsg, useT } from './i18n'
import { toast, useInstances, useLive } from './store'
import { Bar, Spark, StatusDot, fmtMB, Modal } from './ui'

export default function Dashboard({ me }: { me: Me }) {
  const t = useT()
  const { host, live, history } = useLive()
  const { list, reload } = useInstances()
  const [pick, setPick] = useState('')
  const [deleteConfirm, setDeleteConfirm] = useState<Instance | null>(null)

  const power = async (i: Instance, action: string) => {
    try { await api(`/instances/${i.id}/power`, { body: { action } }); reload() } catch (e) { toast(errMsg((e as ApiError).code), 'err') }
  }
  const deleteInstance = async (i: Instance) => {
    try { await api(`/instances/${i.id}`, { method: 'DELETE' }); reload() } catch (e) { toast(errMsg((e as ApiError).code), 'err') }
    setDeleteConfirm(null)
  }
  const memPct = host.memTotalMb ? ((host.memUsedMb ?? 0) / host.memTotalMb) * 100 : 0
  const diskPct = host.diskTotalGb ? ((host.diskUsedGb ?? 0) / host.diskTotalGb) * 100 : 0
  const online = Object.entries(live).flatMap(([id, l]) => l.players.map((p) => ({ p, id })))
  const nameOf = (id: string) => list?.find((x) => x.id === id)?.name ?? ''
  const consoleId = pick || list?.find((i) => i.status === 'running')?.id || list?.[0]?.id || ''
  const consoleInst = list?.find((i) => i.id === consoleId)

  return (
    <div className="space-y-6">
      <div className="grid gap-3 sm:grid-cols-3">
        <div className="card p-4 space-y-2">
          <div className="flex items-baseline justify-between"><span className="text-sm text-ink-mute">{t('dash.cpu')}</span><span className="text-xl font-semibold tabular-nums">{(host.cpu ?? 0).toFixed(0)}%</span></div>
          <Spark data={history.cpu} />
        </div>
        <div className="card p-4 space-y-2">
          <div className="flex items-baseline justify-between"><span className="text-sm text-ink-mute">{t('dash.ram')}</span><span className="text-xl font-semibold tabular-nums">{fmtMB(host.memUsedMb ?? 0)}<span className="text-sm text-ink-mute font-normal"> / {fmtMB(host.memTotalMb ?? 0)}</span></span></div>
          <Spark data={history.mem} />
        </div>
        <div className="card p-4 space-y-3">
          <div className="flex items-baseline justify-between"><span className="text-sm text-ink-mute">{t('dash.disk')}</span><span className="text-xl font-semibold tabular-nums">{(host.diskUsedGb ?? 0).toFixed(0)}<span className="text-sm text-ink-mute font-normal"> / {(host.diskTotalGb ?? 0).toFixed(0)} GB</span></span></div>
          <div className="pt-3"><Bar value={diskPct} /></div>
        </div>
      </div>

      <section>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-lg font-semibold">{t('dash.servers')}</h2>
          {can(me, 'instance.create') && <a href="#/servers/new" className="btn-primary btn-sm"><Plus size={15} />{t('srv.new')}</a>}
        </div>
        {list && list.length === 0 ? (
          <div className="card p-10 text-center">
            <p className="font-medium">{t('dash.empty')}</p>
            <p className="text-sm text-ink-mute mt-1 mb-4">{t('dash.emptyHint')}</p>
            {can(me, 'instance.create') && <a href="#/servers/new" className="btn-primary"><Plus size={16} />{t('srv.new')}</a>}
          </div>
        ) : (
          <div className="grid gap-3 md:grid-cols-2">
            {list?.map((i) => {
              const l = live[i.id]
              const on = i.status === 'running' || i.status === 'starting'
              return (
                <div key={i.id} className="card p-4 hover:border-ink-mute/30 transition-colors">
                  <div className="flex items-start justify-between gap-3">
                    <a href={`#/servers/${i.id}`} className="min-w-0">
                      <div className="font-medium truncate">{i.name}</div>
                      <div className="text-sm text-ink-mute capitalize">{i.type} {i.version}</div>
                    </a>
                    <StatusDot status={i.status} />
                  </div>
                  <div className="mt-4 flex items-center justify-between gap-3">
                    <div className="text-sm text-ink-mute tabular-nums">
                      {on && l ? `${l.cpu.toFixed(0)}% · ${fmtMB(l.memMb)} · ` : ''}{l?.players.length ?? 0} {t('tab.players').toLowerCase()}
                    </div>
                    {can(me, 'instance.power') && (i.status === 'offline' || on) && (
                      on ? <button className="btn-neutral btn-sm" onClick={() => power(i, 'stop')}><Square size={13} />{t('srv.stop')}</button>
                         : <button className="btn-primary btn-sm" onClick={() => power(i, 'start')}><Play size={13} />{t('srv.start')}</button>
                    )}
                    {can(me, 'instance.delete') && i.status === 'offline' && (
                      <button className="btn-neutral btn-sm text-danger hover:bg-danger/10" onClick={() => setDeleteConfirm(i)} title={t('set.delete')}>
                        <Trash2 size={13} />
                      </button>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </section>

      {list && list.length > 0 && (
        <div className="grid gap-6 lg:grid-cols-3">
          <section className="lg:col-span-2 space-y-3">
            <div className="flex items-center justify-between gap-3">
              <h2 className="text-lg font-semibold">{t('dash.console')}</h2>
              <select className="input !w-auto max-w-[55%]" value={consoleId} onChange={(e) => setPick(e.target.value)} aria-label={t('dash.pick')}>
                {list.map((i) => <option key={i.id} value={i.id}>{i.name}</option>)}
              </select>
            </div>
            {consoleInst && <Console key={consoleInst.id} id={consoleInst.id} status={consoleInst.status} canSend={can(me, 'instance.console')} height="h-64" />}
          </section>
          <section className="space-y-3">
            <h2 className="text-lg font-semibold">{t('dash.players')} <span className="text-ink-mute font-normal">{online.length}</span></h2>
            <div className="card divide-y divide-line">
              {online.length === 0 && <p className="p-4 text-sm text-ink-mute">{t('dash.noPlayers')}</p>}
              {online.map((o) => (
                <div key={o.id + o.p} className="flex items-center justify-between px-4 py-2.5 text-sm">
                  <span className="flex items-center gap-2.5"><img alt="" width={20} height={20} className="rounded" src={`data:image/svg+xml,${encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 8 8"><rect width="8" height="8" fill="#5D5C31"/><rect x="1" y="1" width="6" height="6" fill="#8C8B4A"/></svg>')}`} />{o.p}</span>
                  <span className="text-ink-mute truncate ml-3">{nameOf(o.id)}</span>
                </div>
              ))}
            </div>
          </section>
        </div>
      )}

      {deleteConfirm && (
        <Modal title={t('set.delete')} onClose={() => setDeleteConfirm(null)}>
          <div className="space-y-4">
            <div className="flex items-center gap-3 text-lg">
              <AlertTriangle className="text-warning" size={24} />
              <span>{t('set.delete')}</span>
            </div>
            <p className="text-ink-mute">{t('set.deleteConfirm').replace('{name}', deleteConfirm.name)}</p>
            <div className="flex justify-end gap-3 pt-2">
              <button className="btn-neutral" onClick={() => setDeleteConfirm(null)}>{t('common.cancel')}</button>
              <button className="btn-danger" onClick={() => deleteInstance(deleteConfirm)}>{t('set.delete')}</button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  )
}

import { useCallback, useEffect, useState } from 'react'
import { DatabaseBackup, Download, RotateCcw, Trash2 } from 'lucide-react'
import { ApiError, api } from './api'
import { errMsg, useT } from './i18n'
import { toast } from './store'
import { fmtBytes } from './ui'

type B = { id: string; kind: string; size: number; createdAt: number }

export function BackupsTab({ id, offline }: { id: string; offline: boolean }) {
  const t = useT()
  const [data, setData] = useState<{ busy: boolean; items: B[] } | null>(null)
  const base = `/instances/${id}/backups`
  const err = (e: unknown) => toast(errMsg((e as ApiError).code), 'err')
  const load = useCallback(() => api<{ busy: boolean; items: B[] }>(base).then(setData).catch(() => {}), [base])
  useEffect(() => { load(); const i = setInterval(load, 3000); return () => clearInterval(i) }, [load])

  const create = async () => { try { await api(base, { method: 'POST', body: {} }); toast(t('backups.started')); load() } catch (e) { err(e) } }
  const restore = async (b: B) => { if (!confirm(t('backups.restoreConfirm'))) return; try { await api(`${base}/${b.id}/restore`, { method: 'POST', body: {} }); toast(t('backups.restoring')); load() } catch (e) { err(e) } }
  const del = async (b: B) => { if (!confirm(t('backups.deleteConfirm'))) return; try { await api(`${base}/${b.id}`, { method: 'DELETE' }); load() } catch (e) { err(e) } }

  if (!data) return null
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-ink-mute max-w-lg">{t('backups.hint')}</p>
        <button className="btn-primary" disabled={data.busy} onClick={create}><DatabaseBackup size={16} />{data.busy ? t('backups.working') : t('backups.create')}</button>
      </div>
      {!offline && data.items.length > 0 && <p className="text-xs text-ink-mute">{t('backups.needOffline')}</p>}
      <div className="card divide-y divide-line">
        {data.items.length === 0 && <p className="px-4 py-6 text-sm text-ink-mute text-center">{t('backups.empty')}</p>}
        {data.items.map((b) => (
          <div key={b.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-sm">
            <div className="min-w-0">
              <div>{new Date(b.createdAt * 1000).toLocaleString()}</div>
              <div className="text-xs text-ink-mute">{t('backups.kind.' + b.kind)} · {fmtBytes(b.size)}</div>
            </div>
            <div className="flex gap-1.5">
              <a className="btn-neutral btn-sm" href={`/api${base}/${b.id}/download`} aria-label={t('files.download')} title={t('files.download')}><Download size={14} /></a>
              <button className="btn-neutral btn-sm" disabled={!offline || data.busy} onClick={() => restore(b)}><RotateCcw size={14} />{t('backups.restore')}</button>
              <button className="btn-danger btn-sm" onClick={() => del(b)} aria-label={t('files.delete')} title={t('files.delete')}><Trash2 size={14} /></button>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

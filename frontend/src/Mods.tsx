import { useCallback, useEffect, useState } from 'react'
import { Download, Package, Power, RefreshCw, Search, Trash2 } from 'lucide-react'
import { ApiError, api } from './api'
import { errMsg, useT } from './i18n'
import { toast } from './store'

type Hit = { id: string; title: string; description: string; icon: string; downloads: number; author: string }
type Item = { file: string; enabled: boolean; size: number; title: string; version: string; projectId: string }
type Upd = { projectId: string; title: string; current: string; latest: string }
const fmtN = (n: number) => (n >= 1e6 ? (n / 1e6).toFixed(1) + 'M' : n >= 1e3 ? (n / 1e3).toFixed(0) + 'k' : String(n))

export function ModsTab({ id }: { id: string }) {
  const t = useT()
  const base = `/instances/${id}/mods`
  const [items, setItems] = useState<Item[]>([])
  const [q, setQ] = useState('')
  const [hits, setHits] = useState<Hit[] | null>(null)
  const [busy, setBusy] = useState<Set<string>>(new Set())
  const [updates, setUpdates] = useState<Upd[] | null>(null)
  const err = (e: unknown) => toast(errMsg((e as ApiError).code), 'err')

  const loadItems = useCallback(() => api<{ items: Item[] }>(base).then((r) => setItems(r.items)).catch(err), [base])
  useEffect(() => { loadItems() }, [loadItems])
  useEffect(() => {
    const h = setTimeout(() => api<Hit[]>(`${base}/search?q=${encodeURIComponent(q)}`).then(setHits).catch((e) => { setHits([]); err(e) }), q ? 400 : 0)
    return () => clearTimeout(h)
  }, [q, base])

  const install = async (pid: string) => {
    setBusy((s) => new Set(s).add(pid))
    try { const r = await api<{ installed: string[] }>(`${base}/install`, { body: { projectId: pid } }); toast(r.installed.length ? `${t('mods.done')}: ${r.installed.join(', ')}` : t('mods.upToDate')); loadItems(); setUpdates((u) => u && u.filter((x) => x.projectId !== pid)) }
    catch (e) { err(e) } finally { setBusy((s) => { const n = new Set(s); n.delete(pid); return n }) }
  }
  const toggle = async (i: Item) => { try { await api(`${base}/toggle`, { body: { file: i.file, enabled: !i.enabled } }); loadItems() } catch (e) { err(e) } }
  const remove = async (i: Item) => { if (!confirm(t('mods.removeConfirm') + '\n' + i.file)) return; try { await api(`${base}/remove`, { body: { file: i.file } }); loadItems() } catch (e) { err(e) } }
  const check = async () => { try { setUpdates(await api<Upd[]>(`${base}/updates`)) } catch (e) { err(e) } }
  const installedIds = new Set(items.map((i) => i.projectId))

  return (
    <div className="space-y-8">
      <section className="space-y-3">
        <div className="relative max-w-xl">
          <Search size={16} className="absolute left-3.5 top-3 text-ink-mute" />
          <input className="input !pl-10" placeholder={t('mods.search')} value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
        <h2 className="font-medium">{q ? t('mods.results') : t('mods.popular')}</h2>
        {hits && hits.length === 0 && <p className="text-sm text-ink-mute">{t('mods.noResults')}</p>}
        <div className="grid gap-3 md:grid-cols-2">
          {hits?.map((h) => (
            <div key={h.id} className="card p-4 flex gap-3">
              {h.icon ? <img src={h.icon} alt="" width={48} height={48} loading="lazy" referrerPolicy="no-referrer" className="h-12 w-12 rounded-xl bg-raised object-cover shrink-0" />
                : <div className="h-12 w-12 rounded-xl bg-raised grid place-items-center shrink-0"><Package size={20} className="text-ink-mute" /></div>}
              <div className="min-w-0 flex-1">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0"><div className="font-medium truncate">{h.title}</div><div className="text-xs text-ink-mute truncate">{h.author} · {fmtN(h.downloads)} {t('mods.downloads')}</div></div>
                  <button className={installedIds.has(h.id) ? 'btn-neutral btn-sm shrink-0' : 'btn-primary btn-sm shrink-0'} disabled={busy.has(h.id)} onClick={() => install(h.id)}>
                    {busy.has(h.id) ? <RefreshCw size={13} className="animate-spin" /> : <Download size={13} />}{installedIds.has(h.id) ? t('mods.update') : t('mods.install')}
                  </button>
                </div>
                <p className="text-sm text-ink-mute mt-1.5 line-clamp-2">{h.description}</p>
              </div>
            </div>
          ))}
        </div>
        <p className="text-xs text-ink-mute">{t('mods.manual')}</p>
      </section>

      <section className="space-y-3">
        <div className="flex items-center justify-between gap-3">
          <h2 className="font-medium">{t('mods.installed')} <span className="text-ink-mute font-normal">{items.length}</span></h2>
          <button className="btn-neutral btn-sm" onClick={check}><RefreshCw size={13} />{t('mods.checkUpdates')}</button>
        </div>
        {updates && (updates.length === 0 ? <p className="text-sm text-olive-light">{t('mods.upToDate')}</p> : (
          <div className="card divide-y divide-line">
            {updates.map((u) => (
              <div key={u.projectId} className="flex items-center justify-between gap-3 px-4 py-2.5 text-sm">
                <span className="truncate">{u.title} <span className="text-ink-mute">{u.current} → {u.latest}</span></span>
                <button className="btn-primary btn-sm shrink-0" disabled={busy.has(u.projectId)} onClick={() => install(u.projectId)}>{t('mods.update')}</button>
              </div>
            ))}
          </div>
        ))}
        <div className="card divide-y divide-line">
          {items.length === 0 && <p className="px-4 py-4 text-sm text-ink-mute">{t('mods.none')}</p>}
          {items.map((i) => (
            <div key={i.file} className="flex items-center justify-between gap-3 px-4 py-2.5 text-sm">
              <div className={`min-w-0 ${i.enabled ? '' : 'opacity-50'}`}>
                <div className="truncate">{i.title || i.file}{!i.enabled && <span className="ml-2 text-xs text-warn">{t('mods.disabled')}</span>}</div>
                <div className="text-xs text-ink-mute truncate font-mono">{i.file}</div>
              </div>
              <div className="flex gap-1.5 shrink-0">
                <button className="btn-neutral btn-sm" onClick={() => toggle(i)} title={i.enabled ? t('mods.disable') : t('mods.enable')} aria-label={i.enabled ? t('mods.disable') : t('mods.enable')}><Power size={14} /></button>
                <button className="btn-danger btn-sm" onClick={() => remove(i)} title={t('mods.remove')} aria-label={t('mods.remove')}><Trash2 size={14} /></button>
              </div>
            </div>
          ))}
        </div>
        <p className="text-xs text-ink-mute">{t('mods.restartNote')}</p>
      </section>
    </div>
  )
}

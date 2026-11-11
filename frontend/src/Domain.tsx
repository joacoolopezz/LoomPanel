import { useCallback, useEffect, useState } from 'react'
import { Check, Copy, Globe, X } from 'lucide-react'
import { ApiError, api } from './api'
import { errMsg, useT } from './i18n'
import { toast } from './store'
import { Field } from './ui'

type Rec = { type: string; name: string; value: string; note: string }
type Info = { hostname: string; port: number; publicIp: string; records: Rec[] | null; cloudflare: boolean }
type Chk = { a: { ok: boolean; found: string[] | null }; srv: { needed: boolean; ok: boolean; found?: string[] } }

export function DomainCard({ id, port }: { id: string; port: number }) {
  const t = useT()
  const base = `/instances/${id}/domain`
  const [info, setInfo] = useState<Info | null>(null)
  const [host, setHost] = useState('')
  const [chk, setChk] = useState<Chk | null>(null)
  const err = (e: unknown) => toast(errMsg((e as ApiError).code), 'err')
  const load = useCallback(() => api<Info>(base).then((i) => { setInfo(i); setHost(i.hostname) }).catch(() => {}), [base])
  useEffect(() => { load() }, [load, port])

  const save = async () => { try { await api(base, { method: 'PUT', body: { hostname: host } }); setChk(null); load() } catch (e) { err(e) } }
  const check = async () => { try { setChk(await api<Chk>(base + '/check')) } catch (e) { err(e) } }
  const auto = async () => { try { await api(base + '/cloudflare', { body: {} }); toast(t('dom.autoDone')); setChk(null) } catch (e) { err(e) } }
  const copy = (s: string) => { navigator.clipboard?.writeText(s); toast(t('users.copied')) }
  const joinAs = info?.hostname ? info.hostname + (port === 25565 ? '' : '') : ''

  return (
    <div className="card p-5 space-y-4">
      <div><h2 className="font-semibold flex items-center gap-2"><Globe size={18} className="text-olive-light" />{t('dom.title')}</h2><p className="text-sm text-ink-mute mt-1">{t('dom.desc')}</p></div>
      <div className="flex gap-2 items-end">
        <div className="flex-1"><Field label={t('dom.hostname')}><input className="input font-mono" placeholder="mc.tudominio.com" value={host} onChange={(e) => setHost(e.target.value.toLowerCase())} /></Field></div>
        <button className="btn-primary" onClick={save} disabled={host === (info?.hostname ?? '')}>{t('dom.save')}</button>
      </div>
      {info?.hostname && info.records && (
        <div className="space-y-3 border-t border-line pt-4">
          <p className="text-sm">{t('dom.join')} <span className="font-mono text-olive-light">{joinAs}</span></p>
          <h3 className="text-sm font-medium">{t('dom.records')}</h3>
          {!info.publicIp && <p className="text-xs text-warn">{t('dom.ipUnknown')}</p>}
          <div className="space-y-2">
            {info.records.map((r) => (
              <div key={r.type} className="rounded-xl border border-line p-3 space-y-1.5 text-sm">
                <p className="text-xs text-ink-mute">{t('dom.' + (r.type === 'A' ? 'a' : 'srv'))}</p>
                {([['dom.type', r.type], ['dom.name', r.name], ['dom.value', r.value]] as const).map(([k, v]) => (
                  <div key={k} className="flex items-center justify-between gap-3"><span className="text-ink-mute shrink-0">{t(k)}</span>
                    <button className="font-mono text-[13px] text-right break-all hover:text-olive-light inline-flex items-center gap-1.5" onClick={() => copy(v)} title={t('dom.copy')}>{v}<Copy size={12} className="shrink-0 opacity-50" /></button></div>
                ))}
              </div>
            ))}
          </div>
          <p className="text-xs text-ink-mute">{t('dom.proxyNote')}</p>
          <div className="flex flex-wrap gap-2">
            <button className="btn-neutral btn-sm" onClick={check}>{t('dom.check')}</button>
            {info.cloudflare && <button className="btn-primary btn-sm" onClick={auto}>{t('dom.auto')}</button>}
          </div>
          {chk && (
            <div className="space-y-1.5 text-sm">
              <Row ok={chk.a.ok} label="A" found={chk.a.found} />
              {chk.srv.needed && <Row ok={chk.srv.ok} label="SRV" found={chk.srv.found} />}
              {(!chk.a.ok || !chk.srv.ok) && <p className="text-xs text-ink-mute">{t('dom.checkWait')}</p>}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function Row({ ok, label, found }: { ok: boolean; label: string; found?: string[] | null }) {
  const t = useT()
  return (
    <div className="flex items-center gap-2">
      {ok ? <Check size={15} className="text-olive-light" /> : <X size={15} className="text-warn" />}
      <span className="font-mono">{label}</span>
      <span className="text-ink-mute">{ok ? t('dom.checkOk') : found && found.length ? `${t('dom.found')}: ${found.join(', ')}` : ''}</span>
    </div>
  )
}

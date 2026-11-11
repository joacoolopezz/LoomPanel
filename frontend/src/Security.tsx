import { FormEvent, useCallback, useEffect, useState } from 'react'
import { Copy, KeyRound, Plus, ShieldCheck, Trash2 } from 'lucide-react'
import qrcode from 'qrcode-generator'
import { ApiError, Me, api } from './api'
import { errMsg, useT } from './i18n'
import { toast } from './store'
import { Field, Modal } from './ui'

const copy = (s: string, t: (k: string) => string) => { navigator.clipboard?.writeText(s); toast(t('users.copied')) }

function Qr({ value }: { value: string }) {
  const q = qrcode(0, 'M'); q.addData(value); q.make()
  return <div className="inline-block rounded-xl bg-white p-2 [&>svg]:block [&>svg]:h-44 [&>svg]:w-44" aria-label="QR" role="img" dangerouslySetInnerHTML={{ __html: q.createSvgTag({ cellSize: 4, margin: 0, scalable: true }) }} />
}

export function TwoFactor({ me, onChange }: { me: Me; onChange: () => void }) {
  const t = useT()
  const [setup, setSetup] = useState<{ secret: string; uri: string } | null>(null)
  const [code, setCode] = useState('')
  const [codes, setCodes] = useState<string[] | null>(null)
  const [disabling, setDisabling] = useState(false)
  const [pw, setPw] = useState('')
  const err = (e: unknown) => toast(errMsg((e as ApiError).code), 'err')

  const start = async () => { try { setSetup(await api('/me/2fa/setup', { body: {} })) } catch (e) { err(e) } }
  const enable = async (e: FormEvent) => {
    e.preventDefault()
    try { const r = await api<{ recoveryCodes: string[] }>('/me/2fa/enable', { body: { code } }); setCodes(r.recoveryCodes); setSetup(null); setCode(''); onChange() } catch (x) { err(x) }
  }
  const disable = async (e: FormEvent) => {
    e.preventDefault()
    try { await api('/me/2fa/disable', { body: { password: pw, code } }); setDisabling(false); setPw(''); setCode(''); onChange() } catch (x) { err(x) }
  }

  return (
    <div className="card p-5 space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div><h2 className="font-semibold flex items-center gap-2"><ShieldCheck size={18} className="text-olive-light" />{t('sec.2fa')}</h2><p className="text-sm text-ink-mute mt-1">{t('sec.2fa.desc')}</p></div>
        {me.totpEnabled
          ? <button className="btn-neutral btn-sm shrink-0" onClick={() => setDisabling(true)}>{t('sec.2fa.disable')}</button>
          : !setup && <button className="btn-primary btn-sm shrink-0" onClick={start}>{t('sec.2fa.enable')}</button>}
      </div>
      {me.totpEnabled && <p className="text-sm text-olive-light">✓ {t('sec.2fa.on')}</p>}
      {setup && (
        <form onSubmit={enable} className="space-y-4 border-t border-line pt-4">
          <p className="text-sm text-ink-mute">{t('sec.2fa.scan')}</p>
          <Qr value={setup.uri} />
          <Field label={t('sec.2fa.secret')}><input className="input font-mono text-sm" readOnly value={setup.secret} onFocus={(e) => e.target.select()} /></Field>
          <Field label={t('sec.2fa.confirm')}><input className="input font-mono tracking-widest max-w-[10rem]" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))} required /></Field>
          <button className="btn-primary" disabled={code.length !== 6}>{t('sec.2fa.enable')}</button>
        </form>
      )}
      {codes && (
        <Modal title={t('sec.2fa.codesTitle')} onClose={() => setCodes(null)}>
          <p className="text-sm text-ink-mute mb-3">{t('sec.2fa.codesHint')}</p>
          <div className="grid grid-cols-2 gap-2 font-mono text-sm mb-4">{codes.map((c) => <div key={c} className="rounded-lg bg-bg border border-line px-3 py-2 text-center">{c}</div>)}</div>
          <div className="flex gap-2"><button className="btn-neutral flex-1" onClick={() => copy(codes.join('\n'), t)}><Copy size={15} />{t('sec.2fa.copyCodes')}</button><button className="btn-primary flex-1" onClick={() => setCodes(null)}>{t('sec.done')}</button></div>
        </Modal>
      )}
      {disabling && (
        <Modal title={t('sec.2fa.disable')} onClose={() => setDisabling(false)}>
          <form onSubmit={disable} className="space-y-4">
            <p className="text-sm text-ink-mute">{t('sec.2fa.disableHint')}</p>
            <Field label={t('auth.password')}><input className="input" type="password" value={pw} onChange={(e) => setPw(e.target.value)} autoComplete="current-password" required /></Field>
            <Field label={t('auth.2fa.code')}><input className="input font-mono" value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" required /></Field>
            <div className="flex justify-end gap-2"><button type="button" className="btn-neutral" onClick={() => setDisabling(false)}>{t('common.cancel')}</button><button className="btn-danger">{t('sec.2fa.disable')}</button></div>
          </form>
        </Modal>
      )}
    </div>
  )
}

type Tok = { id: string; name: string; prefix: string; scope: string; createdAt: number; lastUsed: number; expiresAt: number }

export function Tokens({ me }: { me: Me }) {
  const t = useT()
  const [list, setList] = useState<Tok[]>([])
  const [adding, setAdding] = useState(false)
  const [f, setF] = useState({ name: '', scope: 'full', expiresDays: 0 })
  const [created, setCreated] = useState('')
  const load = useCallback(() => api<Tok[]>('/tokens').then(setList).catch(() => {}), [])
  useEffect(() => { load() }, [load])

  const create = async (e: FormEvent) => {
    e.preventDefault()
    try { const r = await api<{ token: string }>('/tokens', { body: f }); setCreated(r.token); setAdding(false); setF({ name: '', scope: 'full', expiresDays: 0 }); load() } catch (x) { toast(errMsg((x as ApiError).code), 'err') }
  }
  const revoke = async (tk: Tok) => { if (!confirm(tk.name + '?')) return; await api('/tokens/' + tk.id, { method: 'DELETE' }); load() }

  return (
    <div className="card p-5 space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div><h2 className="font-semibold flex items-center gap-2"><KeyRound size={18} className="text-olive-light" />{t('sec.tokens')}</h2><p className="text-sm text-ink-mute mt-1">{t('sec.tokens.desc')}</p></div>
        <button className="btn-primary btn-sm shrink-0" disabled={me.viaToken} onClick={() => setAdding(true)}><Plus size={14} />{t('sec.tokens.new')}</button>
      </div>
      {me.viaToken && <p className="text-sm text-warn">{t('sec.tokens.viaToken')}</p>}
      <div className="divide-y divide-line rounded-xl border border-line">
        {list.length === 0 && <p className="px-4 py-3 text-sm text-ink-mute">{t('sec.tokens.none')}</p>}
        {list.map((tk) => (
          <div key={tk.id} className="flex items-center justify-between gap-3 px-4 py-2.5 text-sm">
            <div className="min-w-0"><div className="truncate">{tk.name} <span className="text-xs text-ink-mute">· {t('sec.tokens.' + tk.scope)}</span></div>
              <div className="text-xs text-ink-mute font-mono">{tk.prefix}… · {t('sec.tokens.lastUsed')}: {tk.lastUsed ? new Date(tk.lastUsed * 1000).toLocaleDateString() : t('sched.never')}</div></div>
            <button className="btn-danger btn-sm" onClick={() => revoke(tk)} aria-label={t('files.delete')}><Trash2 size={14} /></button>
          </div>
        ))}
      </div>
      {adding && (
        <Modal title={t('sec.tokens.new')} onClose={() => setAdding(false)}>
          <form onSubmit={create} className="space-y-4">
            <Field label={t('sec.tokens.name')}><input className="input" value={f.name} maxLength={40} onChange={(e) => setF({ ...f, name: e.target.value })} required autoFocus /></Field>
            <Field label={t('sec.tokens.scope')}><select className="input" value={f.scope} onChange={(e) => setF({ ...f, scope: e.target.value })}><option value="full">{t('sec.tokens.full')}</option><option value="read">{t('sec.tokens.read')}</option></select></Field>
            <Field label={t('sec.tokens.expires')}><select className="input" value={f.expiresDays} onChange={(e) => setF({ ...f, expiresDays: +e.target.value })}>
              <option value={0}>{t('sec.tokens.never')}</option>{[30, 90, 365].map((d) => <option key={d} value={d}>{d} {t('sec.tokens.days')}</option>)}</select></Field>
            <div className="flex justify-end gap-2"><button type="button" className="btn-neutral" onClick={() => setAdding(false)}>{t('common.cancel')}</button><button className="btn-primary">{t('common.create')}</button></div>
          </form>
        </Modal>
      )}
      {created && (
        <Modal title={t('sec.tokens.new')} onClose={() => setCreated('')}>
          <div className="space-y-3">
            <p className="text-sm text-warn">{t('sec.tokens.created')}</p>
            <input className="input font-mono text-xs" readOnly value={created} onFocus={(e) => e.target.select()} />
            <button className="btn-primary w-full" onClick={() => copy(created, t)}><Copy size={15} />{t('users.copy')}</button>
            <p className="text-xs text-ink-mute">{t('sec.tokens.example')}</p>
            <pre className="rounded-xl bg-bg border border-line p-3 text-xs font-mono overflow-x-auto">{`curl -H "Authorization: Bearer ${created.slice(0, 7)}…" \\\n  ${location.origin}/api/instances`}</pre>
          </div>
        </Modal>
      )}
    </div>
  )
}

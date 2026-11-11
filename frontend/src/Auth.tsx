import { FormEvent, useEffect, useState } from 'react'
import { ApiError, api } from './api'
import { errMsg, useT } from './i18n'
import { Field } from './ui'
import { LangSwitch } from './Settings'

function Shell({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <main className="min-h-full flex items-center justify-center p-4">
      <div className="w-full max-w-sm">
        <div className="flex items-center justify-between mb-8">
          <div className="flex items-center gap-2.5">
            <span className="grid place-items-center h-9 w-9 rounded-xl bg-accent text-white font-semibold">L</span>
            <span className="font-semibold tracking-tight">LoomPanel</span>
          </div>
          <LangSwitch />
        </div>
        <h1 className="text-2xl font-semibold tracking-tight mb-6">{title}</h1>
        {children}
      </div>
    </main>
  )
}

export function Login({ onDone, setupNeeded, invite }: { onDone: () => void; setupNeeded: boolean; invite?: string }) {
  const t = useT()
  const [f, setF] = useState({ username: '', password: '', token: '' })
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [ticket, setTicket] = useState('')
  const [code, setCode] = useState('')
  const [inviteOk, setInviteOk] = useState<boolean | null>(invite ? null : true)

  useEffect(() => { if (invite) api('/invites/' + invite).then(() => setInviteOk(true)).catch(() => setInviteOk(false)) }, [invite])

  const submit = async (e: FormEvent) => {
    e.preventDefault(); setBusy(true); setErr('')
    try {
      if (ticket) await api('/login/2fa', { body: { ticket, code } })
      else if (invite) await api('/invites/' + invite, { body: { username: f.username, password: f.password } })
      else if (setupNeeded) await api('/setup', { body: f })
      else {
        const r = await api<{ needs2fa?: boolean; ticket?: string }>('/login', { body: { username: f.username, password: f.password } })
        if (r.needs2fa) { setTicket(r.ticket!); setBusy(false); return }
      }
      if (invite) location.hash = '/'
      onDone()
    } catch (x) { const c = (x as ApiError).code; setErr(errMsg(c)); if (c === 'ticket_expired' || c === 'too_many_attempts') { setTicket(''); setCode('') } } finally { setBusy(false) }
  }

  if (ticket) return (
    <Shell title={t('auth.2fa.title')}>
      <form onSubmit={submit} className="space-y-4">
        <p className="text-sm text-ink-mute">{t('auth.2fa.hint')}</p>
        <Field label={t('auth.2fa.code')}><input className="input font-mono tracking-widest" value={code} onChange={(e) => setCode(e.target.value)} autoComplete="one-time-code" inputMode="text" required autoFocus /></Field>
        {err && <p className="text-sm text-danger" role="alert">{err}</p>}
        <button className="btn-primary w-full" disabled={busy}>{t('auth.2fa.verify')}</button>
      </form>
    </Shell>
  )
  const title = invite ? t('auth.invite.title') : setupNeeded ? t('auth.setup.title') : t('auth.signin')
  if (invite && inviteOk === false) return <Shell title={t('auth.invite.title')}><p className="text-ink-mute">{t('auth.invite.bad')}</p></Shell>
  return (
    <Shell title={title}>
      <form onSubmit={submit} className="space-y-4">
        {setupNeeded && !invite && (
          <Field label={t('auth.setup.token')} hint={t('auth.setup.hint')}>
            <input className="input font-mono" value={f.token} onChange={(e) => setF({ ...f, token: e.target.value })} autoComplete="off" required />
          </Field>
        )}
        <Field label={t('auth.username')}><input className="input" value={f.username} onChange={(e) => setF({ ...f, username: e.target.value })} autoComplete="username" required autoFocus /></Field>
        <Field label={t('auth.password')}><input className="input" type="password" value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })} autoComplete={setupNeeded || invite ? 'new-password' : 'current-password'} required /></Field>
        {err && <p className="text-sm text-danger" role="alert">{err}</p>}
        <button className="btn-primary w-full" disabled={busy}>{invite ? t('auth.invite.btn') : setupNeeded ? t('auth.setup.btn') : t('auth.signin')}</button>
      </form>
    </Shell>
  )
}

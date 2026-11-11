import { useEffect, useState } from 'react'
import { Link2, Trash2, UserPlus } from 'lucide-react'
import { ApiError, Instance, api } from './api'
import { errMsg, useT } from './i18n'
import { toast } from './store'
import { Field, Modal } from './ui'

type U = { id: number; username: string; role: string; createdAt: number; instances: string[] }
const roles = ['admin', 'moderator', 'viewer']

function Access({ list, value, onChange }: { list: Instance[]; value: string[]; onChange: (v: string[]) => void }) {
  return (
    <div className="flex flex-wrap gap-2">
      {list.map((i) => {
        const on = value.includes(i.id)
        return <button type="button" key={i.id} aria-pressed={on} onClick={() => onChange(on ? value.filter((x) => x !== i.id) : [...value, i.id])}
          className={`h-8 px-3 rounded-lg text-sm border transition-colors ${on ? 'border-accent bg-accent/15' : 'border-line text-ink-mute hover:border-ink-mute/40'}`}>{i.name}</button>
      })}
    </div>
  )
}

export function UsersPage() {
  const t = useT()
  const [users, setUsers] = useState<U[]>([])
  const [insts, setInsts] = useState<Instance[]>([])
  const [inviting, setInviting] = useState(false)
  const load = () => api<U[]>('/users').then(setUsers)
  useEffect(() => { load(); api<Instance[]>('/instances').then(setInsts) }, [])
  const err = (e: unknown) => toast(errMsg((e as ApiError).code), 'err')
  const update = async (u: U, patch: Partial<U>) => { try { await api('/users/' + u.id, { method: 'PATCH', body: { role: u.role, instances: u.instances, ...patch } }); load() } catch (e) { err(e) } }
  const reset2fa = async (u: U) => { if (!confirm(t('users.reset2faConfirm'))) return; try { await api(`/users/${u.id}/2fa-reset`, { body: {} }); toast(t('sec.done')) } catch (e) { err(e) } }
  const del = async (u: U) => { if (!confirm(u.username + '?')) return; try { await api('/users/' + u.id, { method: 'DELETE' }); load() } catch (e) { err(e) } }

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between"><h1 className="text-2xl font-semibold tracking-tight">{t('nav.users')}</h1>
        <button className="btn-primary btn-sm" onClick={() => setInviting(true)}><UserPlus size={15} />{t('users.invite')}</button></div>
      <div className="space-y-3">
        {users.map((u) => (
          <div key={u.id} className="card p-4 space-y-3">
            <div className="flex items-center justify-between gap-3">
              <span className="font-medium truncate">{u.username}</span>
              <div className="flex items-center gap-2">
                <select className="input !h-8 !w-auto text-[13px]" value={u.role} onChange={(e) => update(u, { role: e.target.value })} aria-label={t('users.role')}>
                  {roles.map((r) => <option key={r} value={r}>{t('users.' + r)}</option>)}
                </select>
                <button className="btn-neutral btn-sm" onClick={() => reset2fa(u)}>{t('users.reset2fa')}</button>
                <button className="btn-danger btn-sm" onClick={() => del(u)} aria-label={t('files.delete')}><Trash2 size={14} /></button>
              </div>
            </div>
            {u.role === 'admin' ? <p className="text-sm text-ink-mute">{t('users.allNote')}</p> : (
              <div className="space-y-2"><span className="text-sm text-ink-mute">{t('users.access')}</span><Access list={insts} value={u.instances} onChange={(v) => update(u, { instances: v })} /></div>
            )}
          </div>
        ))}
      </div>
      {inviting && <InviteModal insts={insts} onClose={() => setInviting(false)} />}
    </div>
  )
}

function InviteModal({ insts, onClose }: { insts: Instance[]; onClose: () => void }) {
  const t = useT()
  const [role, setRole] = useState('moderator')
  const [access, setAccess] = useState<string[]>([])
  const [link, setLink] = useState('')
  const make = async () => {
    try { const r = await api<{ token: string }>('/invites', { body: { role, instances: role === 'admin' ? [] : access } }); setLink(`${location.origin}/#/invite/${r.token}`) }
    catch (e) { toast(errMsg((e as ApiError).code), 'err') }
  }
  return (
    <Modal title={t('users.invite')} onClose={onClose}>
      {link ? (
        <div className="space-y-3">
          <Field label={t('users.link')}><input className="input font-mono text-xs" readOnly value={link} onFocus={(e) => e.target.select()} /></Field>
          <button className="btn-primary w-full" onClick={() => { navigator.clipboard?.writeText(link); toast(t('users.copied')) }}><Link2 size={15} />{t('users.copy')}</button>
        </div>
      ) : (
        <div className="space-y-4">
          <Field label={t('users.role')}><select className="input" value={role} onChange={(e) => setRole(e.target.value)}>{roles.map((r) => <option key={r} value={r}>{t('users.' + r)}</option>)}</select></Field>
          {role !== 'admin' && <div className="space-y-2"><span className="label">{t('users.access')}</span><Access list={insts} value={access} onChange={setAccess} /></div>}
          <button className="btn-primary w-full" onClick={make}>{t('users.invite')}</button>
        </div>
      )}
    </Modal>
  )
}

export function ActivityPage() {
  const t = useT()
  const [rows, setRows] = useState<{ ts: number; user: string; action: string; target: string; ip: string }[]>([])
  useEffect(() => { api<typeof rows>('/audit?limit=200').then(setRows) }, [])
  return (
    <div className="space-y-5">
      <h1 className="text-2xl font-semibold tracking-tight">{t('nav.activity')}</h1>
      <div className="card overflow-x-auto">
        <table className="w-full text-sm">
          <thead><tr className="text-left text-ink-mute border-b border-line"><th className="p-3 font-normal">{t('activity.when')}</th><th className="p-3 font-normal">{t('activity.who')}</th><th className="p-3 font-normal">{t('activity.what')}</th><th className="p-3 font-normal hidden md:table-cell">IP</th></tr></thead>
          <tbody className="divide-y divide-line">
            {rows.map((r, i) => (
              <tr key={i}><td className="p-3 text-ink-mute whitespace-nowrap">{new Date(r.ts * 1000).toLocaleString()}</td><td className="p-3">{r.user}</td>
                <td className="p-3"><span className="font-mono text-[13px]">{r.action}</span>{r.target && <span className="text-ink-mute"> · {r.target}</span>}</td><td className="p-3 hidden md:table-cell text-ink-mute font-mono text-xs">{r.ip}</td></tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

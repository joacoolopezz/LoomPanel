import { FormEvent, useEffect, useState } from 'react'
import { ApiError, api } from './api'
import { errMsg, useT } from './i18n'
import { go, toast } from './store'
import { Field, fmtMB } from './ui'

type Sys = { dockerOk: boolean; maxRamMb: number; availRamMb: number; recommendedRamMb: number; nextPort: number; types: { id: string; name: string; available: boolean; beta: boolean }[] }

const JAVA_DISTRIBUTIONS = [
  { id: 'eclipse-temurin', name: 'Eclipse Temurin (Adoptium)', desc: 'Recommended - OpenJDK build by Adoptium' },
  { id: 'azul-zulu', name: 'Azul Zulu', desc: 'Commercial-grade OpenJDK with optimizations' },
  { id: 'graalvm', name: 'GraalVM Community', desc: 'High-performance polyglot runtime' },
  { id: 'amazon-corretto', name: 'Amazon Corretto', desc: 'Long-term supported OpenJDK by Amazon' },
  { id: 'microsoft', name: 'Microsoft Build of OpenJDK', desc: 'Long-term supported OpenJDK by Microsoft' },
] as const

export default function InstanceNew() {
  const t = useT()
  const [sys, setSys] = useState<Sys | null>(null)
  const [f, setF] = useState({ name: '', type: 'paper', version: '', ramMb: 2048, port: 0, network: '', acceptEula: false, java: 0, javaDistro: 'eclipse-temurin' })
  const [versions, setVersions] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [loadingV, setLoadingV] = useState(false)

  useEffect(() => { api<Sys>('/system').then((s) => { setSys(s); setF((p) => ({ ...p, ramMb: s.recommendedRamMb, port: s.nextPort })) }) }, [])
  useEffect(() => {
    setLoadingV(true); setVersions([])
    api<string[]>('/versions?type=' + f.type).then((v) => { setVersions(v); setF((p) => ({ ...p, version: v[0] || '', java: javaFor(v[0] || '') })) }).catch((e) => toast(errMsg(e.code), 'err')).finally(() => setLoadingV(false))
  }, [f.type])

  const maxRam = sys && sys.maxRamMb > 0 ? Math.max(512, sys.availRamMb) : 16384
  const autoJava = javaFor(f.version)
  const submit = async (e: FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { const i = await api<{ id: string }>('/instances', { body: f }); go(`/servers/${i.id}`) }
    catch (x) { toast(errMsg((x as ApiError).code), 'err') } finally { setBusy(false) }
  }

  return (
    <form onSubmit={submit} className="max-w-2xl space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">{t('srv.new')}</h1>
      {sys && !sys.dockerOk && <div className="card border-danger/50 p-4 text-sm text-danger">{t('err.docker')}</div>}
      <div className="card p-5 space-y-5">
        <Field label={t('new.name')}><input className="input" value={f.name} maxLength={40} onChange={(e) => setF({ ...f, name: e.target.value })} required autoFocus /></Field>
        <div>
          <span className="label">{t('new.type')}</span>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-2" role="radiogroup">
            {sys?.types.map((x) => (
              <button type="button" key={x.id} role="radio" aria-checked={f.type === x.id} disabled={!x.available} onClick={() => setF({ ...f, type: x.id, version: '', java: 0 })}
                className={`h-11 rounded-xl border text-sm transition-colors ${f.type === x.id ? 'border-accent bg-accent/15' : 'border-line bg-bg hover:border-ink-mute/40'} disabled:opacity-40 disabled:pointer-events-none`}>
                {x.name}{(!x.available || x.beta) && <span className="block text-[10px] leading-none text-ink-mute">{t(x.available ? 'new.beta' : 'new.soon')}</span>}
              </button>
            ))}
          </div>
        </div>
        <div className="grid sm:grid-cols-2 gap-4">
          <Field label={t('new.version')} hint={`${t('new.java')} (auto: Java ${autoJava})`}>
            <select className="input" value={f.version} onChange={(e) => { const v = e.target.value; setF({ ...f, version: v, java: javaFor(v) }) }} disabled={loadingV} required>
              {loadingV && <option>{t('common.loading')}</option>}
              {versions.map((v) => <option key={v}>{v}</option>)}
            </select>
          </Field>
          <Field label={t('new.javaVersion')} hint={f.java === autoJava ? t('new.javaAuto') : t('new.javaOverride')}>
            <select className="input" value={f.java} onChange={(e) => setF({ ...f, java: +e.target.value })}>
              <option value={8}>{8 >= autoJava ? '✓ ' : ''}Java 8 (1.16.5 and older)</option>
              <option value={17}>{17 >= autoJava ? '✓ ' : ''}Java 17 (1.17 - 1.20.4)</option>
              <option value={21}>{21 >= autoJava ? '✓ ' : ''}Java 21 (1.20.5+)</option>
            </select>
          </Field>
        </div>
        <Field label={t('new.javaDistro')} hint={t('new.javaDistroHint')}>
          <select className="input" value={f.javaDistro} onChange={(e) => setF({ ...f, javaDistro: e.target.value })}>
            {JAVA_DISTRIBUTIONS.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
          </select>
        </Field>
        <Field label={`${t('new.ram')}: ${fmtMB(f.ramMb)}`} hint={sys ? `${t('new.rec')}: ${fmtMB(sys.recommendedRamMb)}` : undefined}>
          <input type="range" min={512} max={maxRam} step={256} value={Math.min(f.ramMb, maxRam)} onChange={(e) => setF({ ...f, ramMb: +e.target.value })} className="w-full accent-[#8C2B32]" />
        </Field>
        <div className="grid sm:grid-cols-2 gap-4">
          <Field label={t('new.port')}><input className="input font-mono" type="number" min={1024} max={65535} value={f.port || ''} onChange={(e) => setF({ ...f, port: +e.target.value })} /></Field>
          <Field label={t('new.network')} hint={t('new.networkHint')}><input className="input" value={f.network} pattern="[a-z0-9\-]{0,30}" onChange={(e) => setF({ ...f, network: e.target.value.toLowerCase() })} placeholder="mi-red" /></Field>
        </div>
      </div>
      <label className="flex items-center gap-3 text-sm"><input type="checkbox" className="h-4 w-4 accent-[#8C2B32]" checked={f.acceptEula} onChange={(e) => setF({ ...f, acceptEula: e.target.checked })} />
        <span>{t('new.eula')} <a className="underline text-ink-mute" href="https://aka.ms/MinecraftEULA" target="_blank" rel="noreferrer">(link)</a></span></label>
      <button className="btn-primary" disabled={busy || !f.acceptEula || !f.version || !f.name.trim()}>{t('new.create')}</button>
    </form>
  )
}

function javaFor(v: string) {
  const p = v.split('.').map(Number); const minor = p[1] || 0, patch = p[2] || 0
  return minor > 20 || (minor === 20 && patch >= 5) ? 21 : minor >= 17 ? 17 : 8
}

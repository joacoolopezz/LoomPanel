import { Lang, setLang, useLang, useT } from './i18n'
import { SidebarPos, setSidebar, useSidebar } from './store'
import { Me, can } from './api'
import { Integrations, Notifications } from './Notifications'
import { Tokens, TwoFactor } from './Security'

export function LangSwitch() {
  const lang = useLang()
  return (
    <div className="inline-flex rounded-xl bg-raised p-1 text-sm" role="group" aria-label="Language">
      {(['es', 'en'] as Lang[]).map((l) => (
        <button key={l} onClick={() => setLang(l)} aria-pressed={lang === l} className={`px-3 h-7 rounded-lg transition-colors ${lang === l ? 'bg-accent text-white' : 'text-ink-mute hover:text-ink'}`}>{l.toUpperCase()}</button>
      ))}
    </div>
  )
}

export function SettingsPage({ me, onMeChange }: { me: Me; onMeChange: () => void }) {
  const t = useT()
  const pos = useSidebar()
  return (
    <div className="max-w-2xl space-y-4">
      <h1 className="text-2xl font-semibold tracking-tight mb-2">{t('nav.settings')}</h1>
      <div className="card p-5 flex items-center justify-between gap-4"><span>{t('prefs.language')}</span><LangSwitch /></div>
      <div className="card p-5 hidden md:flex items-center justify-between gap-4">
        <span>{t('prefs.sidebar')}</span>
        <div className="inline-flex rounded-xl bg-raised p-1 text-sm" role="group">
          {(['left', 'right', 'top'] as SidebarPos[]).map((p) => (
            <button key={p} onClick={() => setSidebar(p)} aria-pressed={pos === p} className={`px-3 h-7 rounded-lg transition-colors ${pos === p ? 'bg-accent text-white' : 'text-ink-mute hover:text-ink'}`}>{t('prefs.' + p)}</button>
          ))}
        </div>
      </div>
      <h2 className="text-lg font-semibold pt-4">{t('sec.title')}</h2>
      <TwoFactor me={me} onChange={onMeChange} />
      <Tokens me={me} />
      {can(me, 'notifications.manage') && <>
        <h2 className="text-lg font-semibold pt-4">{t('notif.title')}</h2>
        <Notifications />
        <h2 className="text-lg font-semibold pt-4">{t('int.title')}</h2>
        <Integrations />
      </>}
    </div>
  )
}

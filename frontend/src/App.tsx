import { useCallback, useEffect, useState } from 'react'
import { Activity, LayoutDashboard, LogOut, Menu, PanelLeftClose, PanelLeftOpen, Server, Settings as Cog, Users as UsersIcon, X } from 'lucide-react'
import { Me, api, can } from './api'
import { Login } from './Auth'
import { useT } from './i18n'
import { go, setCollapsed, useCollapsed, useRoute, useSidebar } from './store'
import { Toasts } from './ui'
import Dashboard from './Dashboard'
import InstanceNew from './InstanceNew'
import InstancePage from './InstancePage'
import { UsersPage, ActivityPage } from './Admin'
import { SettingsPage } from './Settings'

export default function App() {
  const route = useRoute()
  const [me, setMe] = useState<Me | null | undefined>(undefined)
  const [setup, setSetup] = useState(false)

  const load = useCallback(async () => {
    try { setMe(await api<Me>('/me')) } catch {
      setMe(null)
      try { setSetup((await api<{ needed: boolean }>('/setup')).needed) } catch { /* offline */ }
    }
  }, [])
  useEffect(() => { load() }, [load])

  if (me === undefined) return null
  const invite = route.startsWith('/invite/') ? route.slice(8) : undefined
  if (invite || !me) return <><Login onDone={load} setupNeeded={setup} invite={invite} /><Toasts /></>
  return <><Shell me={me} reload={load} route={route} onLogout={() => api('/logout', { method: 'POST' }).then(() => { setMe(null); location.hash = '/' })} /><Toasts /></>
}

function Shell({ me, route, onLogout, reload }: { me: Me; route: string; onLogout: () => void; reload: () => void }) {
  const t = useT()
  const pos = useSidebar()
  const collapsed = useCollapsed()
  const [drawer, setDrawer] = useState(false)
  useEffect(() => setDrawer(false), [route])

  const items = [
    { to: '/', label: t('nav.dashboard'), icon: LayoutDashboard },
    { to: '/servers/new', label: t('srv.new'), icon: Server, show: can(me, 'instance.create') },
    { to: '/users', label: t('nav.users'), icon: UsersIcon, show: can(me, 'users.manage') },
    { to: '/activity', label: t('nav.activity'), icon: Activity, show: can(me, 'users.manage') },
    { to: '/settings', label: t('nav.settings'), icon: Cog },
  ].filter((i) => i.show !== false)
  const active = (to: string) => (to === '/' ? route === '/' || route.startsWith('/servers/') && !route.startsWith('/servers/new') : route.startsWith(to))
  const horizontal = pos === 'top'

  const nav = (compact: boolean, row: boolean) => (
    <nav className={`flex ${row ? 'flex-row items-center gap-1' : 'flex-col gap-1'}`} aria-label="Main">
      {items.map((i) => (
        <a key={i.to} href={'#' + i.to} title={i.label} aria-current={active(i.to) ? 'page' : undefined}
          className={`flex items-center gap-3 rounded-xl h-10 text-sm transition-colors ${compact ? 'justify-center w-10' : 'px-3'} ${active(i.to) ? 'bg-accent/15 text-ink' : 'text-ink-mute hover:bg-raised hover:text-ink'}`}>
          <i.icon size={18} className={active(i.to) ? 'text-accent-hover' : ''} />
          {!compact && <span>{i.label}</span>}
        </a>
      ))}
    </nav>
  )
  const logo = (
    <a href="#/" className="flex items-center gap-2.5 font-semibold tracking-tight">
      <span className="grid place-items-center h-9 w-9 rounded-xl bg-accent text-white shrink-0">L</span>
    </a>
  )
  const userBox = (compact: boolean) => (
    <div className={`flex items-center gap-2 ${compact ? 'flex-col' : ''}`}>
      {!compact && <span className="text-sm text-ink-mute truncate max-w-[9rem]">{me.username}</span>}
      <button onClick={onLogout} title={t('nav.logout')} aria-label={t('nav.logout')} className="grid place-items-center h-10 w-10 rounded-xl text-ink-mute hover:bg-raised hover:text-ink"><LogOut size={18} /></button>
    </div>
  )

  return (
    <div className={`h-full flex ${horizontal ? 'flex-col' : pos === 'right' ? 'flex-row-reverse' : 'flex-row'}`}>
      {/* desktop sidebar / top bar */}
      {horizontal ? (
        <header className="hidden md:flex items-center gap-4 h-16 px-5 border-b border-line bg-card shrink-0">{logo}{nav(false, true)}<div className="ml-auto">{userBox(false)}</div></header>
      ) : (
        <aside className={`hidden md:flex flex-col justify-between shrink-0 bg-card p-3 transition-[width] duration-200 ${pos === 'right' ? 'border-l' : 'border-r'} border-line ${collapsed ? 'w-[68px] items-center' : 'w-60'}`}>
          <div className="space-y-6">
            <div className={`flex items-center ${collapsed ? 'flex-col gap-3' : 'justify-between px-1'}`}>
              {logo}
              {!collapsed && <span className="font-semibold tracking-tight -ml-16">LoomPanel</span>}
              <button onClick={() => setCollapsed(!collapsed)} className="grid place-items-center h-8 w-8 rounded-lg text-ink-mute hover:bg-raised" aria-label="Toggle sidebar">
                {collapsed ? <PanelLeftOpen size={16} /> : <PanelLeftClose size={16} />}
              </button>
            </div>
            {nav(collapsed, false)}
          </div>
          {userBox(collapsed)}
        </aside>
      )}
      {/* mobile top bar + drawer */}
      <header className="md:hidden flex items-center justify-between h-14 px-3 border-b border-line bg-card shrink-0" style={{ paddingTop: 'env(safe-area-inset-top)' }}>
        <button onClick={() => setDrawer(true)} aria-label="Menu" className="grid place-items-center h-10 w-10 rounded-xl hover:bg-raised"><Menu size={20} /></button>
        <span className="font-semibold tracking-tight">LoomPanel</span>
        <span className="w-10" />
      </header>
      {drawer && (
        <div className="md:hidden fixed inset-0 z-40 bg-black/70" onMouseDown={(e) => e.target === e.currentTarget && setDrawer(false)}>
          <div className="pop h-full w-72 max-w-[85vw] bg-card border-r border-line p-3 flex flex-col justify-between">
            <div className="space-y-6">
              <div className="flex items-center justify-between px-1">{logo}<button onClick={() => setDrawer(false)} aria-label="Close" className="grid place-items-center h-10 w-10 rounded-xl hover:bg-raised"><X size={18} /></button></div>
              {nav(false, false)}
            </div>
            {userBox(false)}
          </div>
        </div>
      )}
      <main className="flex-1 min-w-0 overflow-auto"><div className="mx-auto max-w-6xl p-4 md:p-8">{view(route, me, reload)}</div></main>
    </div>
  )
}

function view(route: string, me: Me, reload: () => void) {
  if (route === '/servers/new') return <InstanceNew />
  if (route.startsWith('/servers/')) return <InstancePage id={route.split('/')[2]} tab={route.split('/')[3] || 'console'} me={me} />
  if (route === '/users') return <UsersPage />
  if (route === '/activity') return <ActivityPage />
  if (route === '/settings') return <SettingsPage me={me} onMeChange={reload} />
  return <Dashboard me={me} />
}

export { go }

import { useEffect, useState, type ReactNode } from 'react'
import { useReady } from '../api/hooks'
import { Link, useRouter } from '../lib/router'
import { IncidentIcon, ThemeIcon, TopologyIcon } from './icons'
import { Logo } from './Logo'

type Theme = 'dark' | 'light' | 'system'

const THEME_KEY = 'argus.theme'

function readTheme(): Theme {
  try {
    const raw = localStorage.getItem(THEME_KEY)
    if (raw === 'dark' || raw === 'light') return raw
  } catch {
    /* private browsing, blocked storage: fall through to the OS setting */
  }
  return 'system'
}

function useTheme() {
  const [theme, setTheme] = useState<Theme>(readTheme)

  useEffect(() => {
    const root = document.documentElement
    if (theme === 'system') root.removeAttribute('data-theme')
    else root.setAttribute('data-theme', theme)
    try {
      if (theme === 'system') localStorage.removeItem(THEME_KEY)
      else localStorage.setItem(THEME_KEY, theme)
    } catch {
      /* the attribute is applied either way; only the memory is lost */
    }
  }, [theme])

  return { theme, setTheme }
}

export function Layout({ children }: { children: ReactNode }) {
  const { path } = useRouter()
  const { theme, setTheme } = useTheme()
  const ready = useReady()

  const onIncidents = path === '/' || path.startsWith('/incidents') || path.startsWith('/investigations')
  const onTopology = path.startsWith('/topology')

  return (
    <div className="shell">
      <nav className="sidebar" aria-label="Main">
        <div className="col" style={{ gap: 10 }}>
          <div className="row" style={{ gap: 11 }}>
            <Logo size={30} />
            <span style={{ fontSize: 16, lineHeight: '20px', fontWeight: 600, letterSpacing: '0.2em' }}>
              ARGUS
            </span>
          </div>
          <div className="row" style={{ gap: 8 }}>
            <span className="tag" style={{ letterSpacing: '0.14em' }}>
              AI SRE
            </span>
            <span className="tiny muted">always on</span>
          </div>
        </div>

        <div className="nav">
          <Link to="/" aria-current={onIncidents ? 'page' : undefined}>
            <IncidentIcon />
            Incidents
          </Link>
          <Link to="/topology" aria-current={onTopology ? 'page' : undefined}>
            <TopologyIcon />
            Topology
          </Link>
        </div>

        <div className="col" style={{ gap: 16, marginTop: 'auto' }}>
          <div className="col" style={{ gap: 4 }}>
            <div className="label">INVESTIGATION ENGINE</div>
            <div className="mono tiny">{ready.data?.engine ?? '…'}</div>
            {ready.data?.telemetry_providers && (
              <div className="tiny muted">telemetry: {ready.data.telemetry_providers.join(', ')}</div>
            )}
          </div>

          <div className="col" style={{ gap: 4 }}>
            <div className="label">CATALOG</div>
            <div className="small">
              {ready.data ? `${ready.data.services} services · ${ready.data.dependencies} dependencies` : '…'}
            </div>
            <div className="mono tiny muted">{ready.data?.catalog_source ?? ''}</div>
          </div>

          <button
            type="button"
            className="btn btn-sm"
            style={{ justifyContent: 'flex-start' }}
            onClick={() => setTheme(theme === 'light' ? 'dark' : theme === 'dark' ? 'system' : 'light')}
          >
            <ThemeIcon />
            {theme === 'system' ? 'Theme: system' : theme === 'dark' ? 'Theme: dark' : 'Theme: light'}
          </button>

          {ready.data?.version && <div className="tiny dim">argus {ready.data.version}</div>}
        </div>
      </nav>

      <main className="content">{children}</main>
    </div>
  )
}

/*
 * A tiny History-API router.
 *
 * The app has three routes and one path parameter. A routing library would be
 * more code than this, plus a dependency that has to be kept patched, so the
 * 60 lines below do the job: pushState navigation, back/forward support, and
 * links that behave like links (middle-click and cmd-click still open tabs).
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'

export interface Location {
  path: string
  query: URLSearchParams
}

interface RouterValue extends Location {
  navigate: (to: string, opts?: { replace?: boolean }) => void
}

const RouterContext = createContext<RouterValue | null>(null)

function read(): Location {
  return {
    path: window.location.pathname,
    query: new URLSearchParams(window.location.search),
  }
}

export function RouterProvider({ children }: { children: ReactNode }) {
  const [loc, setLoc] = useState<Location>(read)

  useEffect(() => {
    const onPop = () => setLoc(read())
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])

  const navigate = useCallback((to: string, opts?: { replace?: boolean }) => {
    if (opts?.replace) {
      window.history.replaceState(null, '', to)
    } else {
      window.history.pushState(null, '', to)
    }
    setLoc(read())
    window.scrollTo(0, 0)
  }, [])

  const value = useMemo<RouterValue>(
    () => ({ path: loc.path, query: loc.query, navigate }),
    [loc.path, loc.query, navigate],
  )

  return <RouterContext.Provider value={value}>{children}</RouterContext.Provider>
}

export function useRouter(): RouterValue {
  const ctx = useContext(RouterContext)
  if (!ctx) throw new Error('useRouter must be used inside RouterProvider')
  return ctx
}

/** Link renders a real anchor, so keyboard and modifier clicks keep working. */
export function Link({
  to,
  children,
  className,
  ...rest
}: { to: string; children: ReactNode; className?: string } & Omit<
  React.AnchorHTMLAttributes<HTMLAnchorElement>,
  'href'
>) {
  const { navigate } = useRouter()
  return (
    <a
      href={to}
      className={className}
      onClick={(e) => {
        // Leave the browser to handle anything that is not a plain left click.
        if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) {
          return
        }
        e.preventDefault()
        navigate(to)
      }}
      {...rest}
    >
      {children}
    </a>
  )
}

/**
 * match returns the captured segment when path looks like `<prefix>/<id>`.
 * Only the one shape the app needs; anything fancier belongs in a library.
 */
export function match(path: string, prefix: string): string | null {
  if (!path.startsWith(prefix + '/')) return null
  const rest = path.slice(prefix.length + 1)
  if (!rest || rest.includes('/')) return null
  return decodeURIComponent(rest)
}

'use client'

import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { usePathname, useRouter } from 'next/navigation'
import { authApi, AuthUser } from './api'

const LOGIN_PATH = '/login'

interface AuthContextValue {
  user: AuthUser | null
  // True only until the very first /api/auth/me check resolves. Later,
  // pathname-triggered rechecks (see below) update `user` silently in the
  // background rather than flipping this back to true -- otherwise every
  // client-side navigation would flash a loading spinner for no reason.
  initializing: boolean
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const router = useRouter()
  const [user, setUser] = useState<AuthUser | null>(null)
  const [initializing, setInitializing] = useState(true)

  const checkAuth = useCallback(async () => {
    try {
      const me = await authApi.me()
      setUser(me)
      // Already logged in and sitting on /login (typed the URL directly,
      // or navigated back to it) -- nothing to do there, send them in.
      if (pathname === LOGIN_PATH) {
        router.replace('/')
      }
    } catch {
      setUser(null)
      // Don't redirect away from /login itself -- that's the one page
      // that's supposed to be reachable with no session.
      if (pathname !== LOGIN_PATH) {
        router.replace(LOGIN_PATH)
      }
    } finally {
      setInitializing(false)
    }
  }, [pathname, router])

  useEffect(() => {
    checkAuth()
    // Deliberately re-runs on every route change, not just once on mount --
    // a cheap, local request that catches a session expiring or being
    // revoked mid-visit within one navigation, rather than only surfacing
    // as a confusing 401 on whatever action the user happens to take next.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pathname])

  const logout = useCallback(async () => {
    try {
      await authApi.logout()
    } finally {
      setUser(null)
      router.replace(LOGIN_PATH)
    }
  }, [router])

  // The login page renders immediately regardless of auth state -- either
  // showing the form (not logged in) or briefly, until checkAuth's success
  // branch above redirects away (already logged in). Every other route
  // shows a spinner instead of its real content until `user` is populated
  // -- covers both the very first load (initializing) and the moment
  // right after a successful login, where the route has already changed
  // to '/' but the resulting re-check hasn't resolved yet. Either way, a
  // null user here means a redirect to /login is either already in
  // flight or about to be, so there's nothing worth rendering underneath.
  if (pathname !== LOGIN_PATH && !user) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="animate-spin rounded-full h-32 w-32 border-b-2 border-blue-500"></div>
      </div>
    )
  }

  return (
    <AuthContext.Provider value={{ user, initializing, logout }}>
      {children}
    </AuthContext.Provider>
  )
}

// Safe to call from /login too -- user will just be null there until a
// successful login redirects away.
export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return ctx
}

'use client'

import { createContext, useContext, useEffect, useState } from 'react'

// Shared "is there something unsaved" signal so globally-rendered components
// (Navigation, Footer -- both mounted once in layout.tsx and persisted across
// every page) can find out about a page-local "you have undownloaded data"
// warning that only the individual page (issue/page.tsx, sign-csr/page.tsx)
// knows about. Next.js App Router has no built-in "confirm before route
// change" hook (unlike the old Pages Router's router.events), so this fills
// that gap for click-driven <Link> navigation specifically. beforeunload
// (tab close / refresh / typed URL) is handled separately by each page --
// this context only needs to cover in-app link clicks.
interface NavigationGuardContextValue {
  message: string | null
  setMessage: (message: string | null) => void
}

const NavigationGuardContext = createContext<NavigationGuardContextValue | null>(null)

export function NavigationGuardProvider({ children }: { children: React.ReactNode }) {
  const [message, setMessage] = useState<string | null>(null)
  return (
    <NavigationGuardContext.Provider value={{ message, setMessage }}>
      {children}
    </NavigationGuardContext.Provider>
  )
}

function useNavigationGuardContext() {
  const ctx = useContext(NavigationGuardContext)
  if (!ctx) {
    throw new Error('useNavigationGuardContext must be used within a NavigationGuardProvider')
  }
  return ctx
}

// For Navigation / Footer: read the currently-registered warning (if any) so
// a click on one of their links can confirm() before letting the route
// change proceed.
export function useNavigationGuardMessage() {
  return useNavigationGuardContext().message
}

// For pages that have something to lose (Issue Certificate, Sign CSR):
// register a warning message while it applies, and clear it (pass null)
// once it no longer does, or on unmount. Only one page is ever mounted at a
// time under the App Router, so there's no need to track multiple
// registrations -- the latest call wins. Registration happens in an effect
// (not during render) since it updates state owned by a different
// component (the provider higher up the tree).
export function useRegisterNavigationGuard(message: string | null) {
  const { setMessage } = useNavigationGuardContext()

  useEffect(() => {
    setMessage(message)
    return () => setMessage(null)
  }, [message, setMessage])
}

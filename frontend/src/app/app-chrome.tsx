'use client'

import { usePathname } from 'next/navigation'
import Navigation from './navigation'
import Footer from './footer'

// The login page is deliberately chrome-free: no point showing nav links
// to pages that will just 401/redirect before you've signed in, and a
// bare, focused login screen reads better than one wrapped in a full app
// shell. Every other route gets the normal Navigation + Footer.
export default function AppChrome({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const showChrome = pathname !== '/login'

  return (
    <>
      {showChrome && <Navigation />}
      <div className="flex-1">{children}</div>
      {showChrome && <Footer />}
    </>
  )
}

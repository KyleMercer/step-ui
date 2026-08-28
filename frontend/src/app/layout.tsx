import type { Metadata } from 'next'
import './globals.css'
import { Toaster } from 'react-hot-toast'
import Navigation from './navigation'
import Footer from './footer'
import { NavigationGuardProvider } from '@/lib/navigation-guard'

// Deliberately using the system font stack (via Tailwind's default font-sans)
// instead of next/font/google's Inter. next/font/google fetches font metadata
// from fonts.googleapis.com at BUILD time, which means every `docker build`
// silently requires outbound internet access to Google — a bad fit for a
// homelab CA tool that should build the same whether or not that's reachable.
export const metadata: Metadata = {
  title: 'Step-CA Web UI',
  description: 'Web interface for Step-CA certificate management',
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en">
      <body className="font-sans flex flex-col min-h-screen">
        <NavigationGuardProvider>
          <Navigation />
          <div className="flex-1">{children}</div>
          <Footer />
          <Toaster position="top-right" />
        </NavigationGuardProvider>
      </body>
    </html>
  )
}

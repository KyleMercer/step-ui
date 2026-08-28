'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { Shield, FileText, List, Settings, Home } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useNavigationGuardMessage } from '@/lib/navigation-guard'

const navigation = [
  { name: 'Dashboard', href: '/', icon: Home },
  { name: 'Issue Certificate', href: '/issue', icon: Shield },
  { name: 'Sign CSR', href: '/sign-csr', icon: FileText },
  { name: 'Inventory', href: '/inventory', icon: List },
  { name: 'Settings', href: '/settings', icon: Settings },
]

export default function Navigation() {
  const pathname = usePathname()
  const guardMessage = useNavigationGuardMessage()

  // Issue Certificate / Sign CSR register a warning here while they have
  // undownloaded content -- without this, these persistent top-bar links
  // were the one way to leave those pages with no warning at all (only
  // their own in-page "Back to Dashboard" / "Cancel" links were guarded).
  const confirmLeaveIfGuarded = (e: React.MouseEvent) => {
    if (guardMessage && !confirm(guardMessage)) {
      e.preventDefault()
    }
  }

  return (
    <nav className="bg-white shadow-sm border-b border-gray-200">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex justify-between h-16">
          <div className="flex">
            <div className="flex-shrink-0 flex items-center">
              <Shield className="h-8 w-8 text-blue-600" />
              <span className="ml-2 text-xl font-bold text-gray-900">Step-CA Web UI</span>
            </div>
            <div className="hidden sm:ml-6 sm:flex sm:space-x-8">
              {navigation.map((item) => {
                const Icon = item.icon
                return (
                  <Link
                    key={item.name}
                    href={item.href}
                    onClick={confirmLeaveIfGuarded}
                    className={cn(
                      pathname === item.href
                        ? 'border-blue-500 text-blue-600'
                        : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300',
                      'inline-flex items-center px-1 pt-1 border-b-2 text-sm font-medium'
                    )}
                  >
                    <Icon className="h-4 w-4 mr-2" />
                    {item.name}
                  </Link>
                )
              })}
            </div>
          </div>
        </div>
      </div>
    </nav>
  )
}

import Link from 'next/link'

// A single, simple build number on every page, baked in at build time (see
// Dockerfile) -- just enough to glance at any screen and confirm which
// build you're looking at. Full detail (frontend/backend/Node/Go versions,
// commits, build dates) lives on the Settings page instead, so this stays
// a one-line footer rather than a wall of text repeated on every screen.
export default function Footer() {
  const version = process.env.NEXT_PUBLIC_APP_VERSION || 'unknown'

  return (
    <footer className="mt-auto border-t border-gray-200 bg-white">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-3">
        <p className="text-xs text-gray-400 text-center">
          step-ui v{version} &middot;{' '}
          <Link href="/settings" className="hover:text-gray-600">
            system info
          </Link>
        </p>
      </div>
    </footer>
  )
}

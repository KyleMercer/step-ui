// A single, simple build number on every page, baked in at build time (see
// Dockerfile) -- just enough to glance at any screen and confirm which
// build you're looking at. Full detail (frontend/backend/Node/Go versions,
// commits, build dates) lives on the Settings page instead (reachable from
// the top navigation bar), so this stays a one-line footer rather than a
// wall of text repeated on every screen.
//
// No links here (the earlier "system info" link back to Settings was
// removed), so this can go back to being a plain server component -- no
// navigation-guard wiring needed when there's nothing to click.
//
// THREE_SPACES holds three non-breaking-space characters (character code
// 160), not three plain ASCII space characters: HTML collapses runs of
// ordinary whitespace down to a single space when rendering, but a
// non-breaking space is exempt from that collapsing -- built with
// fromCharCode rather than a literal character in this file so it can't
// silently get normalized back down to plain spaces by an editor/tool.
const THREE_SPACES = String.fromCharCode(160, 160, 160)

export default function Footer() {
  const version = process.env.NEXT_PUBLIC_APP_VERSION || 'unknown'

  return (
    <footer className="mt-auto border-t border-gray-200 bg-white">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-3">
        <p className="text-xs text-gray-400 text-center">
          step-ui{THREE_SPACES}Build#{version}
        </p>
      </div>
    </footer>
  )
}

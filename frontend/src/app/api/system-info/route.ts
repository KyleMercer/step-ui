import { NextResponse } from 'next/server'

// Runs server-side inside the frontend container, so (unlike the browser)
// it can read process.version directly and any env var without needing the
// NEXT_PUBLIC_ prefix. Backs the "System Information" section on the
// Settings page -- the footer's build number is the quick glance, this is
// the full detail: frontend version/commit/build date plus the Node.js and
// Next.js versions actually running it.
export async function GET() {
  let nextVersion = 'unknown'
  try {
    // Resolved from node_modules at request time, so this is the exact
    // version actually installed in the image -- not just the semver range
    // in package.json.
    nextVersion = require('next/package.json').version
  } catch {
    // Standalone output doesn't always trace next's own package.json;
    // "unknown" is a safe fallback rather than crashing this endpoint.
  }

  return NextResponse.json({
    version: process.env.NEXT_PUBLIC_APP_VERSION || 'unknown',
    commit: process.env.NEXT_PUBLIC_APP_COMMIT || 'unknown',
    build_date: process.env.NEXT_PUBLIC_APP_BUILD_DATE || 'unknown',
    node_version: process.version,
    next_version: nextVersion,
  })
}

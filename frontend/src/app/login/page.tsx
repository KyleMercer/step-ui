'use client'

import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useRouter } from 'next/navigation'
import { authApi, AuthConfig, oidcLoginUrl } from '@/lib/api'
import { Shield, LogIn } from 'lucide-react'

// The backend sends the browser back here as /login?sso_error=<code> when
// an SSO sign-in fails; the specific reason is in the backend's log.
const SSO_ERROR_MESSAGES: Record<string, string> = {
  sso_unavailable: "Couldn't reach the identity provider. Try again, or sign in with a local account below.",
  access_denied: 'The identity provider refused the sign-in (you may not be allowed to use this application).',
  not_authorized: "You signed in, but you're not in a group that's allowed to use step-ui. Ask an admin to add you.",
  username_conflict: 'Your SSO username matches an existing step-ui account, so it was not signed in. Ask an admin.',
  state_mismatch: 'That sign-in attempt could not be verified. Please try again.',
  state_expired: 'That sign-in attempt expired. Please try again.',
  too_many_attempts: 'Too many sign-ins in progress. Wait a minute and try again.',
  sso_failed: 'Single sign-on failed. Please try again.',
  server_error: 'Single sign-on failed because of a server error. Please try again.',
}

interface FormData {
  username: string
  password: string
}

export default function LoginPage() {
  const router = useRouter()
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [authConfig, setAuthConfig] = useState<AuthConfig | null>(null)

  useEffect(() => {
    // Read the SSO error once, then drop it from the address bar so a
    // refresh doesn't show it again. window.location rather than
    // useSearchParams: avoids needing a Suspense boundary for one value.
    const params = new URLSearchParams(window.location.search)
    const ssoError = params.get('sso_error')
    if (ssoError) {
      setError(SSO_ERROR_MESSAGES[ssoError] || SSO_ERROR_MESSAGES.sso_failed)
      window.history.replaceState(null, '', window.location.pathname)
    }

    // If this fails (backend down, older backend), just show the local form.
    authApi.config().then(setAuthConfig).catch(() => setAuthConfig(null))
  }, [])

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<FormData>()

  const onSubmit = async (data: FormData) => {
    setError(null)
    setLoading(true)
    try {
      await authApi.login(data.username, data.password)
      // Changing the route re-runs AuthProvider's session check, which
      // picks up the cookie this login just set and renders the app --
      // no separate "refresh my auth state" call needed here.
      router.replace('/')
    } catch (err: any) {
      setError(err.response?.data?.error || 'Login failed. Check your username and password.')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 px-4">
      <div className="max-w-sm w-full bg-white rounded-lg shadow p-8">
        <div className="flex flex-col items-center mb-6">
          <Shield className="h-10 w-10 text-blue-600" />
          <h1 className="mt-2 text-xl font-bold text-gray-900">Step-CA Web UI</h1>
          <p className="text-sm text-gray-500">Sign in to continue</p>
        </div>

        {authConfig?.oidc_enabled && (
          <>
            {/* A plain link, not a button handler: SSO is a full-page
                trip to the backend, then the identity provider, and back. */}
            <a
              href={oidcLoginUrl}
              className="w-full flex items-center justify-center gap-2 py-2 px-4 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-blue-600 hover:bg-blue-700"
            >
              <LogIn className="h-4 w-4" />
              Sign in with {authConfig.oidc_provider_name || 'SSO'}
            </a>
            <div className="my-6 flex items-center">
              <div className="flex-grow border-t border-gray-200" />
              <span className="mx-3 text-xs uppercase tracking-wide text-gray-400">or use a local account</span>
              <div className="flex-grow border-t border-gray-200" />
            </div>
          </>
        )}

        {error && authConfig?.oidc_enabled && <p className="mb-4 text-sm text-red-600">{error}</p>}

        <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
          <div>
            <label htmlFor="username" className="block text-sm font-medium text-gray-700">
              Username
            </label>
            <input
              {...register('username', { required: 'Username is required' })}
              id="username"
              type="text"
              autoComplete="username"
              autoFocus
              className="mt-1 block w-full border-gray-300 rounded-md shadow-sm focus:ring-blue-500 focus:border-blue-500 sm:text-sm"
            />
            {errors.username && (
              <p className="mt-1 text-sm text-red-600">{errors.username.message}</p>
            )}
          </div>

          <div>
            <label htmlFor="password" className="block text-sm font-medium text-gray-700">
              Password
            </label>
            <input
              {...register('password', { required: 'Password is required' })}
              id="password"
              type="password"
              autoComplete="current-password"
              className="mt-1 block w-full border-gray-300 rounded-md shadow-sm focus:ring-blue-500 focus:border-blue-500 sm:text-sm"
            />
            {errors.password && (
              <p className="mt-1 text-sm text-red-600">{errors.password.message}</p>
            )}
          </div>

          {error && !authConfig?.oidc_enabled && <p className="text-sm text-red-600">{error}</p>}

          <button
            type="submit"
            disabled={loading}
            className={
              authConfig?.oidc_enabled
                ? 'w-full flex justify-center py-2 px-4 border border-gray-300 rounded-md shadow-sm text-sm font-medium text-gray-700 bg-white hover:bg-gray-50 disabled:opacity-50 disabled:cursor-not-allowed'
                : 'w-full flex justify-center py-2 px-4 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed'
            }
          >
            {loading ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
      </div>
    </div>
  )
}

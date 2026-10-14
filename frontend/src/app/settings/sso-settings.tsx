'use client'

// Single sign-on (OIDC) settings card for the Settings page. Admin-only --
// the backend independently 403s these calls for a viewer.

import { useEffect, useMemo, useState } from 'react'
import { toast } from 'react-hot-toast'
import { Copy, KeyRound, CheckCircle, XCircle, ChevronDown, ChevronRight, PlugZap } from 'lucide-react'
import { oidcCallbackUrl, ssoSettingsApi, SSOSettings, SSOTestResult } from '@/lib/api'
import { useRegisterNavigationGuard } from '@/lib/navigation-guard'

const UNSAVED_WARNING = 'You have unsaved single sign-on settings. Leave without saving?'

const inputClass =
  'mt-1 block w-full border-gray-300 rounded-md shadow-sm focus:ring-blue-500 focus:border-blue-500 sm:text-sm'

// Everything except the secret-state marker, for change detection.
function comparable(s: SSOSettings, newSecret: string) {
  const { client_secret, active, ...rest } = s
  void client_secret
  void active
  return JSON.stringify({ ...rest, newSecret })
}

function Field({
  id,
  label,
  required,
  help,
  children,
}: {
  id: string
  label: string
  required?: boolean
  help?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div>
      <label htmlFor={id} className="block text-sm font-medium text-gray-700">
        {label}
        {required && <span className="text-red-600"> *</span>}
      </label>
      {children}
      {help && <p className="mt-1 text-xs text-gray-500">{help}</p>}
    </div>
  )
}

export default function SSOSettingsCard() {
  const [saved, setSaved] = useState<SSOSettings | null>(null)
  const [draft, setDraft] = useState<SSOSettings | null>(null)
  const [newSecret, setNewSecret] = useState('')
  const [replacingSecret, setReplacingSecret] = useState(false)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ ok: boolean; text: string; detail?: SSOTestResult } | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)

  useEffect(() => {
    ssoSettingsApi
      .get()
      .then((s) => {
        setSaved(s)
        setDraft(s)
        if (s.trust_ca_root || s.extra_ca_pem || s.groups_claim !== 'groups' || s.username_claim !== 'preferred_username' || s.scopes !== 'openid profile email') {
          setShowAdvanced(true)
        }
      })
      .catch((err) => setLoadError(err.response?.data?.error || 'Failed to load single sign-on settings'))
  }, [])

  const dirty = useMemo(
    () => !!saved && !!draft && comparable(saved, '') !== comparable(draft, newSecret.trim()),
    [saved, draft, newSecret]
  )
  useRegisterNavigationGuard(dirty ? UNSAVED_WARNING : null)

  if (loadError) {
    return (
      <div className="bg-white rounded-lg shadow p-6">
        <h2 className="text-lg font-medium text-gray-900">Single Sign-On</h2>
        <p className="mt-2 text-sm text-red-700">{loadError}</p>
      </div>
    )
  }
  if (!draft || !saved) {
    return null
  }

  const set = <K extends keyof SSOSettings>(key: K, value: SSOSettings[K]) =>
    setDraft((d) => (d ? { ...d, [key]: value } : d))

  const secretState = saved.client_secret // 'set' | 'unset' | 'undecryptable'
  const editingSecret = secretState !== 'set' || replacingSecret

  const save = async () => {
    setSaving(true)
    try {
      const result = await ssoSettingsApi.save({ ...draft, client_secret: newSecret.trim() })
      setSaved(result)
      setDraft(result)
      setNewSecret('')
      setReplacingSecret(false)
      setTestResult(null)
      toast.success(
        result.enabled ? 'Single sign-on settings saved. The next sign-in uses them.' : 'Single sign-on settings saved. SSO is off.'
      )
    } catch (err: any) {
      toast.error(err.response?.data?.error || 'Failed to save single sign-on settings', { duration: 8000 })
    } finally {
      setSaving(false)
    }
  }

  const discard = () => {
    setDraft(saved)
    setNewSecret('')
    setReplacingSecret(false)
  }

  const test = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const r = await ssoSettingsApi.test()
      const warnings = r.warnings && r.warnings.length ? ` Note: ${r.warnings.join('; ')}.` : ''
      setTestResult({ ok: true, text: r.message + warnings, detail: r })
    } catch (err: any) {
      setTestResult({ ok: false, text: err.response?.data?.error || 'Test failed' })
    } finally {
      setTesting(false)
    }
  }

  const copy = (text: string) => {
    navigator.clipboard.writeText(text)
    toast.success('Copied')
  }

  const status = saved.active ? (
    <span className="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium bg-green-50 text-green-700">On</span>
  ) : saved.enabled ? (
    <span className="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium bg-amber-50 text-amber-700">
      On, but not running (see backend log)
    </span>
  ) : (
    <span className="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium bg-gray-100 text-gray-600">Off</span>
  )

  return (
    <div className="bg-white rounded-lg shadow">
      <div className="px-6 py-4 border-b border-gray-200 flex items-start justify-between gap-4">
        <div>
          <h2 className="text-lg font-medium text-gray-900 flex items-center gap-2">
            <KeyRound className="h-5 w-5 text-gray-400" />
            Single Sign-On (OIDC) {status}
          </h2>
          <p className="mt-1 text-sm text-gray-500">
            Let people sign in through Authentik or another OpenID Connect provider. Local accounts keep working
            regardless, so you can always get in if the provider is down.
          </p>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={draft.enabled}
          aria-label="Single sign-on on or off"
          onClick={() => set('enabled', !draft.enabled)}
          className={`relative mt-1 inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors ${
            draft.enabled ? 'bg-green-600' : 'bg-gray-300'
          }`}
        >
          <span
            className={`inline-block h-5 w-5 transform rounded-full bg-white shadow transition ${
              draft.enabled ? 'translate-x-5' : 'translate-x-0'
            }`}
          />
        </button>
      </div>

      <div className="p-6 space-y-5">
        {/* Redirect URI to register at the provider */}
        <div className="rounded-md bg-blue-50 px-4 py-3">
          <p className="text-sm text-blue-900">
            Register this as the <strong>redirect URI</strong> at your provider (Authentik: Strict, Authorization):
          </p>
          <div className="mt-2 flex items-center gap-2">
            <code className="flex-1 text-sm bg-white rounded border border-blue-100 px-2 py-1 break-all">{oidcCallbackUrl}</code>
            <button
              onClick={() => copy(oidcCallbackUrl)}
              className="inline-flex items-center px-2 py-1 border border-blue-200 rounded-md text-sm text-blue-700 bg-white hover:bg-blue-100"
              title="Copy"
            >
              <Copy className="h-4 w-4" />
            </button>
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Field
            id="sso-issuer"
            label="Issuer URL"
            required
            help="Exactly as the provider shows it. Authentik: https://<host>/application/o/<slug>/ (with the trailing slash)."
          >
            <input
              id="sso-issuer"
              className={inputClass}
              value={draft.issuer_url}
              onChange={(e) => set('issuer_url', e.target.value)}
              placeholder="https://sso.example.com/application/o/step-ui/"
            />
          </Field>
          <Field id="sso-name" label="Button label" help={`Shown as "Sign in with ${draft.provider_name || 'SSO'}".`}>
            <input
              id="sso-name"
              className={inputClass}
              value={draft.provider_name}
              onChange={(e) => set('provider_name', e.target.value)}
              placeholder="Authentik"
            />
          </Field>
          <Field id="sso-client" label="Client ID" required>
            <input id="sso-client" className={inputClass} value={draft.client_id} onChange={(e) => set('client_id', e.target.value)} />
          </Field>
          <Field id="sso-secret" label="Client secret" required help="Stored encrypted and never shown again.">
            {!editingSecret ? (
              <div className="mt-1 flex items-center gap-3">
                <span className="flex-1 px-3 py-2 rounded-md bg-gray-50 border border-gray-200 text-sm text-gray-500">•••••••• saved</span>
                <button type="button" className="text-sm text-blue-600 hover:text-blue-800" onClick={() => setReplacingSecret(true)}>
                  Replace
                </button>
              </div>
            ) : (
              <div className="mt-1 flex items-center gap-3">
                <input
                  id="sso-secret"
                  type="password"
                  autoComplete="new-password"
                  className="block w-full border-gray-300 rounded-md shadow-sm focus:ring-blue-500 focus:border-blue-500 sm:text-sm"
                  value={newSecret}
                  onChange={(e) => setNewSecret(e.target.value)}
                />
                {secretState === 'set' && (
                  <button
                    type="button"
                    className="text-sm text-gray-600 hover:text-gray-800"
                    onClick={() => {
                      setReplacingSecret(false)
                      setNewSecret('')
                    }}
                  >
                    Cancel
                  </button>
                )}
              </div>
            )}
            {secretState === 'undecryptable' && !newSecret && (
              <p className="mt-1 text-xs text-red-700">Saved, but can&apos;t be decrypted with the current settings key. Re-enter it.</p>
            )}
          </Field>
          <div className="sm:col-span-2">
            <Field id="sso-redirect" label="Redirect URI" required help="Must match the provider's redirect URI exactly.">
              <div className="flex items-center gap-3">
                <input
                  id="sso-redirect"
                  className={inputClass}
                  value={draft.redirect_url}
                  onChange={(e) => set('redirect_url', e.target.value)}
                  placeholder={oidcCallbackUrl}
                />
                {draft.redirect_url !== oidcCallbackUrl && (
                  <button
                    type="button"
                    onClick={() => set('redirect_url', oidcCallbackUrl)}
                    className="mt-1 shrink-0 text-sm text-blue-600 hover:text-blue-800"
                  >
                    Use the address above
                  </button>
                )}
              </div>
            </Field>
          </div>
          <Field id="sso-admin" label="Admin group" help="Members get full access. Case-sensitive.">
            <input
              id="sso-admin"
              className={inputClass}
              value={draft.admin_group}
              onChange={(e) => set('admin_group', e.target.value)}
              placeholder="stepui-admins"
            />
          </Field>
          <Field id="sso-viewer" label="Viewer group" help="Members get read-only access. Anyone in neither group is refused.">
            <input
              id="sso-viewer"
              className={inputClass}
              value={draft.viewer_group}
              onChange={(e) => set('viewer_group', e.target.value)}
              placeholder="stepui-viewers"
            />
          </Field>
        </div>

        {/* Advanced */}
        <div>
          <button
            type="button"
            onClick={() => setShowAdvanced((v) => !v)}
            className="inline-flex items-center text-sm font-medium text-gray-700 hover:text-gray-900"
          >
            {showAdvanced ? <ChevronDown className="h-4 w-4 mr-1" /> : <ChevronRight className="h-4 w-4 mr-1" />}
            Advanced
          </button>
          {showAdvanced && (
            <div className="mt-4 space-y-4">
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                <Field id="sso-groupsclaim" label="Groups claim">
                  <input
                    id="sso-groupsclaim"
                    className={inputClass}
                    value={draft.groups_claim}
                    onChange={(e) => set('groups_claim', e.target.value)}
                  />
                </Field>
                <Field id="sso-userclaim" label="Username claim">
                  <input
                    id="sso-userclaim"
                    className={inputClass}
                    value={draft.username_claim}
                    onChange={(e) => set('username_claim', e.target.value)}
                  />
                </Field>
                <Field id="sso-scopes" label="Scopes">
                  <input id="sso-scopes" className={inputClass} value={draft.scopes} onChange={(e) => set('scopes', e.target.value)} />
                </Field>
              </div>
              <label className="flex items-start gap-3">
                <input
                  type="checkbox"
                  className="mt-1 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                  checked={draft.trust_ca_root}
                  onChange={(e) => set('trust_ca_root', e.target.checked)}
                />
                <span>
                  <span className="block text-sm font-medium text-gray-700">Trust this CA&apos;s root for the provider</span>
                  <span className="block text-xs text-gray-500">
                    Only needed if the provider&apos;s certificate was issued by your step-ca. Publicly trusted certificates work
                    without it.
                  </span>
                </span>
              </label>
              <Field
                id="sso-pem"
                label="Extra CA certificates (optional)"
                help="PEM certificates to trust for the provider, for a certificate from some other private CA."
              >
                <textarea
                  id="sso-pem"
                  rows={4}
                  className={`${inputClass} font-mono text-xs`}
                  value={draft.extra_ca_pem}
                  onChange={(e) => set('extra_ca_pem', e.target.value)}
                  placeholder="-----BEGIN CERTIFICATE-----"
                />
              </Field>
            </div>
          )}
        </div>

        {testResult && (
          <div
            className={`rounded-md px-4 py-3 text-sm flex items-start gap-2 ${
              testResult.ok ? 'bg-green-50 text-green-800' : 'bg-red-50 text-red-800'
            }`}
          >
            {testResult.ok ? <CheckCircle className="h-4 w-4 mt-0.5 shrink-0" /> : <XCircle className="h-4 w-4 mt-0.5 shrink-0" />}
            <span className="break-words">{testResult.text}</span>
          </div>
        )}
      </div>

      <div className="px-6 py-4 border-t border-gray-200 flex items-center justify-between gap-3">
        <button
          onClick={test}
          disabled={testing || dirty || !saved.issuer_url}
          title={dirty ? 'Save your changes first' : 'Check the saved settings against the provider (no sign-in)'}
          className="inline-flex items-center px-3 py-2 border border-gray-300 shadow-sm text-sm font-medium rounded-md text-gray-700 bg-white hover:bg-gray-50 disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <PlugZap className={`h-4 w-4 mr-2 ${testing ? 'animate-pulse' : ''}`} />
          Test connection
        </button>
        <div className="flex items-center gap-3">
          {dirty && <span className="text-sm text-gray-500">Unsaved changes</span>}
          <button
            onClick={discard}
            disabled={!dirty || saving}
            className="px-4 py-2 border border-gray-300 rounded-md text-sm font-medium text-gray-700 bg-white hover:bg-gray-50 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            Discard
          </button>
          <button
            onClick={save}
            disabled={!dirty || saving}
            className="px-4 py-2 rounded-md text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}

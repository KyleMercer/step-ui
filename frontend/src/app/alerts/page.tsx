'use client'

// Expiry alerts: settings, destinations, status and history. Self-contained
// (with lib/alerts-api.ts) so the feature can be removed cleanly. Admins
// edit; viewers get the same page read-only, never seeing secret values.

import { useCallback, useEffect, useMemo, useState } from 'react'
import Link from 'next/link'
import { toast } from 'react-hot-toast'
import { Bell, BellOff, Plus, Trash2, Send, RefreshCw, CheckCircle, XCircle, AlertTriangle, Clock } from 'lucide-react'
import { useAuth } from '@/lib/auth-context'
import { useRegisterNavigationGuard } from '@/lib/navigation-guard'
import { formatDate } from '@/lib/utils'
import {
  alertsApi,
  AlertConfig,
  AlertField,
  AlertStatus,
  AlertTypeSchema,
  SecretState,
} from '@/lib/alerts-api'

const UNSAVED_WARNING = 'You have unsaved alert settings. Leave without saving?'

interface DraftDestination {
  localKey: string
  id: string // '' until saved
  name: string
  type: string
  enabled: boolean
  settings: Record<string, string>
  secretStates: Record<string, SecretState>
  newSecrets: Record<string, string>
  clearSecrets: string[]
  replacing: Record<string, boolean>
}

interface Draft {
  enabled: boolean
  thresholdsText: string
  step_ui_url: string
  trust_ca_root: boolean
  time_zone: string
  destinations: DraftDestination[]
}

let localKeyCounter = 0
const nextLocalKey = () => `d${++localKeyCounter}`

function toDraft(cfg: AlertConfig): Draft {
  return {
    enabled: cfg.enabled,
    thresholdsText: (cfg.thresholds || []).join(', '),
    step_ui_url: cfg.step_ui_url || '',
    trust_ca_root: cfg.trust_ca_root,
    time_zone: cfg.time_zone || '',
    destinations: (cfg.destinations || []).map((d) => ({
      localKey: d.id,
      id: d.id,
      name: d.name,
      type: d.type,
      enabled: d.enabled,
      settings: { ...d.settings },
      secretStates: { ...d.secrets },
      newSecrets: {},
      clearSecrets: [],
      replacing: {},
    })),
  }
}

// Everything that would be sent on save -- used to detect unsaved changes
// (UI-only state like "replacing" is left out).
function saveShape(d: Draft) {
  return {
    enabled: d.enabled,
    thresholds: d.thresholdsText,
    url: d.step_ui_url,
    trust: d.trust_ca_root,
    tz: d.time_zone,
    dests: d.destinations.map((x) => ({
      id: x.id, name: x.name, type: x.type, enabled: x.enabled, settings: x.settings,
      newSecrets: Object.fromEntries(Object.entries(x.newSecrets).filter(([, v]) => v !== '')),
      clear: x.clearSecrets,
    })),
  }
}

function parseThresholds(text: string): number[] | null {
  const parts = text.split(/[\s,]+/).filter(Boolean)
  const nums = parts.map((p) => Number(p))
  if (nums.length === 0 || nums.some((n) => !Number.isInteger(n) || n < 1 || n > 365)) return null
  return nums
}

function daysText(daysLeft: number, expired: boolean) {
  if (expired) return daysLeft === 0 ? 'expired today' : `expired ${-daysLeft} day${daysLeft === -1 ? '' : 's'} ago`
  if (daysLeft === 0) return 'expires within a day'
  return `expires in ${daysLeft} day${daysLeft === 1 ? '' : 's'}`
}

function levelLabel(level: number) {
  return level === 0 ? 'Expired' : `${level}-day warning`
}

function relativeTime(iso: string | null) {
  if (!iso) return 'never'
  const mins = Math.round((Date.now() - new Date(iso).getTime()) / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins} min ago`
  const hrs = Math.round(mins / 60)
  if (hrs < 24) return `${hrs} h ago`
  return formatDate(iso)
}

const inputClass =
  'mt-1 block w-full border-gray-300 rounded-md shadow-sm focus:ring-blue-500 focus:border-blue-500 sm:text-sm disabled:bg-gray-50 disabled:text-gray-500'

export default function AlertsPage() {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'

  const [schema, setSchema] = useState<AlertTypeSchema[]>([])
  const [baseline, setBaseline] = useState<Draft | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [status, setStatus] = useState<AlertStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [checking, setChecking] = useState(false)
  const [testing, setTesting] = useState<string | null>(null)
  const [addType, setAddType] = useState('')

  const loadStatus = useCallback(async () => {
    try {
      setStatus(await alertsApi.status())
    } catch {
      // status is secondary; the page still works without it
    }
  }, [])

  useEffect(() => {
    ;(async () => {
      try {
        const [sch, cfg] = await Promise.all([alertsApi.schema(), alertsApi.config()])
        setSchema(sch)
        const d = toDraft(cfg)
        setBaseline(d)
        setDraft(d)
        setAddType(sch[0]?.type || '')
      } catch (err: any) {
        toast.error(err.response?.data?.error || 'Failed to load alert settings')
      } finally {
        setLoading(false)
      }
      loadStatus()
    })()
  }, [loadStatus])

  const dirty = useMemo(
    () => !!draft && !!baseline && JSON.stringify(saveShape(draft)) !== JSON.stringify(saveShape(baseline)),
    [draft, baseline]
  )

  useRegisterNavigationGuard(dirty ? UNSAVED_WARNING : null)
  useEffect(() => {
    if (!dirty) return
    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault()
    }
    window.addEventListener('beforeunload', handler)
    return () => window.removeEventListener('beforeunload', handler)
  }, [dirty])

  const schemaFor = (type: string) => schema.find((s) => s.type === type)

  const browserTimeZone = useMemo(() => {
    try {
      return Intl.DateTimeFormat().resolvedOptions().timeZone
    } catch {
      return ''
    }
  }, [])
  const timeZones = useMemo(() => {
    try {
      return (Intl as any).supportedValuesOf('timeZone') as string[]
    } catch {
      return []
    }
  }, [])

  const update = (fn: (d: Draft) => Draft) => setDraft((d) => (d ? fn(d) : d))
  const updateDest = (key: string, fn: (d: DraftDestination) => DraftDestination) =>
    update((d) => ({ ...d, destinations: d.destinations.map((x) => (x.localKey === key ? fn(x) : x)) }))

  const addDestination = () => {
    const s = schemaFor(addType)
    if (!s) return
    const settings: Record<string, string> = {}
    const secretStates: Record<string, SecretState> = {}
    s.fields.forEach((f) => {
      if (f.secret) secretStates[f.key] = 'unset'
      else if (f.default !== undefined) settings[f.key] = f.default
    })
    update((d) => ({
      ...d,
      destinations: [
        ...d.destinations,
        {
          localKey: nextLocalKey(), id: '', name: s.label, type: s.type, enabled: true, settings, secretStates,
          newSecrets: {}, clearSecrets: [], replacing: {},
        },
      ],
    }))
  }

  const save = async () => {
    if (!draft) return
    const thresholds = parseThresholds(draft.thresholdsText)
    if (!thresholds) {
      toast.error('Thresholds must be whole numbers of days between 1 and 365, e.g. 30, 7, 1')
      return
    }
    setSaving(true)
    try {
      const saved = await alertsApi.save({
        enabled: draft.enabled,
        thresholds,
        step_ui_url: draft.step_ui_url,
        trust_ca_root: draft.trust_ca_root,
        time_zone: draft.time_zone,
        destinations: draft.destinations.map((x) => ({
          id: x.id,
          name: x.name,
          type: x.type,
          enabled: x.enabled,
          settings: x.settings,
          secrets: Object.fromEntries(Object.entries(x.newSecrets).filter(([, v]) => v.trim() !== '')),
          clear_secrets: x.clearSecrets,
        })),
      })
      const d = toDraft(saved)
      setBaseline(d)
      setDraft(d)
      toast.success('Alert settings saved')
      loadStatus()
    } catch (err: any) {
      toast.error(err.response?.data?.error || 'Failed to save alert settings')
    } finally {
      setSaving(false)
    }
  }

  const checkNow = async () => {
    setChecking(true)
    try {
      const res = await alertsApi.checkNow()
      toast.success(res.summary, { duration: 6000 })
    } catch (err: any) {
      toast.error(err.response?.data?.error || 'Check failed')
    } finally {
      setChecking(false)
      loadStatus()
    }
  }

  const test = async (dest: DraftDestination) => {
    setTesting(dest.localKey)
    try {
      await alertsApi.test(dest.id)
      toast.success(`Test alert sent to ${dest.name}`)
    } catch (err: any) {
      toast.error(err.response?.data?.error || 'Test failed', { duration: 10000 })
    } finally {
      setTesting(null)
      loadStatus()
    }
  }

  if (loading || !draft) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="animate-spin rounded-full h-32 w-32 border-b-2 border-blue-500"></div>
      </div>
    )
  }

  const readOnly = !isAdmin

  return (
    <div className="min-h-screen bg-gray-50">
      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-8 pb-28">
        <div className="mb-8">
          <Link href="/" className="text-blue-600 hover:text-blue-500 text-sm font-medium">
            ← Back to Dashboard
          </Link>
          <h1 className="mt-4 text-3xl font-bold text-gray-900">Expiry Alerts</h1>
          <p className="mt-2 text-gray-600">
            Get warned before certificates expire, and when they do, by Pushover, MQTT, webhook or email.
          </p>
          {readOnly && (
            <p className="mt-2 text-sm text-gray-500">You can view these settings; only admins can change them.</p>
          )}
        </div>

        <div className="space-y-8">
          {/* Status */}
          <div className="bg-white rounded-lg shadow">
            <div className="px-6 py-5 flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                {draft.enabled ? (
                  <Bell className="h-6 w-6 text-green-600 mt-0.5" />
                ) : (
                  <BellOff className="h-6 w-6 text-gray-400 mt-0.5" />
                )}
                <div>
                  <h2 className="text-lg font-medium text-gray-900">
                    Alerts are {baseline?.enabled ? 'on' : 'off'}
                    {dirty && draft.enabled !== baseline?.enabled && (
                      <span className="ml-2 text-sm font-normal text-amber-600">
                        (turning {draft.enabled ? 'on' : 'off'} when you save)
                      </span>
                    )}
                  </h2>
                  <p className="mt-1 text-sm text-gray-500">
                    Checked every {status?.check_interval_minutes ?? 60} minutes. Last check:{' '}
                    {relativeTime(status?.last_check ?? null)}
                    {status?.last_result ? ` — ${status.last_result}` : ''}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-3 shrink-0">
                {isAdmin && (
                  <button
                    onClick={checkNow}
                    disabled={checking || dirty}
                    title={dirty ? 'Save your changes first' : 'Run a check now (even if alerts are off)'}
                    className="inline-flex items-center px-3 py-2 border border-gray-300 shadow-sm text-sm font-medium rounded-md text-gray-700 bg-white hover:bg-gray-50 disabled:opacity-50 disabled:cursor-not-allowed"
                  >
                    <RefreshCw className={`h-4 w-4 mr-2 ${checking ? 'animate-spin' : ''}`} />
                    Check now
                  </button>
                )}
                <button
                  type="button"
                  role="switch"
                  aria-checked={draft.enabled}
                  aria-label="Alerts on or off"
                  disabled={readOnly}
                  onClick={() => update((d) => ({ ...d, enabled: !d.enabled }))}
                  className={`relative inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors disabled:cursor-not-allowed ${
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
            </div>

            {/* What alerts are currently about */}
            <div className="border-t border-gray-200 px-6 py-4">
              <h3 className="text-sm font-medium text-gray-900">Certificates within the alert window</h3>
              {!status || status.upcoming.length === 0 ? (
                <p className="mt-2 text-sm text-gray-500">None right now.</p>
              ) : (
                <ul className="mt-2 divide-y divide-gray-100">
                  {status.upcoming.map((u) => (
                    <li key={u.cert_id} className="py-2 flex items-center justify-between text-sm">
                      <span className="font-medium text-gray-900">{u.cn}</span>
                      <span className={u.expired ? 'text-red-700 font-semibold' : u.days_left <= 7 ? 'text-red-600' : 'text-yellow-600'}>
                        {daysText(u.days_left, u.expired)}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>

          {/* When */}
          <div className="bg-white rounded-lg shadow">
            <div className="px-6 py-4 border-b border-gray-200">
              <h2 className="text-lg font-medium text-gray-900">When to alert</h2>
            </div>
            <div className="p-6 space-y-5">
              <div>
                <label htmlFor="thresholds" className="block text-sm font-medium text-gray-700">
                  Days before expiry
                </label>
                <input
                  id="thresholds"
                  className={inputClass}
                  value={draft.thresholdsText}
                  disabled={readOnly}
                  onChange={(e) => update((d) => ({ ...d, thresholdsText: e.target.value }))}
                  placeholder="30, 7, 1"
                />
                <p className="mt-1 text-xs text-gray-500">
                  One alert as each certificate reaches each of these, plus one when it expires. Renewing a certificate
                  starts its warnings over.
                </p>
              </div>
              <div>
                <label htmlFor="stepuiurl" className="block text-sm font-medium text-gray-700">
                  step-ui address (for links in alerts)
                </label>
                <input
                  id="stepuiurl"
                  className={inputClass}
                  value={draft.step_ui_url}
                  disabled={readOnly}
                  onChange={(e) => update((d) => ({ ...d, step_ui_url: e.target.value }))}
                  placeholder="https://step-ui.example.com"
                />
              </div>
              <div>
                <label htmlFor="timezone" className="block text-sm font-medium text-gray-700">
                  Time zone for dates in alerts
                </label>
                <div className="flex items-center gap-3">
                  <input
                    id="timezone"
                    list="timezones"
                    className={inputClass}
                    value={draft.time_zone}
                    disabled={readOnly}
                    onChange={(e) => update((d) => ({ ...d, time_zone: e.target.value }))}
                    placeholder="UTC (server default)"
                  />
                  {!readOnly && browserTimeZone && draft.time_zone !== browserTimeZone && (
                    <button
                      type="button"
                      onClick={() => update((d) => ({ ...d, time_zone: browserTimeZone }))}
                      className="mt-1 shrink-0 text-sm text-blue-600 hover:text-blue-800"
                    >
                      Use {browserTimeZone}
                    </button>
                  )}
                </div>
                <datalist id="timezones">
                  {timeZones.map((tz) => (
                    <option key={tz} value={tz} />
                  ))}
                </datalist>
              </div>
              <label className="flex items-start gap-3">
                <input
                  type="checkbox"
                  className="mt-1 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                  checked={draft.trust_ca_root}
                  disabled={readOnly}
                  onChange={(e) => update((d) => ({ ...d, trust_ca_root: e.target.checked }))}
                />
                <span>
                  <span className="block text-sm font-medium text-gray-700">Trust this CA&apos;s root for destinations</span>
                  <span className="block text-xs text-gray-500">
                    For an ntfy server, MQTT broker or mail server whose certificate was issued by your step-ca.
                    The root is checked against the configured fingerprint.
                    {status?.ca_root_error ? (
                      <span className="block mt-1 text-amber-700">Last attempt: {status.ca_root_error}</span>
                    ) : null}
                  </span>
                </span>
              </label>
            </div>
          </div>

          {/* Where */}
          <div className="bg-white rounded-lg shadow">
            <div className="px-6 py-4 border-b border-gray-200">
              <h2 className="text-lg font-medium text-gray-900">Where to send alerts</h2>
              <p className="mt-1 text-sm text-gray-500">
                Add as many as you like. Each alert run sends one combined message to every enabled destination.
                Secrets are stored encrypted and never shown again.
              </p>
            </div>

            <div className="divide-y divide-gray-200">
              {draft.destinations.length === 0 && (
                <p className="px-6 py-6 text-sm text-gray-500">No destinations yet.</p>
              )}
              {draft.destinations.map((dest) => {
                const s = schemaFor(dest.type)
                const ds = status?.destinations.find((x) => x.id === dest.id)
                return (
                  <div key={dest.localKey} className="px-6 py-5">
                    <div className="flex items-center justify-between gap-4">
                      <div className="flex items-center gap-3 min-w-0">
                        <input
                          aria-label="Destination name"
                          className="block w-56 border-gray-300 rounded-md shadow-sm focus:ring-blue-500 focus:border-blue-500 sm:text-sm font-medium disabled:bg-gray-50"
                          value={dest.name}
                          disabled={readOnly}
                          onChange={(e) => updateDest(dest.localKey, (x) => ({ ...x, name: e.target.value }))}
                        />
                        <span className="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium bg-blue-50 text-blue-700">
                          {s?.label || dest.type}
                        </span>
                        {!dest.id && <span className="text-xs text-amber-600">new — not saved yet</span>}
                      </div>
                      <div className="flex items-center gap-4 shrink-0">
                        <label className="flex items-center gap-2 text-sm text-gray-700">
                          <input
                            type="checkbox"
                            className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                            checked={dest.enabled}
                            disabled={readOnly}
                            onChange={(e) => updateDest(dest.localKey, (x) => ({ ...x, enabled: e.target.checked }))}
                          />
                          Enabled
                        </label>
                        {isAdmin && (
                          <>
                            <button
                              onClick={() => test(dest)}
                              disabled={!dest.id || dirty || testing !== null}
                              title={!dest.id || dirty ? 'Save your changes first' : 'Send a test alert'}
                              className="inline-flex items-center text-sm text-blue-600 hover:text-blue-800 disabled:text-gray-300 disabled:cursor-not-allowed"
                            >
                              <Send className={`h-4 w-4 mr-1 ${testing === dest.localKey ? 'animate-pulse' : ''}`} />
                              Test
                            </button>
                            <button
                              onClick={() => {
                                if (confirm(`Remove "${dest.name}"? (Takes effect when you save.)`)) {
                                  update((d) => ({ ...d, destinations: d.destinations.filter((x) => x.localKey !== dest.localKey) }))
                                }
                              }}
                              className="text-red-600 hover:text-red-800"
                              title="Remove destination"
                            >
                              <Trash2 className="h-4 w-4" />
                            </button>
                          </>
                        )}
                      </div>
                    </div>

                    {ds && (ds.last_success || ds.last_error) && (
                      <p className="mt-2 text-xs flex items-center gap-1">
                        {ds.last_error ? (
                          <>
                            <XCircle className="h-3.5 w-3.5 text-red-600" />
                            <span className="text-red-700">
                              Failed {relativeTime(ds.last_error_at)}: {ds.last_error}
                            </span>
                          </>
                        ) : (
                          <>
                            <CheckCircle className="h-3.5 w-3.5 text-green-600" />
                            <span className="text-gray-500">Last delivered {relativeTime(ds.last_success)}</span>
                          </>
                        )}
                      </p>
                    )}

                    {s && <p className="mt-2 text-xs text-gray-500">{s.description}</p>}

                    <div className="mt-4 grid grid-cols-1 sm:grid-cols-2 gap-4">
                      {s?.fields.map((f) => (
                        <FieldInput
                          key={f.key}
                          field={f}
                          dest={dest}
                          readOnly={readOnly}
                          onChange={(fn) => updateDest(dest.localKey, fn)}
                        />
                      ))}
                    </div>
                  </div>
                )
              })}
            </div>

            {isAdmin && (
              <div className="px-6 py-4 border-t border-gray-200 flex items-center gap-3">
                <select
                  aria-label="Destination type"
                  value={addType}
                  onChange={(e) => setAddType(e.target.value)}
                  className="border-gray-300 rounded-md shadow-sm focus:ring-blue-500 focus:border-blue-500 sm:text-sm"
                >
                  {schema.map((s) => (
                    <option key={s.type} value={s.type}>
                      {s.label}
                    </option>
                  ))}
                </select>
                <button
                  onClick={addDestination}
                  className="inline-flex items-center px-3 py-2 border border-gray-300 shadow-sm text-sm font-medium rounded-md text-gray-700 bg-white hover:bg-gray-50"
                >
                  <Plus className="h-4 w-4 mr-1" />
                  Add destination
                </button>
              </div>
            )}
          </div>

          {/* History */}
          <div className="bg-white rounded-lg shadow">
            <div className="px-6 py-4 border-b border-gray-200">
              <h2 className="text-lg font-medium text-gray-900">Recent alerts</h2>
            </div>
            {!status || status.recent.length === 0 ? (
              <p className="px-6 py-6 text-sm text-gray-500">No alerts sent yet.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="min-w-full divide-y divide-gray-200">
                  <thead className="bg-gray-50">
                    <tr>
                      {['Sent', 'Certificate', 'Alert', isAdmin ? 'Delivery' : null].filter(Boolean).map((h) => (
                        <th key={h} className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                          {h}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody className="bg-white divide-y divide-gray-200">
                    {status.recent.map((a) => (
                      <tr key={a.id}>
                        <td className="px-6 py-3 whitespace-nowrap text-sm text-gray-500">{formatDate(a.sent_at)}</td>
                        <td className="px-6 py-3 whitespace-nowrap text-sm font-medium text-gray-900">{a.cn}</td>
                        <td className="px-6 py-3 whitespace-nowrap text-sm">
                          {a.level === 0 ? (
                            <span className="inline-flex items-center text-red-700 font-semibold">
                              <AlertTriangle className="h-4 w-4 mr-1" /> {levelLabel(a.level)}
                            </span>
                          ) : (
                            <span className="inline-flex items-center text-yellow-700">
                              <Clock className="h-4 w-4 mr-1" /> {levelLabel(a.level)}
                            </span>
                          )}
                        </td>
                        {isAdmin && <td className="px-6 py-3 text-sm text-gray-500">{a.results}</td>}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          {isAdmin && status?.key_source && (
            <p className="text-xs text-gray-400">Secrets are encrypted with the key from {status.key_source}.</p>
          )}
        </div>
      </div>

      {/* Save bar: appears only with unsaved changes */}
      {isAdmin && dirty && (
        <div className="fixed bottom-0 inset-x-0 bg-white border-t border-gray-200 shadow-lg">
          <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-3 flex items-center justify-between">
            <span className="text-sm text-gray-700">You have unsaved changes.</span>
            <div className="flex gap-3">
              <button
                onClick={() => setDraft(baseline)}
                disabled={saving}
                className="px-4 py-2 border border-gray-300 rounded-md text-sm font-medium text-gray-700 bg-white hover:bg-gray-50"
              >
                Discard
              </button>
              <button
                onClick={save}
                disabled={saving}
                className="px-4 py-2 rounded-md text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50"
              >
                {saving ? 'Saving…' : 'Save changes'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function FieldInput({
  field,
  dest,
  readOnly,
  onChange,
}: {
  field: AlertField
  dest: DraftDestination
  readOnly: boolean
  onChange: (fn: (d: DraftDestination) => DraftDestination) => void
}) {
  const id = `${dest.localKey}-${field.key}`
  const label = (
    <label htmlFor={id} className="block text-sm font-medium text-gray-700">
      {field.label}
      {field.required && <span className="text-red-600"> *</span>}
    </label>
  )
  const help = field.help ? <p className="mt-1 text-xs text-gray-500">{field.help}</p> : null

  if (field.secret) {
    const state = dest.clearSecrets.includes(field.key) ? 'unset' : dest.secretStates[field.key] || 'unset'
    const editing = state !== 'set' || dest.replacing[field.key]
    return (
      <div>
        {label}
        {!editing ? (
          <div className="mt-1 flex items-center gap-3">
            <span className="flex-1 px-3 py-2 rounded-md bg-gray-50 border border-gray-200 text-sm text-gray-500">
              •••••••• saved
            </span>
            {!readOnly && (
              <>
                <button
                  type="button"
                  className="text-sm text-blue-600 hover:text-blue-800"
                  onClick={() => onChange((x) => ({ ...x, replacing: { ...x.replacing, [field.key]: true } }))}
                >
                  Replace
                </button>
                {!field.required && (
                  <button
                    type="button"
                    className="text-sm text-red-600 hover:text-red-800"
                    onClick={() => onChange((x) => ({ ...x, clearSecrets: [...x.clearSecrets, field.key] }))}
                  >
                    Clear
                  </button>
                )}
              </>
            )}
          </div>
        ) : (
          <div className="mt-1 flex items-center gap-3">
            <input
              id={id}
              type="password"
              autoComplete="new-password"
              className="block w-full border-gray-300 rounded-md shadow-sm focus:ring-blue-500 focus:border-blue-500 sm:text-sm disabled:bg-gray-50"
              placeholder={readOnly ? 'not set' : field.placeholder || ''}
              disabled={readOnly}
              value={dest.newSecrets[field.key] || ''}
              onChange={(e) => onChange((x) => ({ ...x, newSecrets: { ...x.newSecrets, [field.key]: e.target.value } }))}
            />
            {state === 'set' && dest.replacing[field.key] && (
              <button
                type="button"
                className="text-sm text-gray-600 hover:text-gray-800"
                onClick={() =>
                  onChange((x) => {
                    const newSecrets = { ...x.newSecrets }
                    delete newSecrets[field.key]
                    return { ...x, newSecrets, replacing: { ...x.replacing, [field.key]: false } }
                  })
                }
              >
                Cancel
              </button>
            )}
          </div>
        )}
        {dest.secretStates[field.key] === 'undecryptable' && !dest.newSecrets[field.key] && (
          <p className="mt-1 text-xs text-red-700">
            Saved, but can&apos;t be decrypted with the current settings key. Re-enter it.
          </p>
        )}
        {help}
      </div>
    )
  }

  const value = dest.settings[field.key] ?? ''
  const set = (v: string) => onChange((x) => ({ ...x, settings: { ...x.settings, [field.key]: v } }))

  if (field.kind === 'checkbox') {
    const checked = (value || field.default || 'false') === 'true'
    return (
      <label className="flex items-start gap-3 sm:col-span-2">
        <input
          id={id}
          type="checkbox"
          className="mt-1 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
          checked={checked}
          disabled={readOnly}
          onChange={(e) => set(e.target.checked ? 'true' : 'false')}
        />
        <span className="text-sm text-gray-700">{field.label}</span>
      </label>
    )
  }

  if (field.kind === 'select') {
    return (
      <div>
        {label}
        <select id={id} className={inputClass} value={value || field.default || ''} disabled={readOnly} onChange={(e) => set(e.target.value)}>
          {field.options?.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        {help}
      </div>
    )
  }

  return (
    <div>
      {label}
      <input
        id={id}
        type={field.kind === 'number' ? 'number' : 'text'}
        className={inputClass}
        value={value}
        placeholder={field.placeholder || field.default || ''}
        disabled={readOnly}
        onChange={(e) => set(e.target.value)}
      />
      {help}
    </div>
  )
}

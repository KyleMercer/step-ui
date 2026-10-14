// API client for the expiry alerts feature. Kept in its own file (with its
// page under app/alerts) so the feature can be removed without touching
// the rest of the frontend beyond one navigation entry.
import api from './api'

export interface AlertField {
  key: string
  label: string
  kind: 'text' | 'number' | 'select' | 'checkbox' | 'password'
  secret: boolean
  required: boolean
  placeholder?: string
  help?: string
  options?: { value: string; label: string }[]
  default?: string
}

export interface AlertTypeSchema {
  type: string
  label: string
  description: string
  fields: AlertField[]
}

export type SecretState = 'set' | 'unset' | 'undecryptable'

export interface AlertDestination {
  id: string
  name: string
  type: string
  enabled: boolean
  settings: Record<string, string>
  secrets: Record<string, SecretState>
}

export interface AlertConfig {
  enabled: boolean
  thresholds: number[]
  step_ui_url: string
  trust_ca_root: boolean
  time_zone: string
  destinations: AlertDestination[]
}

export interface AlertDestinationUpdate {
  id: string
  name: string
  type: string
  enabled: boolean
  settings: Record<string, string>
  secrets: Record<string, string>
  clear_secrets: string[]
}

export interface AlertConfigUpdate {
  enabled: boolean
  thresholds: number[]
  step_ui_url: string
  trust_ca_root: boolean
  time_zone: string
  destinations: AlertDestinationUpdate[]
}

export interface SentAlert {
  id: number
  cert_id: string
  level: number
  cn: string
  serial: string
  not_after: string
  sent_at: string
  results: string
}

export interface AlertStatus {
  enabled: boolean
  last_check: string | null
  last_result: string
  check_interval_minutes: number
  key_source: string
  ca_root_trusted: boolean
  ca_root_error: string
  destinations: {
    id: string
    last_success: string | null
    last_error: string
    last_error_at: string | null
  }[]
  recent: SentAlert[]
  upcoming: { cert_id: string; cn: string; not_after: string; days_left: number; expired: boolean }[]
}

export interface CheckResult {
  checked: number
  new_alerts: unknown[]
  destinations_ok: number
  destinations_failed: number
  summary: string
}

export const alertsApi = {
  schema: async (): Promise<AlertTypeSchema[]> => (await api.get('/api/alerts/schema')).data.types,
  config: async (): Promise<AlertConfig> => (await api.get('/api/alerts/config')).data,
  status: async (): Promise<AlertStatus> => (await api.get('/api/alerts/status')).data,
  save: async (update: AlertConfigUpdate): Promise<AlertConfig> => (await api.put('/api/alerts/config', update)).data,
  checkNow: async (): Promise<CheckResult> => (await api.post('/api/alerts/check')).data,
  test: async (id: string): Promise<void> => {
    await api.post(`/api/alerts/destinations/${id}/test`)
  },
}

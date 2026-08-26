import axios from 'axios'

const API_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080'

// Only set when the backend's API_AUTH_TOKEN is configured (see
// docker-compose.yml / example.env). NOTE: this is a NEXT_PUBLIC_* value,
// so it ships in the client-side JS bundle -- it's a shared secret between
// this frontend and the backend, not a substitute for real user
// authentication. Anyone who can load this page can read it out of the
// bundle and call the backend directly with it. What it *does* buy you:
// the backend rejects requests that don't go through (or extract the
// secret from) this frontend, which is enough to stop casual/automated
// hits against an exposed backend port.
const API_AUTH_TOKEN = process.env.NEXT_PUBLIC_API_AUTH_TOKEN || ''

const api = axios.create({
  baseURL: API_URL,
  headers: {
    'Content-Type': 'application/json',
    ...(API_AUTH_TOKEN ? { Authorization: `Bearer ${API_AUTH_TOKEN}` } : {}),
  },
})

export interface Certificate {
  id: string
  cn: string
  sans: string[]
  not_after: string
  status: string
  key_strategy: string
  created_at: string
  updated_at: string
}

export interface IssueRequest {
  cn: string
  sans: string[]
  not_after_days: number
  format: 'pem' | 'pfx'
  pfx_password?: string
}

export interface SignCSRRequest {
  csr_pem: string
  not_after_days: number
}

export interface CertBundle {
  data: string
  filename: string
  mime_type: string
}

export interface CASettings {
  ca_url: string
  root_fingerprint: string
  acme_directories: string[]
}

export const certificateApi = {
  // Issue a new certificate
  issueCertificate: async (data: IssueRequest) => {
    const response = await api.post('/api/certs/issue', data)
    return response.data
  },

  // Sign a CSR
  signCSR: async (data: SignCSRRequest) => {
    const response = await api.post('/api/certs/sign-csr', data)
    return response.data
  },

  // List certificates
  listCertificates: async (params?: {
    limit?: number
    offset?: number
    status?: string
  }) => {
    const response = await api.get('/api/certs', { params })
    return response.data
  },

  // Get a specific certificate
  getCertificate: async (id: string) => {
    const response = await api.get(`/api/certs/${id}`)
    return response.data
  },

  // Renew a certificate
  renewCertificate: async (id: string) => {
    const response = await api.post(`/api/certs/${id}/renew`)
    return response.data
  },

  // Revoke a certificate
  revokeCertificate: async (id: string) => {
    const response = await api.post(`/api/certs/${id}/revoke`)
    return response.data
  },

  // Get CA settings
  getCASettings: async () => {
    const response = await api.get('/api/settings/ca')
    return response.data
  },

  // Health check
  health: async () => {
    const response = await api.get('/health')
    return response.data
  },
}

export default api

import axios from 'axios'

const API_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080'

const api = axios.create({
  baseURL: API_URL,
  headers: {
    'Content-Type': 'application/json',
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

export interface VersionInfo {
  version: string
  commit: string
  build_date: string
  go_version?: string
}

export interface FrontendSystemInfo {
  version: string
  commit: string
  build_date: string
  node_version: string
  next_version: string
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

  // Backend build identity (version, commit, build date, Go version)
  getVersion: async (): Promise<VersionInfo> => {
    const response = await api.get('/api/version')
    return response.data
  },
}

// Frontend's own build identity, served by this Next.js app itself (not the
// Go backend) -- a plain same-origin fetch, deliberately NOT routed through
// the `api` axios instance above, since that instance's baseURL points at
// NEXT_PUBLIC_API_URL (the backend). This hits the frontend server's own
// /api/system-info route instead.
export const getFrontendSystemInfo = async (): Promise<FrontendSystemInfo> => {
  const response = await fetch('/api/system-info')
  if (!response.ok) {
    throw new Error(`system-info request failed: ${response.status}`)
  }
  return response.json()
}

export default api

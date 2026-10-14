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
  // Required for the session cookie (see auth-context.tsx) to actually
  // flow: the browser is calling a different port than the page it's
  // running on (frontend:3000 talking to backend:8080 directly), which
  // counts as cross-origin for credentialed-request purposes even though
  // it's the same host. Without this, the cookie set by /api/auth/login
  // would never be sent back on subsequent requests. The backend's CORS
  // middleware has the matching Access-Control-Allow-Credentials header.
  withCredentials: true,
  headers: {
    'Content-Type': 'application/json',
    ...(API_AUTH_TOKEN ? { Authorization: `Bearer ${API_AUTH_TOKEN}` } : {}),
  },
})

export interface Certificate {
  id: string
  cn: string
  sans: string[]
  serial: string
  not_after: string
  status: string
  key_strategy: string
  owner: string
  created_at: string
  updated_at: string
}

export type Role = 'admin' | 'viewer'

// 'local' = username/password stored in step-ui. 'oidc' = signs in through
// the identity provider (e.g. Authentik); created automatically at first
// sign-in, no password here, role comes from identity-provider groups.
export type AuthSource = 'local' | 'oidc'

export interface AuthUser {
  username: string
  role: Role
  auth_source?: AuthSource
}

export interface UserAccount {
  id: number
  username: string
  role: Role
  auth_source: AuthSource
  created_at: string
}

export interface AuthConfig {
  oidc_enabled: boolean
  oidc_provider_name: string
}

// SSO sign-in is a full-page navigation to the backend (which then
// redirects to the identity provider), not an XHR -- so this is just a
// URL for a plain link, not an axios call.
export const oidcLoginUrl = `${API_URL}/api/auth/oidc/login`

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

// User accounts and sessions. Login/logout/me are usable by anyone (me
// reports 401 when there's no valid session); the user-management calls
// are admin-only at the backend, so a viewer calling them just gets a 403.
export const authApi = {
  login: async (username: string, password: string): Promise<AuthUser> => {
    const response = await api.post('/api/auth/login', { username, password })
    return response.data
  },

  logout: async (): Promise<void> => {
    await api.post('/api/auth/logout')
  },

  // Which sign-in options to offer (works without a session)
  config: async (): Promise<AuthConfig> => {
    const response = await api.get('/api/auth/config')
    return response.data
  },

  me: async (): Promise<AuthUser> => {
    const response = await api.get('/api/auth/me')
    return response.data
  },

  listUsers: async (): Promise<UserAccount[]> => {
    const response = await api.get('/api/users')
    return response.data.users
  },

  createUser: async (data: { username: string; password: string; role: Role }): Promise<UserAccount> => {
    const response = await api.post('/api/users', data)
    return response.data.user
  },

  updateUser: async (id: number, data: { password?: string; role?: Role }): Promise<UserAccount> => {
    const response = await api.put(`/api/users/${id}`, data)
    return response.data.user
  },

  deleteUser: async (id: number): Promise<void> => {
    await api.delete(`/api/users/${id}`)
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

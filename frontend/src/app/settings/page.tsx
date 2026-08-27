'use client'

import { useEffect, useState } from 'react'
import { certificateApi, getFrontendSystemInfo, CASettings, VersionInfo, FrontendSystemInfo } from '@/lib/api'
import { Shield, Copy, Download, AlertCircle, CheckCircle } from 'lucide-react'
import { toast } from 'react-hot-toast'
import Link from 'next/link'

export default function Settings() {
  const [settings, setSettings] = useState<CASettings | null>(null)
  const [loading, setLoading] = useState(true)
  const [backendInfo, setBackendInfo] = useState<VersionInfo | null>(null)
  const [backendInfoError, setBackendInfoError] = useState(false)
  const [frontendInfo, setFrontendInfo] = useState<FrontendSystemInfo | null>(null)
  const [frontendInfoError, setFrontendInfoError] = useState(false)

  const loadSettings = async () => {
    try {
      const response = await certificateApi.getCASettings()
      setSettings(response)
    } catch (error) {
      console.error('Failed to load CA settings:', error)
      toast.error('Failed to load CA settings')
    } finally {
      setLoading(false)
    }
  }

  const loadSystemInfo = async () => {
    try {
      setFrontendInfo(await getFrontendSystemInfo())
    } catch (error) {
      console.error('Failed to load frontend system info:', error)
      setFrontendInfoError(true)
    }

    try {
      setBackendInfo(await certificateApi.getVersion())
    } catch (error) {
      console.error('Failed to load backend version:', error)
      setBackendInfoError(true)
    }
  }

  useEffect(() => {
    loadSettings()
    loadSystemInfo()
  }, [])

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text)
    toast.success('Copied to clipboard!')
  }

  const downloadRootCA = () => {
    // This would typically fetch the root CA certificate
    toast('Root CA download would be implemented here')
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="animate-spin rounded-full h-32 w-32 border-b-2 border-blue-500"></div>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-gray-50">
      <div className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        {/* Header */}
        <div className="mb-8">
          <div className="flex items-center">
            <Link
              href="/"
              className="text-blue-600 hover:text-blue-500 text-sm font-medium"
            >
              ← Back to Dashboard
            </Link>
          </div>
          <h1 className="mt-4 text-3xl font-bold text-gray-900">CA Settings</h1>
          <p className="mt-2 text-gray-600">Certificate Authority configuration and trust instructions</p>
        </div>

        <div className="space-y-8">
          {/* CA Information */}
          <div className="bg-white rounded-lg shadow">
            <div className="px-6 py-4 border-b border-gray-200">
              <h2 className="text-lg font-medium text-gray-900">Certificate Authority Information</h2>
            </div>
            <div className="p-6 space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700">CA URL</label>
                <div className="mt-1 flex items-center">
                  <input
                    type="text"
                    value={settings?.ca_url || ''}
                    readOnly
                    className="flex-1 block w-full border-gray-300 rounded-md shadow-sm bg-gray-50 sm:text-sm"
                  />
                  <button
                    onClick={() => copyToClipboard(settings?.ca_url || '')}
                    className="ml-2 inline-flex items-center px-3 py-2 border border-gray-300 shadow-sm text-sm leading-4 font-medium rounded-md text-gray-700 bg-white hover:bg-gray-50"
                  >
                    <Copy className="h-4 w-4" />
                  </button>
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-700">Root Fingerprint</label>
                <div className="mt-1 flex items-center">
                  <input
                    type="text"
                    value={settings?.root_fingerprint || 'Not available'}
                    readOnly
                    className="flex-1 block w-full border-gray-300 rounded-md shadow-sm bg-gray-50 sm:text-sm font-mono"
                  />
                  <button
                    onClick={() => copyToClipboard(settings?.root_fingerprint || '')}
                    className="ml-2 inline-flex items-center px-3 py-2 border border-gray-300 shadow-sm text-sm leading-4 font-medium rounded-md text-gray-700 bg-white hover:bg-gray-50"
                  >
                    <Copy className="h-4 w-4" />
                  </button>
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-700">ACME Directories</label>
                <div className="mt-1">
                  {settings?.acme_directories?.map((dir, index) => (
                    <div key={index} className="flex items-center mb-2">
                      <input
                        type="text"
                        value={dir}
                        readOnly
                        className="flex-1 block w-full border-gray-300 rounded-md shadow-sm bg-gray-50 sm:text-sm"
                      />
                      <button
                        onClick={() => copyToClipboard(dir)}
                        className="ml-2 inline-flex items-center px-3 py-2 border border-gray-300 shadow-sm text-sm leading-4 font-medium rounded-md text-gray-700 bg-white hover:bg-gray-50"
                      >
                        <Copy className="h-4 w-4" />
                      </button>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          </div>

          {/* Trust Installation */}
          <div className="bg-white rounded-lg shadow">
            <div className="px-6 py-4 border-b border-gray-200">
              <h2 className="text-lg font-medium text-gray-900">Trust Installation</h2>
            </div>
            <div className="p-6 space-y-6">
              <div className="bg-blue-50 border border-blue-200 rounded-md p-4">
                <div className="flex">
                  <AlertCircle className="h-5 w-5 text-blue-400" />
                  <div className="ml-3">
                    <h3 className="text-sm font-medium text-blue-800">Important</h3>
                    <p className="mt-1 text-sm text-blue-700">
                      To trust certificates issued by this CA, you need to install the root CA certificate
                      on client systems. This allows browsers and applications to verify the certificate chain.
                    </p>
                  </div>
                </div>
              </div>

              {/* Linux/Ubuntu Instructions */}
              <div>
                <h3 className="text-lg font-medium text-gray-900 mb-3">Linux/Ubuntu</h3>
                <div className="bg-gray-50 rounded-md p-4">
                  <pre className="text-sm text-gray-800 whitespace-pre-wrap">
{`# Download the root CA certificate
curl -O ${settings?.ca_url}/roots.pem

# Install the certificate
sudo cp roots.pem /usr/local/share/ca-certificates/my-ca.crt
sudo update-ca-certificates

# Verify installation
openssl verify -CAfile /etc/ssl/certs/my-ca.pem /path/to/your/certificate.pem`}
                  </pre>
                  <button
                    onClick={() => copyToClipboard(`curl -O ${settings?.ca_url}/roots.pem\nsudo cp roots.pem /usr/local/share/ca-certificates/my-ca.crt\nsudo update-ca-certificates`)}
                    className="mt-2 inline-flex items-center px-3 py-1 border border-transparent text-sm font-medium rounded text-blue-700 bg-blue-100 hover:bg-blue-200"
                  >
                    <Copy className="h-4 w-4 mr-1" />
                    Copy Commands
                  </button>
                </div>
              </div>

              {/* Windows Instructions */}
              <div>
                <h3 className="text-lg font-medium text-gray-900 mb-3">Windows</h3>
                <div className="bg-gray-50 rounded-md p-4">
                  <pre className="text-sm text-gray-800 whitespace-pre-wrap">
{`# PowerShell (Run as Administrator)
# Download the root CA certificate
Invoke-WebRequest -Uri "${settings?.ca_url}/roots.pem" -OutFile "roots.pem"

# Import the certificate
Import-Certificate -FilePath "roots.pem" -CertStoreLocation Cert:\LocalMachine\Root

# Verify installation
Get-ChildItem -Path Cert:\LocalMachine\Root | Where-Object {$_.Subject -like "*Your CA Name*"}`}
                  </pre>
                  <button
                    onClick={() => copyToClipboard(`Invoke-WebRequest -Uri "${settings?.ca_url}/roots.pem" -OutFile "roots.pem"\nImport-Certificate -FilePath "roots.pem" -CertStoreLocation Cert:\LocalMachine\Root`)}
                    className="mt-2 inline-flex items-center px-3 py-1 border border-transparent text-sm font-medium rounded text-blue-700 bg-blue-100 hover:bg-blue-200"
                  >
                    <Copy className="h-4 w-4 mr-1" />
                    Copy Commands
                  </button>
                </div>
              </div>

              {/* macOS Instructions */}
              <div>
                <h3 className="text-lg font-medium text-gray-900 mb-3">macOS</h3>
                <div className="bg-gray-50 rounded-md p-4">
                  <pre className="text-sm text-gray-800 whitespace-pre-wrap">
{`# Download the root CA certificate
curl -O ${settings?.ca_url}/roots.pem

# Add to Keychain (double-click the .pem file)
open roots.pem

# Or via command line
sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain roots.pem

# Verify installation
security find-certificate -a -c "Your CA Name" /Library/Keychains/System.keychain`}
                  </pre>
                  <button
                    onClick={() => copyToClipboard(`curl -O ${settings?.ca_url}/roots.pem\nsudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain roots.pem`)}
                    className="mt-2 inline-flex items-center px-3 py-1 border border-transparent text-sm font-medium rounded text-blue-700 bg-blue-100 hover:bg-blue-200"
                  >
                    <Copy className="h-4 w-4 mr-1" />
                    Copy Commands
                  </button>
                </div>
              </div>

              {/* Android Instructions */}
              <div>
                <h3 className="text-lg font-medium text-gray-900 mb-3">Android</h3>
                <div className="bg-gray-50 rounded-md p-4">
                  <ol className="text-sm text-gray-800 space-y-2">
                    <li>1. Download the root CA certificate to your device</li>
                    <li>2. Go to Settings → Security → Encryption & credentials</li>
                    <li>3. Tap &quot;Install a certificate&quot; → &quot;CA certificate&quot;</li>
                    <li>4. Select the downloaded .pem file</li>
                    <li>5. Give the certificate a name and tap &quot;OK&quot;</li>
                    <li>6. The certificate is now trusted for all apps</li>
                  </ol>
                </div>
              </div>
            </div>
          </div>

          {/* Verification */}
          <div className="bg-white rounded-lg shadow">
            <div className="px-6 py-4 border-b border-gray-200">
              <h2 className="text-lg font-medium text-gray-900">Verification</h2>
            </div>
            <div className="p-6">
              <div className="bg-gray-50 rounded-md p-4">
                <h3 className="text-sm font-medium text-gray-900 mb-2">Test Certificate Trust</h3>
                <pre className="text-sm text-gray-800 whitespace-pre-wrap">
{`# Test with a certificate issued by this CA
openssl s_client -connect your-domain:443 -showcerts

# Verify certificate chain
openssl verify -CAfile chain.pem fullchain.pem

# Check certificate details
openssl x509 -in certificate.pem -text -noout`}
                </pre>
                <button
                  onClick={() => copyToClipboard(`openssl s_client -connect your-domain:443 -showcerts\nopenssl verify -CAfile chain.pem fullchain.pem\nopenssl x509 -in certificate.pem -text -noout`)}
                  className="mt-2 inline-flex items-center px-3 py-1 border border-transparent text-sm font-medium rounded text-blue-700 bg-blue-100 hover:bg-blue-200"
                >
                  <Copy className="h-4 w-4 mr-1" />
                  Copy Commands
                </button>
              </div>
            </div>
          </div>

          {/* System Information */}
          <div className="bg-white rounded-lg shadow">
            <div className="px-6 py-4 border-b border-gray-200">
              <h2 className="text-lg font-medium text-gray-900">System Information</h2>
              <p className="mt-1 text-sm text-gray-500">
                Exactly what&apos;s running right now -- useful when more than one build, stack, or
                Docker UI is in the picture. The footer on every page shows just the version number;
                this is the full detail.
              </p>
            </div>
            <div className="p-6 grid grid-cols-1 md:grid-cols-2 gap-6">
              <div>
                <h3 className="text-sm font-medium text-gray-900 mb-2">Frontend</h3>
                <div className="bg-gray-50 rounded-md p-4 text-sm text-gray-700 space-y-1 font-mono">
                  {frontendInfo ? (
                    <>
                      <div>version: {frontendInfo.version}</div>
                      <div>commit: {frontendInfo.commit}</div>
                      <div>built: {frontendInfo.build_date}</div>
                      <div>node: {frontendInfo.node_version}</div>
                      <div>next.js: {frontendInfo.next_version}</div>
                    </>
                  ) : (
                    <div className="text-gray-400">
                      {frontendInfoError ? 'Unavailable' : 'Loading…'}
                    </div>
                  )}
                </div>
              </div>

              <div>
                <h3 className="text-sm font-medium text-gray-900 mb-2">Backend</h3>
                <div className="bg-gray-50 rounded-md p-4 text-sm text-gray-700 space-y-1 font-mono">
                  {backendInfo ? (
                    <>
                      <div>version: {backendInfo.version}</div>
                      <div>commit: {backendInfo.commit}</div>
                      <div>built: {backendInfo.build_date}</div>
                      <div>go: {backendInfo.go_version}</div>
                    </>
                  ) : (
                    <div className="text-gray-400">
                      {backendInfoError ? 'Unavailable' : 'Loading…'}
                    </div>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

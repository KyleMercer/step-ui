'use client'

import { useEffect, useState } from 'react'
import { certificateApi, Certificate } from '@/lib/api'
import { formatDate, getDaysUntilExpiry, getExpiryStatus } from '@/lib/utils'
import { Shield, AlertTriangle, CheckCircle, Clock } from 'lucide-react'
import Link from 'next/link'

export default function Dashboard() {
  const [certificates, setCertificates] = useState<Certificate[]>([])
  const [loading, setLoading] = useState(true)
  const [stats, setStats] = useState({
    total: 0,
    expiring: 0,
    expired: 0,
    active: 0,
  })

  const loadCertificates = async () => {
    try {
      const response = await certificateApi.listCertificates({ limit: 100 })
      const certs = response.certificates || []
      setCertificates(certs)

      // Calculate stats
      const now = new Date()
      const expiring = certs.filter((cert: Certificate) => {
        const days = getDaysUntilExpiry(cert.not_after)
        return days > 0 && days <= 30
      }).length

      const expired = certs.filter((cert: Certificate) => {
        const days = getDaysUntilExpiry(cert.not_after)
        return days <= 0
      }).length

      const active = certs.filter((cert: Certificate) => cert.status === 'active').length

      setStats({
        total: certs.length,
        expiring,
        expired,
        active,
      })
    } catch (error) {
      console.error('Failed to load certificates:', error)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadCertificates()
  }, [])

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'active':
        return <CheckCircle className="h-4 w-4 text-green-500" />
      case 'revoked':
        return <AlertTriangle className="h-4 w-4 text-red-500" />
      case 'expired':
        return <Clock className="h-4 w-4 text-gray-500" />
      default:
        return <Clock className="h-4 w-4 text-gray-500" />
    }
  }

  const getExpiryColor = (notAfter: string) => {
    const status = getExpiryStatus(notAfter)
    switch (status) {
      case 'critical':
        return 'text-red-600'
      case 'warning':
        return 'text-yellow-600'
      case 'expired':
        return 'text-gray-500'
      default:
        return 'text-green-600'
    }
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
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        {/* Header */}
        <div className="mb-8">
          <h1 className="text-3xl font-bold text-gray-900">Certificate Dashboard</h1>
          <p className="mt-2 text-gray-600">Manage your Step-CA certificates</p>
        </div>

        {/* Stats Cards */}
        <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
          <div className="bg-white rounded-lg shadow p-6">
            <div className="flex items-center">
              <Shield className="h-8 w-8 text-blue-500" />
              <div className="ml-4">
                <p className="text-sm font-medium text-gray-500">Total Certificates</p>
                <p className="text-2xl font-bold text-gray-900">{stats.total}</p>
              </div>
            </div>
          </div>

          <div className="bg-white rounded-lg shadow p-6">
            <div className="flex items-center">
              <CheckCircle className="h-8 w-8 text-green-500" />
              <div className="ml-4">
                <p className="text-sm font-medium text-gray-500">Active</p>
                <p className="text-2xl font-bold text-gray-900">{stats.active}</p>
              </div>
            </div>
          </div>

          <div className="bg-white rounded-lg shadow p-6">
            <div className="flex items-center">
              <AlertTriangle className="h-8 w-8 text-yellow-500" />
              <div className="ml-4">
                <p className="text-sm font-medium text-gray-500">Expiring Soon</p>
                <p className="text-2xl font-bold text-gray-900">{stats.expiring}</p>
              </div>
            </div>
          </div>

          <div className="bg-white rounded-lg shadow p-6">
            <div className="flex items-center">
              <Clock className="h-8 w-8 text-red-500" />
              <div className="ml-4">
                <p className="text-sm font-medium text-gray-500">Expired</p>
                <p className="text-2xl font-bold text-gray-900">{stats.expired}</p>
              </div>
            </div>
          </div>
        </div>

        {/* Quick Actions */}
        <div className="bg-white rounded-lg shadow mb-8">
          <div className="px-6 py-4 border-b border-gray-200">
            <h2 className="text-lg font-medium text-gray-900">Quick Actions</h2>
          </div>
          <div className="p-6">
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <Link
                href="/issue"
                className="flex items-center justify-center px-4 py-3 border border-gray-300 rounded-md shadow-sm text-sm font-medium text-gray-700 bg-white hover:bg-gray-50"
              >
                <Shield className="h-5 w-5 mr-2" />
                Issue New Certificate
              </Link>
              <Link
                href="/sign-csr"
                className="flex items-center justify-center px-4 py-3 border border-gray-300 rounded-md shadow-sm text-sm font-medium text-gray-700 bg-white hover:bg-gray-50"
              >
                <AlertTriangle className="h-5 w-5 mr-2" />
                Sign CSR
              </Link>
              <Link
                href="/inventory"
                className="flex items-center justify-center px-4 py-3 border border-gray-300 rounded-md shadow-sm text-sm font-medium text-gray-700 bg-white hover:bg-gray-50"
              >
                <Clock className="h-5 w-5 mr-2" />
                View All Certificates
              </Link>
            </div>
          </div>
        </div>

        {/* Recent Certificates */}
        <div className="bg-white rounded-lg shadow">
          <div className="px-6 py-4 border-b border-gray-200">
            <h2 className="text-lg font-medium text-gray-900">Recent Certificates</h2>
          </div>
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Certificate
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Status
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Expires
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Days Left
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    Created
                  </th>
                </tr>
              </thead>
              <tbody className="bg-white divide-y divide-gray-200">
                {certificates.slice(0, 10).map((cert: Certificate) => (
                  <tr key={cert.id} className="hover:bg-gray-50">
                    <td className="px-6 py-4 whitespace-nowrap">
                      <div>
                        <div className="text-sm font-medium text-gray-900">{cert.cn}</div>
                        {cert.sans && cert.sans.length > 0 && (
                          <div className="text-sm text-gray-500">
                            SANs: {cert.sans.slice(0, 2).join(', ')}
                            {cert.sans.length > 2 && ` +${cert.sans.length - 2} more`}
                          </div>
                        )}
                      </div>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <div className="flex items-center">
                        {getStatusIcon(cert.status)}
                        <span className="ml-2 text-sm text-gray-900 capitalize">{cert.status}</span>
                      </div>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-900">
                      {formatDate(cert.not_after)}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <span className={`text-sm font-medium ${getExpiryColor(cert.not_after)}`}>
                        {getDaysUntilExpiry(cert.not_after)} days
                      </span>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                      {formatDate(cert.created_at)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {certificates.length > 10 && (
            <div className="px-6 py-3 bg-gray-50 border-t border-gray-200">
              <Link
                href="/inventory"
                className="text-sm text-blue-600 hover:text-blue-500"
              >
                View all certificates →
              </Link>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

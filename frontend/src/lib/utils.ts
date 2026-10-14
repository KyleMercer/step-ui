import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatDate(date: string | Date) {
  return new Date(date).toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function getDaysUntilExpiry(notAfter: string) {
  const expiry = new Date(notAfter)
  const now = new Date()
  const diffTime = expiry.getTime() - now.getTime()
  const diffDays = Math.ceil(diffTime / (1000 * 60 * 60 * 24))
  return diffDays
}

export function isExpired(notAfter: string) {
  return new Date(notAfter).getTime() <= Date.now()
}

// Text for the "Days Left" column. Past the expiry time it says how long
// ago instead of showing a negative day count.
export function formatDaysLeft(notAfter: string) {
  if (isExpired(notAfter)) {
    const daysAgo = Math.floor((Date.now() - new Date(notAfter).getTime()) / (1000 * 60 * 60 * 24))
    if (daysAgo === 0) return 'Expired today'
    return `Expired ${daysAgo} ${daysAgo === 1 ? 'day' : 'days'} ago`
  }
  // Under a full day left would otherwise round up to "1 day".
  if (new Date(notAfter).getTime() - Date.now() < 1000 * 60 * 60 * 24) return 'Under 1 day'
  const days = getDaysUntilExpiry(notAfter)
  return `${days} days`
}

export function getExpiryStatus(notAfter: string) {
  // Based on the actual expiry time, not the rounded day count -- a
  // certificate that expired an hour ago rounds to "0 days" and would
  // otherwise still be shown as merely critical.
  if (isExpired(notAfter)) return 'expired'
  const days = getDaysUntilExpiry(notAfter)
  if (days <= 7) return 'critical'
  if (days <= 30) return 'warning'
  return 'good'
}

export function downloadFile(data: string, filename: string, mimeType: string) {
  const blob = new Blob([data], { type: mimeType })
  const url = window.URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  window.URL.revokeObjectURL(url)
}

export function downloadBase64File(base64Data: string, filename: string, mimeType: string) {
  const byteCharacters = atob(base64Data)
  const byteNumbers = new Array(byteCharacters.length)
  for (let i = 0; i < byteCharacters.length; i++) {
    byteNumbers[i] = byteCharacters.charCodeAt(i)
  }
  const byteArray = new Uint8Array(byteNumbers)
  const blob = new Blob([byteArray], { type: mimeType })
  const url = window.URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  window.URL.revokeObjectURL(url)
}

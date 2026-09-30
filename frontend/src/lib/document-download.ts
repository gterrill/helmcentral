import { apiBaseUrl } from '@/config/api'

export const documentContentUrl = (id: string) => `${apiBaseUrl}/api/documents/${encodeURIComponent(id)}/content`

// Fetches the file into a blob and saves it via a synthetic <a download>
// click, rather than navigating the tab there directly (review finding).
// A plain `window.location.href = contentUrlFor(id)` turned a 401 or the
// endpoint's own 500 "document file missing on disk"
// (documentContentHandler, backend/documents_handlers.go) into the SPA
// itself being replaced by raw JSON, with no way back short of a reload.
// A failure here throws instead, so the caller's own error surface shows it
// and the app never leaves the page.
export async function downloadDocument(id: string, filename: string): Promise<void> {
  const response = await fetch(`${documentContentUrl(id)}?download=1`)
  if (!response.ok) {
    const body = (await response.json().catch(() => null)) as { error?: string } | null
    throw new Error(body?.error && body.error !== '' ? body.error : `Download failed (HTTP ${response.status})`)
  }
  const blob = await response.blob()
  const url = URL.createObjectURL(blob)
  try {
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    link.click()
  } finally {
    URL.revokeObjectURL(url)
  }
}

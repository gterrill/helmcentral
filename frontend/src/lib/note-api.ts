import { apiBaseUrl } from '@/config/api'
import type { NoteDetail, NotePatch } from '@/hooks/use-notes'

// Plain-fetch note reads and writes for a surface that opens ONE note and has
// no note list to keep fresh (the document viewer opened from an Equipment
// item). hooks/use-notes.ts's getNote/patchNote refresh that hook's own list
// as a side effect, which is what the Documents panel wants and nobody else
// does. Same failure rule as the hook: a non-2xx throws with the server's
// own message.
async function submitJSON<T>(url: string, method: string, body?: unknown): Promise<T> {
  const response = await fetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!response.ok) {
    const payload = (await response.json().catch(() => ({}))) as { error?: string }
    throw new Error(payload.error ?? `HTTP ${response.status}`)
  }
  return (await response.json()) as T
}

export const fetchNote = (id: string) =>
  submitJSON<NoteDetail>(`${apiBaseUrl}/api/notes/${encodeURIComponent(id)}`, 'GET')

export const saveNote = (id: string, patch: NotePatch) =>
  submitJSON<NoteDetail>(`${apiBaseUrl}/api/notes/${encodeURIComponent(id)}`, 'PATCH', patch)

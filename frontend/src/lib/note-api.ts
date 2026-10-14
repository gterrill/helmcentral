import { apiRequest } from '@/lib/api-query'
import type { NoteDetail, NotePatch } from '@/hooks/use-notes'

// Plain note reads and writes for a surface that opens ONE note and has no
// note list to keep fresh (the document viewer opened from an Equipment
// item). hooks/use-notes.ts's getNote/patchNote refresh that hook's own list
// as a side effect, which is what the Documents panel wants and nobody else
// does. Same failure rule as the hook: a non-2xx throws with the server's
// own message.
export const fetchNote = (id: string) =>
  apiRequest<NoteDetail>(`/api/notes/${encodeURIComponent(id)}`)

export const saveNote = (id: string, patch: NotePatch) =>
  apiRequest<NoteDetail>(`/api/notes/${encodeURIComponent(id)}`, { method: 'PATCH', json: patch })

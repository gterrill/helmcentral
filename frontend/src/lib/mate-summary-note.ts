import { apiBaseUrl } from '@/config/api'

// ADR 0162: summarising a Mate conversation into a note. The draft call
// spends model tokens and writes nothing; the save call is what the Save
// button makes. Both surface the server's own sentence on failure, verbatim.

export interface SummaryEquipmentSuggestion {
  id: string
  name: string
  /** Already linked to the conversation's existing note. */
  linked: boolean
}

export interface SummaryDraft {
  title: string
  body: string
  type: string
  equipment: SummaryEquipmentSuggestion[]
  /** Set when the conversation already has a live note: Save replaces it. */
  existingNoteId: string | null
}

export interface SaveSummaryNoteInput {
  title: string
  body: string
  type: string
  /** Every equipment ticked in the dialog, linked already or not. */
  equipmentIds: string[]
  removeEquipmentIds: string[]
}

async function failure(response: Response): Promise<Error> {
  const payload = (await response.json().catch(() => ({}))) as { error?: string }
  return new Error(payload.error ?? `HTTP ${response.status}`)
}

export async function fetchSummaryDraft(conversationId: string, signal?: AbortSignal): Promise<SummaryDraft> {
  const response = await fetch(
    `${apiBaseUrl}/api/assistant/conversations/${encodeURIComponent(conversationId)}/summary-draft`,
    { method: 'POST', signal },
  )
  if (!response.ok) throw await failure(response)
  const data = (await response.json()) as {
    title: string
    body: string
    type: string
    equipment?: { id: string; name: string; linked?: boolean }[]
    existing_note_id?: string
  }
  return {
    title: data.title,
    body: data.body,
    type: data.type,
    equipment: (data.equipment ?? []).map((e) => ({ id: e.id, name: e.name, linked: e.linked === true })),
    existingNoteId: data.existing_note_id ?? null,
  }
}

export async function saveSummaryNote(conversationId: string, input: SaveSummaryNoteInput): Promise<{ noteId: string; created: boolean }> {
  const response = await fetch(
    `${apiBaseUrl}/api/assistant/conversations/${encodeURIComponent(conversationId)}/summary-note`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title: input.title,
        body: input.body,
        type: input.type,
        equipment_ids: input.equipmentIds,
        remove_equipment_ids: input.removeEquipmentIds,
      }),
    },
  )
  if (!response.ok) throw await failure(response)
  const data = (await response.json()) as { note_id: string; created: boolean }
  return { noteId: data.note_id, created: data.created }
}

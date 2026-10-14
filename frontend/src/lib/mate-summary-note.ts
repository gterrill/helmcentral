import { apiRequest } from '@/lib/api-query'

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

export async function fetchSummaryDraft(conversationId: string, signal?: AbortSignal): Promise<SummaryDraft> {
  const data = await apiRequest<{
    title: string
    body: string
    type: string
    equipment?: { id: string; name: string; linked?: boolean }[]
    existing_note_id?: string
  }>(`/api/assistant/conversations/${encodeURIComponent(conversationId)}/summary-draft`, { method: 'POST', signal })
  return {
    title: data.title,
    body: data.body,
    type: data.type,
    equipment: (data.equipment ?? []).map((e) => ({ id: e.id, name: e.name, linked: e.linked === true })),
    existingNoteId: data.existing_note_id ?? null,
  }
}

export async function saveSummaryNote(conversationId: string, input: SaveSummaryNoteInput): Promise<{ noteId: string; created: boolean }> {
  const data = await apiRequest<{ note_id: string; created: boolean }>(
    `/api/assistant/conversations/${encodeURIComponent(conversationId)}/summary-note`,
    {
      method: 'POST',
      json: {
        title: input.title,
        body: input.body,
        type: input.type,
        equipment_ids: input.equipmentIds,
        remove_equipment_ids: input.removeEquipmentIds,
      },
    },
  )
  return { noteId: data.note_id, created: data.created }
}

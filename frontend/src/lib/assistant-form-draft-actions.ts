import { apiBaseUrl } from '@/config/api'
import { apiRequest } from '@/lib/api-query'
import { mapFormDraft, type AssistantFormDraft, type FormDraftApi } from '@/hooks/use-assistant-conversations'

/**
 * Save and Dismiss on a filled-in form's card (ADR 0165). Saving is the one
 * place the form becomes a document; until then it is a draft the server
 * holds.
 */

/** A refused save or dismiss: an ApiError carrying the server's own reason, verbatim. */
export { ApiError as FormDraftActionError } from '@/lib/api-query'

export const formDraftContentUrl = (id: string) =>
  `${apiBaseUrl}/api/assistant/form-drafts/${encodeURIComponent(id)}/content`

export async function saveFormDraft(id: string, title: string, folder: string): Promise<AssistantFormDraft> {
  const body = await apiRequest<{ draft: FormDraftApi; document: { id: string } }>(
    `/api/assistant/form-drafts/${encodeURIComponent(id)}/save`,
    { method: 'POST', json: { title, folder } },
  )
  return mapFormDraft({ ...body.draft, document_id: body.document.id })
}

export async function dismissFormDraft(id: string): Promise<AssistantFormDraft> {
  const body = await apiRequest<{ draft: FormDraftApi }>(
    `/api/assistant/form-drafts/${encodeURIComponent(id)}/dismiss`,
    { method: 'POST' },
  )
  return mapFormDraft(body.draft)
}

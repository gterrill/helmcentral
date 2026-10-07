import { apiBaseUrl } from '@/config/api'
import { mapFormDraft, type AssistantFormDraft, type FormDraftApi } from '@/hooks/use-assistant-conversations'

/**
 * Save and Dismiss on a filled-in form's card (ADR 0165). Saving is the one
 * place the form becomes a document; until then it is a draft the server
 * holds.
 */

/** A refused save or dismiss. `message` is the server's own reason, verbatim. */
export class FormDraftActionError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'FormDraftActionError'
    this.status = status
  }
}

async function post(path: string, body?: unknown): Promise<Response> {
  const response = await fetch(`${apiBaseUrl}${path}`, {
    method: 'POST',
    ...(body === undefined ? {} : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  })
  if (!response.ok) {
    let message = `HTTP ${response.status}`
    try {
      const parsed = (await response.json()) as { error?: string }
      if (parsed.error) message = parsed.error
    } catch {
      // Not JSON: keep the status line.
    }
    throw new FormDraftActionError(response.status, message)
  }
  return response
}

export const formDraftContentUrl = (id: string) =>
  `${apiBaseUrl}/api/assistant/form-drafts/${encodeURIComponent(id)}/content`

export async function saveFormDraft(id: string, title: string, folder: string): Promise<AssistantFormDraft> {
  const response = await post(`/api/assistant/form-drafts/${encodeURIComponent(id)}/save`, { title, folder })
  const body = (await response.json()) as { draft: FormDraftApi; document: { id: string } }
  return mapFormDraft({ ...body.draft, document_id: body.document.id })
}

export async function dismissFormDraft(id: string): Promise<AssistantFormDraft> {
  const response = await post(`/api/assistant/form-drafts/${encodeURIComponent(id)}/dismiss`)
  const body = (await response.json()) as { draft: FormDraftApi }
  return mapFormDraft(body.draft)
}

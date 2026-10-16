import type { FormDraftApi, ProposalApi } from '@/hooks/use-assistant-conversations'

/** The stored reply and its conversation, as the server sends them on a message event. */
export interface MessageAttachmentApi {
  document_id: string
  filename: string
}

export interface MessageApi {
  id: string
  conversation_id: string
  seq: number
  role: 'user' | 'assistant' | 'watch'
  content: string
  model?: string
  prompt_tokens?: number
  cached_tokens?: number
  completion_tokens?: number
  cost_usd?: number
  tool_rounds?: number
  created_at: string
  attachments?: MessageAttachmentApi[]
  proposals?: ProposalApi[]
  form_drafts?: FormDraftApi[]
}

export interface ConversationApi {
  id: string
  title: string
  created_at: string
  updated_at: string
}

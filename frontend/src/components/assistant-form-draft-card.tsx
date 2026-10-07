import { Check, FileText, Loader2 } from 'lucide-react'
import { useCallback, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { AssistantFormDraft } from '@/hooks/use-assistant-conversations'
import { dismissFormDraft, formDraftContentUrl, FormDraftActionError, saveFormDraft } from '@/lib/assistant-form-draft-actions'
import { documentViewerHref } from '@/lib/document-citation'

interface AssistantFormDraftCardProps {
  draft: AssistantFormDraft
  /** Below write tier the card still lets the operator open the form, with
   * no way to save or dismiss it. */
  canWrite: boolean
  /** Called with the server's answer to a successful Save or Dismiss, so the
   * thread holds the new status. */
  onChange: (draft: AssistantFormDraft) => void
}

/**
 * The card under a Mate reply that filled in a PDF form (ADR 0165). The form
 * is a draft: the operator opens it, checks it, and either saves it to
 * Documents under a title and folder they can change, or dismisses it. Nothing
 * reaches Documents until Save. The card renders from the stored status, so a
 * reloaded thread shows a saved or dismissed card as it was left.
 */
export function AssistantFormDraftCard({ draft, canWrite, onChange }: AssistantFormDraftCardProps) {
  const [title, setTitle] = useState(draft.title)
  const [folder, setFolder] = useState(draft.folder)
  const [busy, setBusy] = useState<'save' | 'dismiss' | null>(null)
  const [error, setError] = useState<string | null>(null)

  const run = useCallback(
    async (kind: 'save' | 'dismiss') => {
      setBusy(kind)
      setError(null)
      try {
        onChange(kind === 'save' ? await saveFormDraft(draft.id, title, folder) : await dismissFormDraft(draft.id))
      } catch (err) {
        setError(err instanceof FormDraftActionError || err instanceof Error ? err.message : String(err))
      } finally {
        setBusy(null)
      }
    },
    [draft.id, title, folder, onChange],
  )

  const pending = draft.status === 'draft'
  const saved = draft.status === 'saved'
  const dismissed = draft.status === 'dismissed'
  const pages = draft.pageCount === 1 ? '1 page' : `${draft.pageCount} pages`

  return (
    <section
      aria-label="Filled-in form"
      data-testid="assistant-form-draft-card"
      data-status={draft.status}
      className="flex min-w-0 flex-col gap-2 rounded-md border border-border bg-card p-3"
    >
      <div className="flex min-w-0 items-center gap-2">
        {saved ? (
          <Check className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        ) : (
          <FileText className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        )}
        <span className="min-w-0 truncate text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
          Filled-in form
        </span>
        <span className="ml-auto shrink-0 text-xs text-muted-foreground">
          {saved ? 'Saved to Documents' : dismissed ? 'Dismissed' : 'Not saved yet'}
        </span>
      </div>

      <p className={dismissed ? 'min-w-0 truncate text-sm text-muted-foreground line-through' : 'min-w-0 truncate text-sm text-foreground'}>
        {draft.title}
        <span className="text-muted-foreground"> · {pages}</span>
      </p>

      {!dismissed && (
        <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs">
          <a
            href={formDraftContentUrl(draft.id)}
            target="_blank"
            rel="noreferrer"
            className="text-primary underline underline-offset-2"
          >
            Open the form
          </a>
          <a href={`${formDraftContentUrl(draft.id)}?download=1`} className="text-primary underline underline-offset-2">
            Download
          </a>
          {saved && draft.documentId && (
            <a href={documentViewerHref(draft.documentId)} className="text-primary underline underline-offset-2">
              View in Documents
            </a>
          )}
        </div>
      )}

      {pending && canWrite && (
        <div className="flex min-w-0 flex-col gap-2">
          <label className="flex min-w-0 flex-col gap-1 text-xs text-muted-foreground">
            Title
            <Input value={title} onChange={(e) => setTitle(e.target.value)} className="h-8 text-sm text-foreground" />
          </label>
          <label className="flex min-w-0 flex-col gap-1 text-xs text-muted-foreground">
            Folder
            <Input
              value={folder}
              onChange={(e) => setFolder(e.target.value)}
              placeholder="Documents (no folder)"
              className="h-8 text-sm text-foreground"
            />
          </label>
        </div>
      )}

      {pending && !canWrite && (
        <p className="text-xs text-muted-foreground">Read-only session. Saving this form needs write access.</p>
      )}

      {error !== null && (
        <p role="alert" className="text-xs text-destructive">
          {error}
        </p>
      )}

      {pending && canWrite && (
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="sm" disabled={busy !== null || title.trim() === ''} onClick={() => { void run('save') }}>
            {busy === 'save' && <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />}
            {busy === 'save' ? 'Saving…' : 'Save to Documents'}
          </Button>
          <Button type="button" size="sm" variant="ghost" disabled={busy !== null} onClick={() => { void run('dismiss') }}>
            Dismiss
          </Button>
        </div>
      )}
    </section>
  )
}

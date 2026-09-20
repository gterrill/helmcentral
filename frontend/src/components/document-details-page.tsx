import { Plus, X } from 'lucide-react'
import { forwardRef, useCallback, useEffect, useImperativeHandle, useState } from 'react'

import { Badge } from '@/components/ui/badge'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { useDocument, type DocumentRecord } from '@/hooks/use-documents'
import { documentDisplayName, formatBytes, formatDocumentTime, mimeLabel } from '@/lib/document-display'

// ADR 0115: a full-panel page for one document's metadata,
// mirroring ADR 0112's `/wall-displays/<slug>` editor (display-editor-panel.tsx) -
// its own URL (app-location.ts's documentEditId) rather than a dialog, so it
// can be linked and survives Back/Forward, with App.tsx resolving the route
// the same way it resolves a wall display by slug.
//
// Review finding (ADR 0115 §2/§3): the page holds an explicit Save/Discard
// draft, and used to let any navigation throw it away unasked. App.tsx
// already has exactly this "Unsaved changes" guard for Settings
// (settingsDirty/SettingsPageHandle) - onDirtyChange and the imperative
// `save()` handle below are this page's half of that same machinery,
// generalized rather than reinvented, mirroring SettingsPageHandle's own
// shape and naming.

export interface DocumentDetailsPageProps {
  documentId: string
  onBack: () => void
  /** Reported the same way SettingsPage reports settingsDirty - App.tsx
   * tracks it as documentDetailsDirty and clears it once this page is no
   * longer what's rendered. */
  onDirtyChange?: (dirty: boolean) => void
}

export interface DocumentDetailsPageHandle {
  save: () => Promise<void>
}

interface DocumentDraft {
  title: string
  notes: string
  /** Operator-sourced tag names only - a suggested tag lives in
   * `suggestedTags` (derived from the fetched document, not the draft)
   * until "Keep" moves its name in here. Saving sends exactly this array as
   * the patch's `tags` field, which is what documents_store.go's UpdateMeta
   * treats as the WHOLE operator set - see that function's own comment on
   * why a name already present as a suggested tag is promoted rather than
   * duplicated. */
  tags: string[]
}

function operatorTagNames(doc: DocumentRecord): string[] {
  return doc.tags.filter((t) => t.source === 'operator').map((t) => t.tag)
}

function draftFrom(doc: DocumentRecord): DocumentDraft {
  return { title: doc.title, notes: doc.notes, tags: operatorTagNames(doc) }
}

// Order-insensitive: the draft only ever grows a tag at the end (Add, Keep)
// or removes one in place (Remove), so a plain array `===` would already
// agree with a set comparison for every path through this page - but
// comparing as sets is what actually matches the operator's mental model
// ("did the tag set change", not "did the array happen to reorder") and
// costs nothing extra to get right.
function sameTagSet(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false
  const sortedA = [...a].sort()
  const sortedB = [...b].sort()
  return sortedA.every((tag, index) => tag === sortedB[index])
}

// Same wording as documents-panel.tsx's own statusBadge - duplicated locally
// rather than imported/shared (that file is already committed and out of
// scope for this change) the same way stagedUploadStatusLabel there already
// duplicates a label from assistant-thread.tsx.
function statusLabel(doc: DocumentRecord): string {
  switch (doc.status) {
    case 'pending':
      return 'Reading…'
    case 'failed':
      return 'Failed'
    case 'indexed':
    default:
      return 'Indexed'
  }
}

// documents_indexer.go's finishIndexed writes indexed_with as exactly
// "local", "mate" or "" (never indexed) - operator wording for each, keyed
// the same way statusLabel above is. An unrecognised value passes through
// as-is rather than being swallowed or guessed at: if a fourth reader is
// ever added, seeing its raw name here is a better failure than seeing a
// wrong label for it.
function readByLabel(doc: DocumentRecord): string {
  switch (doc.indexed_with) {
    case 'local':
      return 'On board'
    case 'mate':
      return 'Mate'
    case '':
      return '--'
    default:
      return doc.indexed_with
  }
}

export const DocumentDetailsPage = forwardRef<DocumentDetailsPageHandle, DocumentDetailsPageProps>(function DocumentDetailsPage(
  { documentId, onBack, onDirtyChange },
  ref,
) {
  const { document, loading, error, patch } = useDocument(documentId)

  const [draft, setDraft] = useState<DocumentDraft | null>(null)
  // Re-seeds only when a *different* document has loaded (its id changes),
  // not on every incidental re-render of the same one - patch() below
  // already sets the hook's `document` to the server's echoed-back response
  // on a successful save, and keying this off `document` itself (rather than
  // its id) would re-run right after every Save too. That happens to be
  // harmless the moment it fires (the draft already matches what was just
  // submitted), but it is the wrong signal to build on: a future caller that
  // refreshes the same document in the background for some other reason
  // would silently discard an edit in progress. Keying on the id is what
  // actually tells "a new document arrived" apart from "the same one came
  // back again".
  useEffect(() => {
    if (document) setDraft(draftFrom(document))
    // eslint-disable-next-line react-hooks/exhaustive-deps -- deliberately keyed on the id only, see the comment above.
  }, [document?.id])

  const [tagInput, setTagInput] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  // Hoisted above the loading/error/not-found returns below (unlike the rest
  // of this page's derived values) because both this and the imperative
  // handle right after it are hooks - useEffect/useImperativeHandle have to
  // run on every render, not only once document/draft are ready, so `dirty`
  // is computed defensively against either still being null rather than
  // after the early returns the way suggestedTags etc. are.
  const dirty = draft !== null && document !== null
    && (draft.title !== document.title
      || draft.notes !== document.notes
      || !sameTagSet(draft.tags, operatorTagNames(document)))

  useEffect(() => {
    onDirtyChange?.(dirty)
  }, [dirty, onDirtyChange])

  // Exposed below via useImperativeHandle so App.tsx's "Save and Continue"
  // (the generalized Settings dirty-navigation guard, ADR 0115 §2) can save
  // this page's own draft and keep the operator here on a failure, without
  // this component knowing anything about navigation. Throws rather than
  // swallowing the rejection - the same contract SettingsPageHandle's own
  // performSave already gives handleSaveAndContinue - so the guard dialog
  // can show the server's message instead of navigating past a save that
  // didn't actually happen. patch() itself already throws on a rejected
  // PATCH (see that hook's own comment); a null draft here would mean this
  // was called before any document ever loaded, which the guard can't
  // trigger (nothing is dirty yet) - that's a caller bug, not a case to
  // paper over (AGENTS.md fallback policy), so it throws too rather than
  // silently doing nothing.
  const performSave = useCallback(async () => {
    if (!draft) throw new Error('DocumentDetailsPage: no draft to save')
    await patch({ title: draft.title, notes: draft.notes, tags: draft.tags })
  }, [draft, patch])

  useImperativeHandle(ref, () => ({ save: performSave }), [performSave])

  const breadcrumb = (
    <Breadcrumb>
      <BreadcrumbList>
        <BreadcrumbItem>
          <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); onBack() }}>
            Documents
          </BreadcrumbLink>
        </BreadcrumbItem>
        {document && (
          <>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage>{documentDisplayName(document)}</BreadcrumbPage>
            </BreadcrumbItem>
          </>
        )}
      </BreadcrumbList>
    </Breadcrumb>
  )

  if (loading && !document) {
    return (
      <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-4">
        {breadcrumb}
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">Loading…</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-4">
        {breadcrumb}
        <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {error}
        </p>
      </div>
    )
  }

  // Same shape as display-editor-panel.tsx's own not-found state: a fetch
  // that resolved with nothing (rather than throwing) is a distinct case
  // from `error` above - it means the id itself doesn't (or no longer) name
  // a document, not that the request failed.
  if (!document || !draft) {
    return (
      <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-4">
        {breadcrumb}
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          This document could not be found.
        </div>
      </div>
    )
  }

  // Suggested tags the draft doesn't already hold - once "Keep" moves one
  // into draft.tags, it drops out of this list on the very next render
  // (rather than needing its own "kept" flag), since membership in
  // draft.tags is the only thing that decides which list a tag shows in.
  const suggestedTags = document.tags
    .filter((t) => t.source === 'suggested' && !draft.tags.includes(t.tag))
    .map((t) => t.tag)

  const addTag = () => {
    const name = tagInput.trim()
    if (name === '' || draft.tags.includes(name)) { setTagInput(''); return }
    setDraft({ ...draft, tags: [...draft.tags, name] })
    setTagInput('')
  }
  const removeTag = (tag: string) => setDraft({ ...draft, tags: draft.tags.filter((t) => t !== tag) })
  // This is the one way an operator promotes a suggestion (ADR 0115 §4) - it
  // matches what documents_store.go's UpdateMeta already does when a
  // suggested tag's name is named in a `tags` patch: promote it to operator
  // source rather than insert a duplicate row.
  const keepTag = (tag: string) => { if (!draft.tags.includes(tag)) setDraft({ ...draft, tags: [...draft.tags, tag] }) }

  const handleSave = async () => {
    setSaving(true)
    setSaveError(null)
    try {
      await performSave()
    } catch (err) {
      // AGENTS.md fallback policy: the server's own message, and the draft
      // is left exactly as typed - a rejected save (a transient network
      // drop, a duplicate title some other rule now forbids) must not cost
      // the operator the edit they were mid-way through.
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }
  const handleDiscard = () => {
    setDraft(draftFrom(document))
    setSaveError(null)
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-4">
      {breadcrumb}

      <FieldSet className="rounded-md border border-border bg-card p-4">
        <FieldLegend variant="label">Details</FieldLegend>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="document-details-title">Title</FieldLabel>
            <Input
              id="document-details-title"
              value={draft.title}
              onChange={(e) => setDraft({ ...draft, title: e.target.value })}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="document-details-notes">Notes</FieldLabel>
            <Textarea
              id="document-details-notes"
              value={draft.notes}
              onChange={(e) => setDraft({ ...draft, notes: e.target.value })}
              rows={3}
            />
          </Field>

          <Field>
            <FieldLabel>Tags</FieldLabel>
            <div className="flex flex-wrap gap-1.5">
              {draft.tags.length === 0 && <span className="text-xs text-muted-foreground">No tags yet.</span>}
              {draft.tags.map((tag) => (
                <Badge key={tag} variant="secondary" className="gap-1">
                  {tag}
                  <button type="button" aria-label={`Remove tag ${tag}`} onClick={() => removeTag(tag)}>
                    <X className="h-3 w-3" aria-hidden="true" />
                  </button>
                </Badge>
              ))}
            </div>
            <div className="flex items-center gap-2">
              <Input
                aria-label="Add tag"
                value={tagInput}
                onChange={(e) => setTagInput(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addTag() } }}
                className="max-w-48"
              />
              <Button type="button" variant="outline" size="sm" onClick={addTag}>Add</Button>
            </div>
            {suggestedTags.length > 0 && (
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-xs text-muted-foreground">Suggested by Mate</span>
                {suggestedTags.map((tag) => (
                  <Badge key={tag} variant="outline" className="gap-1">
                    {tag}
                    <button type="button" aria-label={`Keep tag ${tag}`} onClick={() => keepTag(tag)}>
                      <Plus className="h-3 w-3" aria-hidden="true" />
                    </button>
                  </Badge>
                ))}
              </div>
            )}
          </Field>
        </FieldGroup>

        {saveError && (
          <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {saveError}
          </p>
        )}

        <div className="flex items-center gap-2">
          <Button type="button" disabled={!dirty || saving} onClick={() => { void handleSave() }}>Save</Button>
          <Button type="button" variant="outline" disabled={!dirty || saving} onClick={handleDiscard}>Discard</Button>
        </div>
      </FieldSet>

      <FieldSet className="rounded-md border border-border bg-card p-4">
        <FieldLegend variant="label">Indexing</FieldLegend>
        <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1.5 text-sm">
          <dt className="text-muted-foreground">Status</dt>
          <dd>
            {statusLabel(document)}
            {document.status === 'failed' && document.error && ` · ${document.error}`}
          </dd>

          <dt className="text-muted-foreground">File</dt>
          <dd>{document.filename}</dd>

          <dt className="text-muted-foreground">Type</dt>
          <dd>{mimeLabel(document.mime)}</dd>

          <dt className="text-muted-foreground">Size</dt>
          <dd>{formatBytes(document.size_bytes)}</dd>

          <dt className="text-muted-foreground">Pages</dt>
          <dd>{document.page_count}</dd>

          <dt className="text-muted-foreground">Read by</dt>
          <dd>{readByLabel(document)}</dd>

          <dt className="text-muted-foreground">Model</dt>
          <dd>{document.index_model || '--'}</dd>

          {/* Four places, not the listing's old three (ADR 0115 §5): the real
              figures are fractions of a cent, and three places rounded a
              $0.0012 receipt down to $0.001. */}
          <dt className="text-muted-foreground">Indexing cost</dt>
          <dd>
            ${document.index_cost_usd.toFixed(4)}
            <p className="text-xs text-muted-foreground">
              What reading this document has cost so far, added up across every reindex.
            </p>
          </dd>

          <dt className="text-muted-foreground">Uploaded</dt>
          <dd>{formatDocumentTime(document.created_at) ?? '--'}</dd>

          <dt className="text-muted-foreground">Last indexed</dt>
          <dd>{formatDocumentTime(document.indexed_at) ?? 'Not yet'}</dd>
        </dl>

        {document.summary && (
          <div className="flex flex-col gap-1 border-t border-border pt-3">
            <span className="text-xs text-muted-foreground">Summary</span>
            <p className="text-sm">{document.summary}</p>
          </div>
        )}
      </FieldSet>
    </div>
  )
})

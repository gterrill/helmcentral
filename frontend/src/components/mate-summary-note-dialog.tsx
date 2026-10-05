import { Loader2 } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { NoteEditorBody } from '@/components/note-editor'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { fetchSummaryDraft, saveSummaryNote, type SummaryDraft } from '@/lib/mate-summary-note'

// ADR 0162: the review step between Mate's summary of a conversation and a
// note. Opening it asks the server for a draft (a few seconds of model time,
// nothing written); only Save writes. A conversation keeps one note, so when
// the draft reports an existing note the button reads "Update note".

export interface MateSummaryNoteDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  conversationId: string
  /** Called after the server has saved, with the note and whether it was new. */
  onSaved: (noteId: string, created: boolean) => void
}

export function MateSummaryNoteDialog({ open, onOpenChange, conversationId, onSaved }: MateSummaryNoteDialogProps) {
  const [draft, setDraft] = useState<SummaryDraft | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [title, setTitle] = useState('')
  const bodyRef = useRef('')
  const [ticked, setTicked] = useState<Record<string, boolean>>({})

  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    setDraft(null)
    setLoadError(null)
    setSaveError(null)
    fetchSummaryDraft(conversationId, controller.signal)
      .then((d) => {
        setDraft(d)
        setTitle(d.title)
        bodyRef.current = d.body
        // Suggested links start ticked: the operator unticks what is wrong.
        setTicked(Object.fromEntries(d.equipment.map((e) => [e.id, true])))
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        setLoadError(err instanceof Error ? err.message : String(err))
      })
    return () => controller.abort()
  }, [open, conversationId])

  const updating = draft?.existingNoteId != null

  async function handleSave() {
    if (!draft) return
    setSaving(true)
    setSaveError(null)
    try {
      const result = await saveSummaryNote(conversationId, {
        title: title.trim(),
        body: bodyRef.current,
        type: draft.type,
        equipmentIds: draft.equipment.filter((e) => ticked[e.id]).map((e) => e.id),
        removeEquipmentIds: draft.equipment.filter((e) => !ticked[e.id] && e.linked).map((e) => e.id),
      })
      onSaved(result.noteId, result.created)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90dvh] max-w-2xl flex-col overflow-hidden">
        <DialogHeader>
          <DialogTitle>{updating ? 'Update note' : 'Summarise to note'}</DialogTitle>
          <DialogDescription>
            What this conversation established about the boat. Nothing is saved until you press {updating ? 'Update note' : 'Save'}.
          </DialogDescription>
        </DialogHeader>

        <div className="min-h-0 min-w-0 flex-1 overflow-y-auto">
          {!draft && !loadError && (
            <p className="flex items-center gap-2 text-sm text-muted-foreground" role="status">
              <Loader2 className="h-4 w-4 shrink-0 animate-spin" />
              Writing the summary…
            </p>
          )}
          {loadError && (
            <p className="text-sm text-destructive" role="alert">
              {loadError}
            </p>
          )}
          {draft && (
            <div className="flex min-w-0 flex-col gap-4">
              <label className="flex flex-col gap-1">
                <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Title</span>
                <Input value={title} onChange={(e) => setTitle(e.target.value)} aria-label="Title" />
              </label>
              <div className="flex min-w-0 flex-col gap-1">
                <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Note</span>
                <div className="min-w-0 rounded-md border border-border p-2">
                  <NoteEditorBody
                    value={draft.body}
                    onMarkdownChange={(markdown) => {
                      bodyRef.current = markdown
                    }}
                  />
                </div>
              </div>
              {draft.equipment.length > 0 && (
                <fieldset className="flex flex-col gap-2">
                  <legend className="mb-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                    Link to equipment
                  </legend>
                  {draft.equipment.map((e) => (
                    <div key={e.id} className="flex min-w-0 items-center gap-2 text-sm">
                      <Checkbox
                        aria-label={e.name}
                        checked={ticked[e.id] === true}
                        onCheckedChange={(checked) => setTicked((prev) => ({ ...prev, [e.id]: checked === true }))}
                      />
                      <span className="truncate">{e.name}</span>
                    </div>
                  ))}
                </fieldset>
              )}
            </div>
          )}
          {saveError && (
            <p className="mt-3 text-sm text-destructive" role="alert">
              {saveError}
            </p>
          )}
        </div>

        <DialogFooter className="gap-2">
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          {draft && (
            <Button type="button" disabled={saving || title.trim() === ''} onClick={() => void handleSave()}>
              {updating ? 'Update note' : 'Save'}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

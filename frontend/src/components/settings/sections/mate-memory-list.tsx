import { useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { toast } from 'sonner'

import { ConfirmDelete, EmptyState, FormSection, ResourceItem, ResourceList, type RowAction } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { useMateMemories, type MateMemory } from '@/hooks/use-mate-memories'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { errorMessage } from '@/lib/errors'
import { formatDayMonthIn, selectTimeZone } from '@/lib/vessel-time'

interface MemoryFormProps {
  /** The fact being edited, or null for a new one. */
  memory: MateMemory | null
  maxChars: number
  onClose: () => void
  onSave: (text: string) => Promise<void>
}

function MemoryForm({ memory, maxChars, onClose, onSave }: MemoryFormProps) {
  const [text, setText] = useState(memory?.text ?? '')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const trimmed = text.trim()
  const invalid = trimmed === '' || trimmed === (memory?.text ?? '')

  const submit = async () => {
    if (invalid) return
    setSaving(true)
    setError(null)
    try {
      await onSave(trimmed)
      onClose()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <form
      className="flex min-w-0 flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
    >
      <DialogHeader>
        <DialogTitle>{memory ? 'Edit fact' : 'Add fact'}</DialogTitle>
        <DialogDescription>
          One plain sentence that stays true about the boat. Leave out where you are, a level or a reading: those change, and the boat measures them.
        </DialogDescription>
      </DialogHeader>
      <div className="flex min-w-0 flex-col gap-1.5">
        <label htmlFor="mate-memory-text" className="text-xs font-medium text-muted-foreground">Fact</label>
        <Textarea
          id="mate-memory-text"
          value={text}
          rows={3}
          maxLength={maxChars}
          onChange={(e) => setText(e.target.value.replace(/\s*\n\s*/g, ' '))}
        />
        <p className="text-xs tabular-nums text-muted-foreground">{text.length} of {maxChars}</p>
      </div>
      {error && <p className="text-sm text-destructive" role="alert">{error}</p>}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose} disabled={saving}>Cancel</Button>
        <Button type="submit" disabled={invalid || saving}>{saving ? 'Saving…' : 'Save'}</Button>
      </DialogFooter>
    </form>
  )
}

/**
 * The facts Mate keeps about the boat, under Standing notes. Each shows the day
 * it was kept, and can be rewritten or removed; a fact can also be added by hand.
 */
export function MateMemoryList() {
  const { memories, limit, maxChars, loading, error, refresh, add, change, remove } = useMateMemories()
  const timeZone = useVesselStateSelector(selectTimeZone)
  // The fact being edited, 'new' for the add dialog, null when it is closed.
  const [editing, setEditing] = useState<MateMemory | 'new' | null>(null)
  const [deleting, setDeleting] = useState<MateMemory | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)
  const full = memories.length >= limit

  const handleDelete = async () => {
    if (!deleting) return
    setDeleteBusy(true)
    try {
      await remove(deleting.id)
      setDeleting(null)
    } catch (err) {
      toast.error('Could not remove the fact', { description: errorMessage(err) })
    } finally {
      setDeleteBusy(false)
    }
  }

  const actions: RowAction<MateMemory>[] = [
    { label: 'Edit', icon: <Pencil className="h-4 w-4" aria-hidden="true" />, onSelect: setEditing },
    { label: 'Remove', icon: <Trash2 className="h-4 w-4" aria-hidden="true" />, onSelect: setDeleting, destructive: true },
  ]

  return (
    <FormSection
      title="Facts Mate keeps"
      description={`Lasting facts about the boat that you asked Mate to keep, such as "the fuel polisher has no sampling port on its discharge manifold". Mate reads them in every chat and every watch review. Say "remember that" to Mate, or add one here. Up to ${limit}.`}
    >
      <div className="flex flex-wrap items-center gap-3">
        <Button type="button" variant="outline" size="sm" disabled={full} onClick={() => setEditing('new')}>
          <Plus className="h-4 w-4" aria-hidden="true" />
          Add fact
        </Button>
        {full && <p className="text-xs text-muted-foreground">The list is full. Remove a fact to add another.</p>}
      </div>
      <ResourceList
        label="Facts Mate keeps"
        loading={loading}
        error={error}
        onRetry={() => void refresh()}
        count={memories.length}
        header="Facts Mate keeps"
        empty={
          <EmptyState
            title="No facts kept"
            description='Tell Mate "remember that" and a fact about the boat, or choose Add fact.'
          />
        }
      >
        {memories.map((memory) => (
          <ResourceItem
            key={memory.id}
            item={memory}
            title={memory.text}
            meta={<span>Kept {formatDayMonthIn(memory.created_at, timeZone, { year: true })}</span>}
            actions={actions}
            actionsLabel={`Actions for ${memory.text}`}
          />
        ))}
      </ResourceList>

      <Dialog open={editing !== null} onOpenChange={(next) => !next && setEditing(null)}>
        <DialogContent>
          {editing !== null && (
            <MemoryForm
              key={editing === 'new' ? 'new' : editing.id}
              memory={editing === 'new' ? null : editing}
              maxChars={maxChars}
              onClose={() => setEditing(null)}
              onSave={(text) => (editing === 'new' ? add(text) : change(editing.id, text))}
            />
          )}
        </DialogContent>
      </Dialog>
      <ConfirmDelete
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title="Remove this fact?"
        description="Mate will no longer know it. You can tell Mate again."
        deleting={deleteBusy}
        onConfirm={() => void handleDelete()}
      />
    </FormSection>
  )
}

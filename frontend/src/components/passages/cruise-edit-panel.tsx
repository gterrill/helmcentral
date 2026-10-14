import { useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDelete, FormSection } from '@/components/patterns'
import { useNavigationGuard } from '@/hooks/use-navigation-guard'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { passagesApi } from '@/lib/passages-api'
import type { CruiseRecord } from '@/lib/passages'

export interface CruiseEditPanelProps {
  cruise: CruiseRecord
  canWrite: boolean
  onChanged: () => void
  onDeleted: () => void
}

export function CruiseEditPanel({ cruise, canWrite, onChanged, onDeleted }: CruiseEditPanelProps) {
  const [name, setName] = useState(cruise.name)
  const [notes, setNotes] = useState(cruise.notes)
  const [saving, setSaving] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const dirty = name !== cruise.name || notes !== cruise.notes

  const persist = async () => {
    await passagesApi.patchCruise(cruise.id, { name: name.trim(), notes })
    onChanged()
  }

  useNavigationGuard(dirty && name.trim() !== ''
    ? {
        owner: 'cruise-details',
        label: 'Cruise',
        blocks: (_from, to) => (
          to.panel === 'passages' && to.route.view === 'cruise' && to.route.id === cruise.id ? null : { kind: 'dirty' }
        ),
        save: persist,
      }
    : null)

  const fail = (action: string, err: unknown) =>
    toast.error(`Could not ${action}`, { description: err instanceof Error ? err.message : String(err) })

  return (
    <>
      <FormSection
        title="Cruise"
        action={
          <Button
            type="button"
            size="sm"
            disabled={!canWrite || !dirty || saving || !name.trim()}
            onClick={async () => {
              setSaving(true)
              try {
                await persist()
              } catch (err) {
                fail('save cruise', err)
              } finally {
                setSaving(false)
              }
            }}
          >
            {saving ? 'Saving…' : 'Save'}
          </Button>
        }
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor="cruise-name-edit">Name</Label>
          <Input id="cruise-name-edit" value={name} disabled={!canWrite} maxLength={120} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="cruise-notes">Notes</Label>
          <Textarea id="cruise-notes" rows={6} value={notes} disabled={!canWrite} onChange={(e) => setNotes(e.target.value)} />
        </div>
        <Button type="button" variant="outline" size="sm" className="self-start" disabled={!canWrite} onClick={() => setDeleteOpen(true)}>
          Delete cruise
        </Button>
      </FormSection>
      <ConfirmDelete
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={`Delete ${cruise.name}?`}
        description="The passages are kept. They become unfiled."
        confirmLabel="Delete cruise"
        onConfirm={async () => {
          try {
            await passagesApi.deleteCruise(cruise.id)
            setDeleteOpen(false)
            onDeleted()
          } catch (err) {
            fail('delete cruise', err)
          }
        }}
      />
    </>
  )
}

import { forwardRef, useCallback, useEffect, useImperativeHandle, useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ConfirmDelete, DetailsLayout, FormSection, Page, ResourceList, SaveBar } from '@/components/patterns'
import { DeckPlan } from '@/components/inventory/deck-plan'
import { BinRow } from '@/components/inventory/location-bin-row'
import { useInventoryDecks, useInventoryZones, type InventoryZone } from '@/hooks/use-inventory'

// ADR 0142: one location's own page - rename it, add and remove its bins,
// delete it. Built from the pattern library the way EquipmentEditor is.
// Zones are "locations" to the operator; the code keeps the zone name.
//
// The name is a draft saved through the SaveBar (dirty tracking, reported up
// through onDirtyChange so App.tsx's unsaved-changes guard covers it). Bins
// are different: each is its own record, added, renamed and removed on the
// spot, exactly as the old inline list did, so they never make the page dirty.
//
// Bin removal has no confirm of its own (unlike deleting the location): the
// interesting failure - a bin still holding equipment - is a 409 the server
// itself refuses and explains, surfaced verbatim in the banner rather than
// pre-empted by a generic "are you sure?". Deleting a location gets the
// standard ConfirmDelete, and the same 409 message lands inside it.

export interface LocationEditorHandle {
  save: () => Promise<void>
}

interface LocationEditorProps {
  id: string
  onBack: () => void
  /** A delete that already succeeded - the page no longer has a record. */
  onDeleted: () => void
  /** ADR 0127: each bin's code opens its bin page. */
  onOpenBin?: (code: string) => void
  /** ADR 0156: "Edit on plan" opens the deck page the location is drawn on. */
  onOpenDeck?: (deckId: string) => void
  /** ADR 0156: a location not on any plan points to the Decks list. */
  onOpenDecks?: () => void
  onDirtyChange?: (dirty: boolean) => void
  canWrite?: boolean
}

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

// ADR 0156: where this location sits on its deck plan. Shown whether or not it
// is on one, so the way to put it there is never hidden.
function OnThePlan({
  zone, zones, canWrite, onOpenDeck, onOpenDecks,
}: {
  zone: InventoryZone
  zones: InventoryZone[]
  canWrite: boolean
  onOpenDeck: (deckId: string) => void
  onOpenDecks: () => void
}) {
  const { decks, loading, error } = useInventoryDecks()
  const deck = zone.deck_id ? decks.find((d) => d.id === zone.deck_id) ?? null : null

  return (
    <FormSection title="On the plan" description="Where this location sits on a deck plan.">
      {error ? (
        <p role="alert" className="text-sm text-destructive">{error}</p>
      ) : deck && deck.plan_document_id ? (
        <div className="flex max-w-xl flex-col gap-3">
          <DeckPlan deck={deck} zones={zones} highlightZoneId={zone.id} showPinLabels={false} />
          {canWrite && (
            <Button type="button" variant="outline" size="sm" className="w-fit gap-2" onClick={() => onOpenDeck(deck.id)}>
              <Pencil className="h-4 w-4" aria-hidden="true" />
              Edit on plan
            </Button>
          )}
        </div>
      ) : loading ? (
        <p className="text-sm text-muted-foreground">Loading...</p>
      ) : (
        <div className="flex flex-wrap items-center gap-3">
          <p className="text-sm text-muted-foreground">This location is not on a deck plan.</p>
          <Button type="button" variant="outline" size="sm" onClick={onOpenDecks}>Open Decks</Button>
        </div>
      )}
    </FormSection>
  )
}

export const LocationEditor = forwardRef<LocationEditorHandle, LocationEditorProps>(function LocationEditor(
  { id, onBack, onDeleted, onOpenBin = () => {}, onOpenDeck = () => {}, onOpenDecks = () => {}, onDirtyChange, canWrite = true },
  ref,
) {
  const { zones, loading, error, refresh, renameZone, deleteZone, createBin, renameBin, deleteBin } = useInventoryZones()
  const zone = zones.find((z) => z.id === id) ?? null

  const [name, setName] = useState(zone?.name ?? '')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [newBinCode, setNewBinCode] = useState('')
  const [newBinName, setNewBinName] = useState('')
  const [pendingDelete, setPendingDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  // Re-baseline from the server's name: first load, and after a save lands.
  // Done during render rather than in an effect, so no frame ever commits
  // with an empty draft against a loaded name and reports itself dirty.
  const serverName = zone?.name
  const [baselineName, setBaselineName] = useState(serverName)
  if (serverName !== baselineName) {
    setBaselineName(serverName)
    if (serverName !== undefined) setName(serverName)
  }

  const dirty = canWrite && zone !== null && name.trim() !== zone.name
  useEffect(() => { onDirtyChange?.(dirty) }, [dirty, onDirtyChange])

  const performSave = useCallback(async () => {
    if (!zone) return
    const trimmed = name.trim()
    if (trimmed === '') {
      setSaveError('A location needs a name.')
      throw new Error('A location needs a name.')
    }
    setSaving(true)
    setSaveError(null)
    try {
      await renameZone(zone.id, trimmed)
    } catch (err) {
      // AGENTS.md fallback policy: the server's own message (a duplicate
      // name, say), never an invented one.
      setSaveError(errorMessage(err))
      throw err
    } finally {
      setSaving(false)
    }
  }, [zone, name, renameZone])

  // Same contract as EquipmentEditorHandle: App.tsx's "Save and Continue".
  useImperativeHandle(ref, () => ({ save: performSave }), [performSave])

  const handleDiscard = () => {
    setName(zone?.name ?? '')
    setSaveError(null)
  }

  const handleAddBin = async () => {
    if (!zone) return
    const code = newBinCode.trim()
    if (code === '') return
    setActionError(null)
    try {
      await createBin(zone.id, code, newBinName.trim())
      setNewBinCode('')
      setNewBinName('')
    } catch (err) {
      setActionError(errorMessage(err))
    }
  }

  // Returns whether BinRow's own tag-orphaned warning should show - true for
  // an actual, successful write (or a no-op commit that changed nothing:
  // BinRow only turns this into a warning when the code itself changed,
  // so a no-op's `true` here is never shown as one), false on a rejected
  // write. The rejection's own message still reaches the operator via
  // actionError below either way - BinRow's own warning is additional
  // information about tags, not the failure notice itself.
  const handleRenameBin = async (binId: string, code: string, binName: string, original: { code: string; name: string }): Promise<boolean> => {
    const trimmedCode = code.trim()
    const trimmedName = binName.trim()
    if (trimmedCode === '') return false
    if (trimmedCode === original.code && trimmedName === original.name) return true
    setActionError(null)
    try {
      await renameBin(binId, { code: trimmedCode, name: trimmedName })
      return true
    } catch (err) {
      setActionError(errorMessage(err))
      return false
    }
  }

  const handleDeleteBin = async (binId: string) => {
    setActionError(null)
    try {
      await deleteBin(binId)
    } catch (err) {
      setActionError(errorMessage(err))
    }
  }

  const handleConfirmDelete = async () => {
    if (!zone) return
    setDeleting(true)
    setDeleteError(null)
    try {
      await deleteZone(zone.id)
      setPendingDelete(false)
      onDeleted()
    } catch (err) {
      // The server refuses a location that still holds bins or equipment and
      // says what is in it; the dialog stays open so that message is read.
      setDeleteError(errorMessage(err))
    } finally {
      setDeleting(false)
    }
  }

  if (!zone && loading) {
    return (
      <Page title="Location" onBack={onBack}>
        <div className="flex min-h-[240px] items-center justify-center text-sm text-muted-foreground">Loading...</div>
      </Page>
    )
  }
  if (!zone && error) {
    return (
      <Page title="Location" onBack={onBack}>
        <div className="flex min-h-[240px] flex-col items-center justify-center gap-3 text-center">
          <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {error}
          </p>
          <Button type="button" variant="outline" size="sm" onClick={() => { void refresh() }}>Retry</Button>
        </div>
      </Page>
    )
  }
  if (!zone) {
    return (
      <Page title="Location" onBack={onBack}>
        <div className="flex min-h-[240px] items-center justify-center text-sm text-muted-foreground">
          This location could not be found.
        </div>
      </Page>
    )
  }

  return (
    <Page
      title={zone.name}
      onBack={onBack}
      secondaryActions={
        canWrite
          ? [
              {
                label: 'Delete location',
                icon: <Trash2 className="h-4 w-4" aria-hidden="true" />,
                destructive: true,
                onClick: () => { setDeleteError(null); setPendingDelete(true) },
              },
            ]
          : undefined
      }
    >
      {actionError && (
        <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {actionError}
        </p>
      )}
      <DetailsLayout>
        <FormSection title="Location" description="An area of the boat: salon, engine room, lazarette.">
          <Field>
            <FieldLabel htmlFor="location-name">Name</FieldLabel>
            <Input
              id="location-name"
              aria-label="Location name"
              value={name}
              disabled={!canWrite}
              className="max-w-sm"
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && dirty) { e.preventDefault(); void performSave().catch(() => {}) } }}
            />
          </Field>
        </FormSection>

        <OnThePlan zone={zone} zones={zones} canWrite={canWrite} onOpenDeck={onOpenDeck} onOpenDecks={onOpenDecks} />

        <FormSection title="Bins" description="Numbered containers in this location. Each bin's code is what goes on its label.">
          <ResourceList
            label={`Bins in ${zone.name}`}
            count={zone.bins.length}
            empty={<p className="text-sm text-muted-foreground">No bins in this location yet.</p>}
          >
            {zone.bins.map((bin) => (
              <BinRow
                key={bin.id}
                bin={bin}
                canWrite={canWrite}
                onRename={(binId, code, binName, original) => handleRenameBin(binId, code, binName, original)}
                onDelete={(binId) => { void handleDeleteBin(binId) }}
                onOpen={onOpenBin}
              />
            ))}
          </ResourceList>

          {canWrite && (
            <div className="flex flex-wrap items-center gap-2">
              <Input
                aria-label="New bin code"
                placeholder="Code"
                className="h-9 w-28 font-mono"
                value={newBinCode}
                onChange={(e) => setNewBinCode(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void handleAddBin() } }}
              />
              <Input
                aria-label="New bin name"
                placeholder="Name (optional)"
                className="h-9 min-w-0 flex-1 basis-40"
                value={newBinName}
                onChange={(e) => setNewBinName(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void handleAddBin() } }}
              />
              <Button type="button" variant="outline" size="sm" className="gap-2" onClick={() => { void handleAddBin() }}>
                <Plus className="h-4 w-4" aria-hidden="true" />
                Add bin
              </Button>
            </div>
          )}
        </FormSection>
      </DetailsLayout>

      <ConfirmDelete
        open={pendingDelete}
        onOpenChange={(open) => { if (!open) { setPendingDelete(false); setDeleteError(null) } }}
        title={`Delete "${zone.name}"?`}
        description={deleteError ?? "This removes the location. It can't be undone."}
        deleting={deleting}
        onConfirm={() => { void handleConfirmDelete() }}
      />

      {canWrite && (
        <SaveBar
          dirty={dirty}
          saving={saving}
          error={saveError}
          onSave={() => { void performSave().catch(() => {}) }}
          onDiscard={handleDiscard}
        />
      )}
    </Page>
  )
})

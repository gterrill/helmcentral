import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react'
import { ImageUp, Trash2 } from 'lucide-react'

import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from '@/components/ui/breadcrumb'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ConfirmDelete, DetailsLayout, FormSection, Page, SaveBar } from '@/components/patterns'
import { DeckPlan } from '@/components/inventory/deck-plan'
import { DeckPlanEditor } from '@/components/inventory/deck-plan-editor'
import { deckPlanUrl, useInventoryDecks, useInventoryZones } from '@/hooks/use-inventory'
import { layoutFromZones, layoutToPayload, sameLayout } from '@/lib/deck-layout'
import { downscaleImage, PLAN_MAX_DIMENSION, photoFilename } from '@/lib/image-downscale'
import { loadImageSize, useImageSize } from '@/lib/use-image-size'

// ADR 0156: one deck's page - its name, its plan image and the drawing
// surface. The name and the whole layout (outlines and pins) are one draft
// saved through the Save bar and reported up through onDirtyChange so the
// app's unsaved-changes guard covers it; a half-drawn outline never reaches
// the server. The plan image is different, like a photo: it uploads the moment
// it is chosen.

export interface DeckEditorHandle {
  save: () => Promise<void>
}

interface DeckEditorProps {
  id: string
  /** The breadcrumb's Locations: up to the Locations index. */
  onOpenLocations: () => void
  /** The breadcrumb's Decks: up to the Decks list. */
  onOpenDecks: () => void
  /** A delete that already succeeded - the page no longer has a record. */
  onDeleted: () => void
  onDirtyChange?: (dirty: boolean) => void
  canWrite?: boolean
}

/** How far the new plan's width-to-height ratio may differ from the old one's before we warn. */
const ASPECT_TOLERANCE = 0.02

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

export const DeckEditor = forwardRef<DeckEditorHandle, DeckEditorProps>(function DeckEditor(
  { id, onOpenLocations, onOpenDecks, onDeleted, onDirtyChange, canWrite = true },
  ref,
) {
  const { decks, loading, error, refresh, renameDeck, deleteDeck, uploadPlan, saveLayout } = useInventoryDecks()
  const { zones, loading: zonesLoading, error: zonesError, refresh: refreshZones } = useInventoryZones()
  const deck = decks.find((d) => d.id === id) ?? null
  const { size: planSize } = useImageSize(deck ? deckPlanUrl(deck) : null)

  const [name, setName] = useState(deck?.name ?? '')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState<string | null>(null)
  const [shapeWarning, setShapeWarning] = useState<string | null>(null)
  const [pendingDelete, setPendingDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)

  // Re-baseline from the server's name and layout: first load, and after a
  // save lands. Done during render so no frame commits with an empty draft
  // against a loaded record and reports itself dirty.
  const serverName = deck?.name
  const [baselineName, setBaselineName] = useState(serverName)
  if (serverName !== baselineName) {
    setBaselineName(serverName)
    if (serverName !== undefined) setName(serverName)
  }
  const baseline = useMemo(() => layoutFromZones(zones, id), [zones, id])
  const baselineKey = JSON.stringify(baseline)
  const [seenKey, setSeenKey] = useState(baselineKey)
  const [layout, setLayout] = useState(baseline)
  if (baselineKey !== seenKey) {
    setSeenKey(baselineKey)
    setLayout(baseline)
  }

  const nameChanged = deck !== null && name.trim() !== deck.name
  const layoutChanged = !sameLayout(layout, baseline)
  const dirty = canWrite && deck !== null && (nameChanged || layoutChanged)
  useEffect(() => { onDirtyChange?.(dirty) }, [dirty, onDirtyChange])

  const performSave = useCallback(async () => {
    if (!deck) return
    const trimmed = name.trim()
    if (trimmed === '') {
      setSaveError('A deck needs a name.')
      throw new Error('A deck needs a name.')
    }
    setSaving(true)
    setSaveError(null)
    try {
      if (trimmed !== deck.name) await renameDeck(deck.id, trimmed)
      if (layoutChanged) {
        await saveLayout(deck.id, layoutToPayload(layout, zones))
        await refreshZones()
      }
    } catch (err) {
      // AGENTS.md fallback policy: the server's own message, never an invented one.
      setSaveError(errorMessage(err))
      throw err
    } finally {
      setSaving(false)
    }
  }, [deck, name, layout, layoutChanged, zones, renameDeck, saveLayout, refreshZones])

  // Same contract as LocationEditorHandle: App.tsx's "Save and Continue".
  useImperativeHandle(ref, () => ({ save: performSave }), [performSave])

  const handleDiscard = () => {
    setName(deck?.name ?? '')
    setLayout(baseline)
    setSaveError(null)
  }

  const handlePickPlan = async (file: File) => {
    if (!deck) return
    setUploading(true)
    setUploadError(null)
    setShapeWarning(null)
    try {
      // The old plan's shape, measured now if the page has not finished
      // loading it yet: a replacement chosen quickly must still be compared.
      // An old plan that will not load has no shape to compare against, and
      // is no reason to refuse its replacement.
      const oldUrl = deckPlanUrl(deck)
      const oldSize = planSize ?? (oldUrl ? await loadImageSize(oldUrl).catch(() => null) : null)
      const blob = await downscaleImage(file, PLAN_MAX_DIMENSION)
      const bitmap = await createImageBitmap(blob)
      const newAspect = bitmap.width / bitmap.height
      bitmap.close()
      await uploadPlan(deck.id, blob, photoFilename(file.name))
      if (oldSize) {
        const oldAspect = oldSize.width / oldSize.height
        if (Math.abs(newAspect / oldAspect - 1) > ASPECT_TOLERANCE) {
          setShapeWarning(
            'The new plan has a different shape from the old one, so the outlines and pins may no longer line up. Check each location against the new image.',
          )
        }
      }
    } catch (err) {
      setUploadError(errorMessage(err))
    } finally {
      setUploading(false)
      if (fileInput.current) fileInput.current.value = ''
    }
  }

  // Until the locations are known the page cannot draw them, and Delete
  // cannot truthfully say how many outlines it would clear.
  const zonesKnown = !zonesError && !zonesLoading
  const zonesOnDeck = zones.filter((z) => z.deck_id === id).length

  const handleConfirmDelete = async () => {
    if (!deck) return
    setDeleting(true)
    setDeleteError(null)
    try {
      await deleteDeck(deck.id)
      await refreshZones()
      setPendingDelete(false)
      onDeleted()
    } catch (err) {
      setDeleteError(errorMessage(err))
    } finally {
      setDeleting(false)
    }
  }

  // Both links go through the app's unsaved-changes guard, as the old back
  // arrow did. Until the record loads the last crumb is just "Deck".
  const breadcrumb = (
    <Breadcrumb>
      <BreadcrumbList>
        <BreadcrumbItem>
          <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); onOpenLocations() }}>Locations</BreadcrumbLink>
        </BreadcrumbItem>
        <BreadcrumbSeparator />
        <BreadcrumbItem>
          <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); onOpenDecks() }}>Decks</BreadcrumbLink>
        </BreadcrumbItem>
        <BreadcrumbSeparator />
        <BreadcrumbItem className="min-w-0">
          <BreadcrumbPage className="truncate">{deck?.name ?? 'Deck'}</BreadcrumbPage>
        </BreadcrumbItem>
      </BreadcrumbList>
    </Breadcrumb>
  )

  if (!deck && loading) {
    return (
      <Page title="Deck" breadcrumb={breadcrumb}>
        <div className="flex min-h-[240px] items-center justify-center text-sm text-muted-foreground">Loading...</div>
      </Page>
    )
  }
  if (!deck && error) {
    return (
      <Page title="Deck" breadcrumb={breadcrumb}>
        <div className="flex min-h-[240px] flex-col items-center justify-center gap-3 text-center">
          <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">{error}</p>
          <Button type="button" variant="outline" size="sm" onClick={() => { void refresh() }}>Retry</Button>
        </div>
      </Page>
    )
  }
  if (!deck) {
    return (
      <Page title="Deck" breadcrumb={breadcrumb}>
        <div className="flex min-h-[240px] items-center justify-center text-sm text-muted-foreground">This deck could not be found.</div>
      </Page>
    )
  }

  const deckNames = Object.fromEntries(decks.map((d) => [d.id, d.name]))

  return (
    <Page
      title={deck.name}
      breadcrumb={breadcrumb}
      secondaryActions={
        canWrite && zonesKnown
          ? [
              {
                label: 'Delete deck',
                icon: <Trash2 className="h-4 w-4" aria-hidden="true" />,
                destructive: true,
                onClick: () => { setDeleteError(null); setPendingDelete(true) },
              },
            ]
          : undefined
      }
    >
      <DetailsLayout>
        <FormSection title="Deck" description="A plan of one deck of the boat.">
          <Field>
            <FieldLabel htmlFor="deck-name">Name</FieldLabel>
            <Input
              id="deck-name"
              aria-label="Deck name"
              value={name}
              disabled={!canWrite}
              className="max-w-sm"
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && dirty) { e.preventDefault(); void performSave().catch(() => {}) } }}
            />
          </Field>
        </FormSection>

        <FormSection
          title="Plan image"
          description="A JPEG or PNG of the deck, such as the builder's general arrangement drawing. Export a PDF drawing to an image first."
        >
          {canWrite && (
            <div className="flex flex-wrap items-center gap-3">
              <input
                ref={fileInput}
                type="file"
                accept="image/jpeg,image/png"
                className="hidden"
                tabIndex={-1}
                aria-label="Plan image file"
                onChange={(e) => {
                  const file = e.target.files?.[0]
                  if (file) void handlePickPlan(file)
                }}
              />
              <Button type="button" variant="outline" size="sm" className="gap-2" disabled={uploading} onClick={() => fileInput.current?.click()}>
                <ImageUp className="h-4 w-4" aria-hidden="true" />
                {uploading ? 'Uploading…' : deck.plan_document_id ? 'Replace plan' : 'Upload plan'}
              </Button>
            </div>
          )}
          {uploadError && <p role="alert" className="text-sm text-destructive">{uploadError}</p>}
          {shapeWarning && (
            <p role="status" className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-sm text-foreground">
              {shapeWarning}
            </p>
          )}
        </FormSection>

        <FormSection
          title="Locations on this plan"
          description="Outline each location and pin its bins. Changes are kept as a draft until you save."
        >
          {zonesError ? (
            <div className="flex flex-col items-start gap-2">
              <p role="alert" className="text-sm text-destructive">{zonesError}</p>
              <Button type="button" variant="outline" size="sm" onClick={() => { void refreshZones() }}>Retry</Button>
            </div>
          ) : zonesLoading && zones.length === 0 ? (
            <p className="text-sm text-muted-foreground">Loading...</p>
          ) : !deck.plan_document_id ? (
            <p className="text-sm text-muted-foreground">Upload a plan image to start drawing.</p>
          ) : canWrite ? (
            <DeckPlanEditor deck={deck} zones={zones} deckNames={deckNames} layout={layout} onChange={setLayout} />
          ) : (
            <DeckPlan deck={deck} zones={zones} />
          )}
        </FormSection>
      </DetailsLayout>

      <ConfirmDelete
        open={pendingDelete}
        onOpenChange={(open) => { if (!open) { setPendingDelete(false); setDeleteError(null) } }}
        title={`Delete "${deck.name}"?`}
        description={
          deleteError
          ?? (zonesOnDeck > 0
            ? `${zonesOnDeck} ${zonesOnDeck === 1 ? 'location loses its outline' : 'locations lose their outline'} and their bins lose their pins. The locations and bins stay, and the plan image stays in Documents.`
            : 'This removes the deck. The plan image stays in Documents.')
        }
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

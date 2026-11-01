import { errorMessage } from '@/lib/errors'
import { useMemo, useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { toast } from 'sonner'

import { ConfirmDelete, EmptyState, FormSection, ResourceItem, ResourceList, SettingsLayout, type RowAction } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { useAppConfig } from '@/hooks/use-app-config'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { useSavedPlaces, type NewSavedPlace, type SavedPlace, type SavedPlaceChange } from '@/hooks/use-saved-places'
import { formatCoordinateDM, formatPositionDM, parseCoordinate } from '@/lib/coordinates'
import { depthSummary, depthToText, depthUnitLabel, parseDepthText, todayDate } from '@/lib/saved-place-depth'
import { selectTimeZone } from '@/lib/vessel-time'

interface PlaceDialogProps {
  /** The place being edited, or null for a new one. */
  place: SavedPlace | null
  open: boolean
  onClose: () => void
  /** All the fields as typed, and just those the operator changed from what was saved. */
  onSave: (fields: NewSavedPlace, changed: SavedPlaceChange) => Promise<void>
}

function PlaceDialog({ place, open, onClose, onSave }: PlaceDialogProps) {
  // Mounted only while open, so each opening starts from what is saved.
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent>{open && <PlaceForm key={place?.id ?? 'new'} place={place} onClose={onClose} onSave={onSave} />}</DialogContent>
    </Dialog>
  )
}

function PlaceForm({ place, onClose, onSave }: Omit<PlaceDialogProps, 'open'>) {
  const [name, setName] = useState(place?.name ?? '')
  const savedLat = place ? formatCoordinateDM(place.lat, 'lat') : ''
  const savedLon = place ? formatCoordinateDM(place.lon, 'lon') : ''
  const [lat, setLat] = useState(savedLat)
  const [lon, setLon] = useState(savedLon)
  const [note, setNote] = useState(place?.note ?? '')
  const { ui } = useAppConfig()
  const units = ui.distanceUnits
  const savedDepth = depthToText(place?.depth_m ?? null, units)
  const [depth, setDepth] = useState(savedDepth)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const latValue = parseCoordinate(lat, 'lat')
  const lonValue = parseCoordinate(lon, 'lon')
  const depthValue = parseDepthText(depth, units)
  const depthBad = !depthValue.ok
  // A typed depth is dated today at the boat, which needs the boat's zone.
  const timeZone = useVesselStateSelector(selectTimeZone)
  const today = useMemo(() => todayDate(timeZone), [timeZone])
  const depthChanged = depth.trim() !== savedDepth
  // What the operator typed into the depth box, in metres: undefined while it
  // is untouched or not a depth, null once emptied, a number for a new figure.
  const typedDepth = depthChanged && depthValue.ok ? depthValue.depthM : undefined
  const needsDate = typeof typedDepth === 'number'
  const dateMissing = needsDate && today === null
  const invalid = name.trim() === '' || latValue === null || lonValue === null || depthBad || dateMissing
  // A figure typed over a saved one replaces it, so the old one is shown first.
  const hadDepth = place !== null && place.depth_m !== null
  const replacing = hadDepth && typeof typedDepth === 'number'
  const clearing = hadDepth && typedDepth === null
  // A new place starts with empty boxes; do not shout about them before the operator types.
  const latBad = latValue === null && lat.trim() !== ''
  const lonBad = lonValue === null && lon.trim() !== ''

  const submit = async () => {
    if (invalid || latValue === null || lonValue === null || !depthValue.ok) return
    setSaving(true)
    setError(null)
    try {
      const stamp = depthValue.depthM === null || today === null ? {} : { depth_m: depthValue.depthM, depth_source: 'charted' as const, depth_date: today }
      const fields: NewSavedPlace = { name: name.trim(), lat: latValue, lon: lonValue, note: note.trim(), ...stamp }
      // The boxes show the position rounded to a thousandth of a minute, so a
      // position the operator did not touch is left out rather than re-sent.
      const changed: SavedPlaceChange = {}
      if (fields.name !== place?.name) changed.name = fields.name
      if (lat !== savedLat) changed.lat = fields.lat
      if (lon !== savedLon) changed.lon = fields.lon
      if (fields.note !== place?.note) changed.note = fields.note
      // A typed depth is charted and dated today; a box emptied clears it.
      if (depthChanged) Object.assign(changed, depthValue.depthM === null ? { depth_m: null } : stamp)
      await onSave(fields, changed)
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
        <DialogTitle>{place ? `Edit ${place.name}` : 'Add place'}</DialogTitle>
        <DialogDescription>Type a position as degrees and decimal minutes, such as 020°32.676&apos; S.</DialogDescription>
      </DialogHeader>
      <div className="flex min-w-0 flex-col gap-1.5">
        <label htmlFor="saved-place-name" className="text-xs font-medium text-muted-foreground">Name</label>
        <Input id="saved-place-name" value={name} maxLength={80} onChange={(e) => setName(e.target.value)} />
      </div>
      <div className="grid min-w-0 grid-cols-[repeat(auto-fit,minmax(0,1fr))] gap-3">
        <div className="flex min-w-0 flex-col gap-1.5">
          <label htmlFor="saved-place-lat" className="text-xs font-medium text-muted-foreground">Latitude</label>
          <Input id="saved-place-lat" value={lat} placeholder="020°32.676' S" aria-invalid={latBad} onChange={(e) => setLat(e.target.value)} className="tabular-nums" />
          {latBad && <p className="text-xs text-destructive">Not a latitude. Try 20°32.676&apos; S.</p>}
        </div>
        <div className="flex min-w-0 flex-col gap-1.5">
          <label htmlFor="saved-place-lon" className="text-xs font-medium text-muted-foreground">Longitude</label>
          <Input id="saved-place-lon" value={lon} placeholder="149°07.136' E" aria-invalid={lonBad} onChange={(e) => setLon(e.target.value)} className="tabular-nums" />
          {lonBad && <p className="text-xs text-destructive">Not a longitude. Try 149°07.136&apos; E.</p>}
        </div>
      </div>
      <div className="flex min-w-0 flex-col gap-1.5">
        <label htmlFor="saved-place-note" className="text-xs font-medium text-muted-foreground">Note</label>
        <Input id="saved-place-note" value={note} maxLength={300} onChange={(e) => setNote(e.target.value)} />
      </div>
      <div className="flex min-w-0 flex-col gap-1.5">
        <label htmlFor="saved-place-depth" className="text-xs font-medium text-muted-foreground">Depth at chart datum ({depthUnitLabel(units)}), optional</label>
        <Input id="saved-place-depth" inputMode="decimal" value={depth} aria-invalid={depthBad} aria-describedby="saved-place-depth-help" onChange={(e) => setDepth(e.target.value)} className="tabular-nums" />
        {depthBad && <p className="text-xs text-destructive">Not a depth. Type a number in {units === 'imperial' ? 'feet' : 'metres'}; a drying height is negative.</p>}
        <p id="saved-place-depth-help" className="text-xs text-muted-foreground">
          What a chart or someone local gives for the water here. Mate uses it for the swing and clearance checks. Saved as charted, dated today.
        </p>
        {place !== null && place.depth_m !== null && (
          <p className="text-xs text-muted-foreground" data-testid="saved-place-depth-current">Now {depthSummary(place, units)}.</p>
        )}
        {replacing && (
          <p role="status" className="text-xs text-foreground" data-testid="saved-place-depth-replaces">
            Saving replaces {depthSummary(place, units)} with {depthToText(typedDepth ?? null, units)} {depthUnitLabel(units)} (charted {today}).
          </p>
        )}
        {dateMissing && (
          <p className="text-xs text-destructive" role="alert">The boat has not reported its time zone yet, so today&apos;s date is not known. Try again once it has.</p>
        )}
        {clearing && (
          <p role="status" className="text-xs text-foreground" data-testid="saved-place-depth-clears">
            Saving clears {depthSummary(place, units)}.
          </p>
        )}
      </div>
      {error && <p className="text-sm text-destructive" role="alert">{error}</p>}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose} disabled={saving}>Cancel</Button>
        <Button type="submit" disabled={invalid || saving}>{saving ? 'Saving…' : 'Save'}</Button>
      </DialogFooter>
    </form>
  )
}

export function SavedPlacesSection() {
  const { ui } = useAppConfig()
  const { places, loading, error, refresh, add, change, remove } = useSavedPlaces()
  // The place being edited, 'new' for the add dialog, null when it is closed.
  const [editing, setEditing] = useState<SavedPlace | 'new' | null>(null)
  const [deleting, setDeleting] = useState<SavedPlace | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)

  const handleDelete = async () => {
    if (!deleting) return
    setDeleteBusy(true)
    try {
      await remove(deleting.id)
      setDeleting(null)
    } catch (err) {
      toast.error('Could not delete the place', { description: errorMessage(err) })
    } finally {
      setDeleteBusy(false)
    }
  }

  const actions: RowAction<SavedPlace>[] = [
    { label: 'Edit', icon: <Pencil className="h-4 w-4" aria-hidden="true" />, onSelect: setEditing },
    { label: 'Delete', icon: <Trash2 className="h-4 w-4" aria-hidden="true" />, onSelect: setDeleting, destructive: true },
  ]

  return (
    <SettingsLayout title="Saved places">
      <FormSection
        title="Places Mate knows"
        description="Positions you gave Mate for spots the map doesn't name. Mate looks here before it looks anything up, and in every conversation. Mate offers to add one when you give it a position, and you can add one yourself."
      >
        <div>
          <Button type="button" variant="outline" size="sm" onClick={() => setEditing('new')}>
            <Plus className="h-4 w-4" aria-hidden="true" />
            Add place
          </Button>
        </div>
        <ResourceList
          label="Saved places"
          loading={loading}
          error={error}
          onRetry={() => void refresh()}
          count={places.length}
          header="Saved places"
          empty={
            <EmptyState
              title="No saved places"
              description="Tell Mate the position of a spot, such as an anchorage with no name on the map, and it will offer to save it. Or choose Add place."
            />
          }
        >
          {places.map((place) => {
            const depth = depthSummary(place, ui.distanceUnits)
            return (
              <ResourceItem
                key={place.id}
                item={place}
                title={place.name}
                meta={<span className="tabular-nums">{formatPositionDM(place.lat, place.lon)}{depth ? ` · ${depth}` : ''}{place.note ? ` · ${place.note}` : ''}</span>}
                actions={actions}
                actionsLabel={`Actions for ${place.name}`}
              />
            )
          })}
        </ResourceList>
      </FormSection>

      <PlaceDialog
        place={editing === 'new' ? null : editing}
        open={editing !== null}
        onClose={() => setEditing(null)}
        onSave={(fields, changed) => (editing === 'new' || editing === null ? add(fields) : change(editing.id, changed))}
      />
      <ConfirmDelete
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={`Delete ${deleting?.name ?? 'this place'}?`}
        description="Mate will no longer know this position. You can give it to Mate again."
        deleting={deleteBusy}
        onConfirm={() => void handleDelete()}
      />
    </SettingsLayout>
  )
}

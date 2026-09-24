import { useMemo, useRef, useState } from 'react'
import { ArrowLeft } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { BinQuickAdd } from '@/components/inventory/bin-quick-add'
import { TagRow } from '@/components/inventory/tag-row'
import { apiBaseUrl } from '@/config/api'
import { findBinByCode, useEquipment, useInventoryZones, type EquipmentItem, type InventoryBin, type InventoryZone } from '@/hooks/use-inventory'
import { formatAppLocation } from '@/lib/app-location'

// ADR 0127 (the plan's A4): the screen a scan lands on. Resolves `code`
// case-insensitively against the zone/bin tree (useInventoryZones) - there
// is no server-side "get bin by code" route, because the whole tree is
// already small enough aboard one boat that the frontend already fetches
// it whole for the Locations section and the equipment editor's own
// zone/bin selects.

interface BinPageProps {
  code: string
  onClose: () => void
  onOpenEquipment: (id: string) => void
  /** Full item (below) - pre-sets a brand new draft's location. */
  onNewEquipment: (preset?: { zoneId?: string; binId?: string }) => void
  canWrite?: boolean
  /** Release-fixes code-review finding: forwarded to the quick-add form's
   * own onHasWorkChange (its doc comment, bin-quick-add.tsx) - App.tsx
   * routes Open/Full item through the same unsaved-work guard when this is
   * true. */
  onHasWorkChange?: (hasWork: boolean, detail?: string) => void
}

export function BinPage({ code, onClose, onOpenEquipment, onNewEquipment, canWrite = true, onHasWorkChange }: BinPageProps) {
  // ONE useInventoryZones() instance for the whole page (there is no shared
  // store between separate calls - the hook's own header comment), so that
  // when BinNotFound's Create bin below calls createBin/createZone, THIS
  // component's own `zones` state is what updates. That is what lets a
  // freshly created bin fall straight through `match` into the ordinary
  // BinContents render below, with its real onOpenEquipment/onNewEquipment
  // callbacks, rather than a second, dummy-callback render path.
  const { zones, loading: zonesLoading, error: zonesError, createZone, createBin } = useInventoryZones()

  const match = useMemo(() => findBinByCode(zones, code), [zones, code])

  if (match) {
    return (
      <BinContents
        zone={match.zone}
        bin={match.bin}
        onClose={onClose}
        onOpenEquipment={onOpenEquipment}
        onNewEquipment={onNewEquipment}
        canWrite={canWrite}
        onHasWorkChange={onHasWorkChange}
      />
    )
  }

  // Loading and "genuinely not found" both render nothing-matched-yet -
  // ADR 0127 §2: "An unknown code renders 'No bin LAZ-02' and never
  // redirects", so a still-loading zone list must not flash that state
  // before the fetch has even landed.
  if (zonesLoading) {
    return <div className="p-4 text-sm text-muted-foreground">Loading...</div>
  }

  // Review finding: a failed zones fetch used to be indistinguishable from
  // a genuinely unknown code - both fell through to BinNotFound's "No bin
  // <code>" + Create, offering to create a duplicate of a bin the zone list
  // simply failed to load. AGENTS.md fallback policy: the failure is
  // surfaced explicitly, and Create - a write that would land wrong - is
  // not offered in its place.
  if (zonesError) {
    return (
      <div className="mx-auto flex max-w-2xl flex-col gap-4">
        <Button type="button" variant="ghost" size="sm" className="w-fit gap-1.5" onClick={onClose}>
          <ArrowLeft className="h-4 w-4" aria-hidden="true" />
          Locations
        </Button>
        <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {zonesError}
        </p>
      </div>
    )
  }

  return (
    <BinNotFound
      code={code}
      zones={zones}
      createZone={createZone}
      createBin={createBin}
      onClose={onClose}
      canWrite={canWrite}
    />
  )
}

// ── not found / create ───────────────────────────────────────────────────

function BinNotFound({
  code, zones, createZone, createBin, onClose, canWrite,
}: {
  code: string
  zones: InventoryZone[]
  createZone: (name: string) => Promise<InventoryZone>
  createBin: (zoneId: string, code: string, name: string) => Promise<InventoryBin>
  onClose: () => void
  canWrite: boolean
}) {
  const [creating, setCreating] = useState(false)
  const [selectedZoneId, setSelectedZoneId] = useState<string>(zones[0]?.id ?? '')
  const [newZoneName, setNewZoneName] = useState('')
  const [binName, setBinName] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleCreate = async () => {
    setSaving(true)
    setError(null)
    try {
      let zoneId = selectedZoneId
      if (zones.length === 0) {
        const trimmedZoneName = newZoneName.trim()
        if (trimmedZoneName === '') {
          setError('Zone name is required')
          setSaving(false)
          return
        }
        const created = await createZone(trimmedZoneName)
        zoneId = created.id
      }
      if (!zoneId) {
        setError('Pick a zone')
        setSaving(false)
        return
      }
      // The code is kept exactly as typed in the URL, apart from case
      // (ADR 0127 §2) - never re-derived from what the operator types into
      // this form, which is only the zone/name. createBin's own refresh()
      // (use-inventory.ts) updates BinPage's `zones` state above, so its
      // `match` picks the new bin up and swaps this view for BinContents -
      // no local "just created" state needed here.
      await createBin(zoneId, code, binName.trim())
    } catch (err) {
      // AGENTS.md fallback policy: the server's own conflict message
      // (e.g. a code already in use case-insensitively), never an
      // invented one.
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-4">
      <Button type="button" variant="ghost" size="sm" className="w-fit gap-1.5" onClick={onClose}>
        <ArrowLeft className="h-4 w-4" aria-hidden="true" />
        Locations
      </Button>

      <div className="rounded-md border border-border bg-card p-4">
        <p className="text-sm">
          No bin <span className="font-mono">{code}</span>
        </p>

        {canWrite && !creating && (
          <Button type="button" variant="outline" size="sm" className="mt-3" onClick={() => setCreating(true)}>
            Create bin {code}
          </Button>
        )}

        {canWrite && creating && (
          <div className="mt-3 flex flex-col gap-3">
            {zones.length === 0 ? (
              <Field>
                <FieldLabel htmlFor="bin-new-zone-name">New zone</FieldLabel>
                <Input id="bin-new-zone-name" value={newZoneName} onChange={(e) => setNewZoneName(e.target.value)} placeholder="Lazarette" />
              </Field>
            ) : (
              <Field>
                <FieldLabel htmlFor="bin-create-zone">Zone</FieldLabel>
                <Select value={selectedZoneId} onValueChange={(v) => { if (v) setSelectedZoneId(v) }}>
                  <SelectTrigger id="bin-create-zone" aria-label="Zone">
                    <SelectValue>{(v: string) => zones.find((z) => z.id === v)?.name ?? v}</SelectValue>
                  </SelectTrigger>
                  <SelectPopup>
                    {zones.map((z) => <SelectItem key={z.id} value={z.id}>{z.name}</SelectItem>)}
                  </SelectPopup>
                </Select>
              </Field>
            )}
            <Field>
              <FieldLabel htmlFor="bin-create-name">Bin name (optional)</FieldLabel>
              <Input id="bin-create-name" value={binName} onChange={(e) => setBinName(e.target.value)} placeholder="Adhesives" />
            </Field>
            {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
            <div className="flex items-center gap-2">
              <Button type="button" onClick={() => { void handleCreate() }} disabled={saving}>
                {saving ? 'Creating...' : 'Create'}
              </Button>
              <Button type="button" variant="outline" onClick={() => setCreating(false)}>Cancel</Button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

// ── contents ─────────────────────────────────────────────────────────────

function photoBlockId(itemId: string): string {
  return `bin-photo-${itemId}`
}

function PhotoBlock({ item, onOpenEquipment }: { item: EquipmentItem; onOpenEquipment: (id: string) => void }) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const [activeIndex, setActiveIndex] = useState(0)
  const photoCount = item.photo_ids.length

  const handleScroll = () => {
    const el = scrollRef.current
    if (!el || el.clientWidth === 0) return
    const idx = Math.round(el.scrollLeft / el.clientWidth)
    setActiveIndex(Math.min(Math.max(idx, 0), Math.max(photoCount - 1, 0)))
  }

  return (
    <div id={photoBlockId(item.id)} className="flex min-w-0 flex-col gap-1">
      {photoCount === 0 ? (
        <div className="flex h-40 items-center justify-center rounded-md border border-border bg-muted">
          <p className="text-sm text-muted-foreground">No photo</p>
        </div>
      ) : (
        <div className="relative">
          {/* CSS scroll-snap, no carousel library (ADR 0127) - swiping
              sideways moves through THIS item's own photos; the bin's
              contents list above still scrolls independently. */}
          <div
            ref={scrollRef}
            onScroll={handleScroll}
            className="flex h-56 snap-x snap-mandatory overflow-x-auto rounded-md border border-border bg-muted"
          >
            {item.photo_ids.map((photoId) => (
              <img
                key={photoId}
                src={`${apiBaseUrl}/api/documents/${encodeURIComponent(photoId)}/content`}
                alt=""
                loading="lazy"
                className="h-full w-full shrink-0 snap-center object-contain"
              />
            ))}
          </div>
          {photoCount > 1 && (
            <>
              <span className="absolute bottom-1.5 right-1.5 rounded-sm bg-background/90 px-1.5 py-0.5 text-[10px] tabular-nums text-muted-foreground">
                {activeIndex + 1} / {photoCount}
              </span>
              <div className="absolute bottom-1.5 left-1.5 flex gap-1">
                {item.photo_ids.map((photoId, i) => (
                  <span
                    key={photoId}
                    className={i === activeIndex ? 'h-1.5 w-1.5 rounded-full bg-primary' : 'h-1.5 w-1.5 rounded-full bg-muted-foreground/40'}
                  />
                ))}
              </div>
            </>
          )}
        </div>
      )}
      <div className="flex min-w-0 items-center justify-between gap-2 px-0.5">
        <button
          type="button"
          className="min-w-0 flex-1 truncate text-left text-sm font-medium hover:text-primary"
          onClick={() => onOpenEquipment(item.id)}
        >
          {item.name}
        </button>
        <Button type="button" variant="ghost" size="sm" onClick={() => onOpenEquipment(item.id)}>Open</Button>
      </div>
    </div>
  )
}

/** The photo stack grid - exported so Stocktake (stocktake-section.tsx)
 * can show the same photo grid under the current bin it's confirming
 * against ("the bin's photo grid, reused from A4", the plan's own words)
 * rather than a second implementation of it. */
export function BinPhotoGrid({ items, onOpenEquipment }: { items: EquipmentItem[]; onOpenEquipment: (id: string) => void }) {
  if (items.length === 0) return null
  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {items.map((item) => (
        <PhotoBlock key={item.id} item={item} onOpenEquipment={onOpenEquipment} />
      ))}
    </div>
  )
}

function BinContents({
  zone, bin, onClose, onOpenEquipment, onNewEquipment, canWrite, onHasWorkChange,
}: {
  zone: InventoryZone
  bin: InventoryBin
  onClose: () => void
  onOpenEquipment: (id: string) => void
  onNewEquipment: (preset?: { zoneId?: string; binId?: string }) => void
  canWrite: boolean
  onHasWorkChange?: (hasWork: boolean, detail?: string) => void
}) {
  const { items, loading, error, refresh } = useEquipment({ bin: bin.id })

  const scrollToPhoto = (itemId: string) => {
    document.getElementById(photoBlockId(itemId))?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-4">
      <Button type="button" variant="ghost" size="sm" className="w-fit gap-1.5" onClick={onClose}>
        <ArrowLeft className="h-4 w-4" aria-hidden="true" />
        Locations
      </Button>

      <div className="flex flex-col gap-1 rounded-md border border-border bg-card p-4">
        <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{zone.name}</p>
        <div className="flex items-baseline gap-2">
          <h2 className="font-mono text-lg font-semibold">{bin.code}</h2>
          {bin.name && <span className="text-sm text-muted-foreground">{bin.name}</span>}
        </div>
        <p className="text-[11px] text-muted-foreground">{items.length} item{items.length === 1 ? '' : 's'}</p>
      </div>

      {error && (
        <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {error}
        </p>
      )}

      {canWrite && (
        <BinQuickAdd
          zoneId={zone.id}
          binId={bin.id}
          onCreated={() => { void refresh() }}
          canWrite={canWrite}
          onHasWorkChange={onHasWorkChange}
        />
      )}

      {canWrite && (
        <Button type="button" variant="outline" size="sm" className="w-fit" onClick={() => onNewEquipment({ zoneId: zone.id, binId: bin.id })}>
          Full item
        </Button>
      )}

      {!loading && items.length > 0 && (
        <div className="flex flex-col gap-1 rounded-md border border-border bg-card p-2">
          {items.map((item) => (
            <button
              key={item.id}
              type="button"
              className="flex min-w-0 items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm hover:bg-muted"
              onClick={() => scrollToPhoto(item.id)}
            >
              <span className="min-w-0 flex-1 truncate">{item.name}</span>
              {item.quantity > 1 && <span className="shrink-0 text-[11px] text-muted-foreground">×{item.quantity}</span>}
            </button>
          ))}
        </div>
      )}

      {!loading && <BinPhotoGrid items={items} onOpenEquipment={onOpenEquipment} />}

      {/* formatAppLocation (not raw interpolation) so the code is
          percent-encoded the same way the app's own /inventory/bins/<code>
          URLs are - a bin code with a space or another reserved character
          would otherwise produce a path the app itself wouldn't parse back
          the same way. */}
      <TagRow path={formatAppLocation({ panel: 'inventory', inventorySection: 'locations', binCode: bin.code }, { firstPageId: null })} />
    </div>
  )
}

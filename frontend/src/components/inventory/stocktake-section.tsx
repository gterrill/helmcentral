import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { BinPhotoGrid } from '@/components/inventory/bin-page'
import { parseAppLocation } from '@/lib/app-location'
import { nfcSupported, scanTags } from '@/lib/nfc'
import {
  EquipmentNotFoundError,
  fetchEquipment,
  findBinByCode,
  toEquipmentInput,
  updateEquipment,
  useEquipment,
  useInventoryZones,
  type EquipmentItem,
  type InventoryBin,
  type InventoryZone,
} from '@/hooks/use-inventory'

// ADR 0127 (the plan's Phase B): ADR 0065 §3's rule holds unchanged here -
// "a scan never moves, marks missing or deletes anything." Every write this
// section makes is either automatic-but-narrow (confirming verified_aboard,
// which only ever turns false into true) or gated behind an explicit press
// (Move). Two independent scan inputs, neither a fallback for the other -
// Start scanning (NFC, Chrome/Android only) and the focused text field
// (a keyboard-wedge reader, or a pasted URL/bin code, ending on Enter -
// ADR 0065's original USB-reader path, and what makes this section
// testable on desktop).

interface CurrentBin {
  zone: InventoryZone
  bin: InventoryBin
}

type ScanEvent =
  | { id: string; kind: 'bin'; code: string; zoneName: string }
  | { id: string; kind: 'confirmed'; item: EquipmentItem }
  // targetBin is the bin that was current WHEN THIS ITEM WAS SCANNED, not a
  // live reference to currentBin - a later scan of a different bin must not
  // retarget an already-reported card (the review finding this fixes: the
  // Move button used to read the live currentBin at render/click time, so
  // scanning bin Y after this event turned it into "Move to Y").
  | { id: string; kind: 'elsewhere'; item: EquipmentItem; recordedBinCode: string | null; targetBin: CurrentBin }
  | { id: string; kind: 'unrecognised'; text: string }

interface StocktakeSectionProps {
  /** The bin photo grid's own Open (BinPage's own onOpenEquipment) - wired
   * from InventoryPanel/App the same way BinPage's is, so tapping a photo's
   * Open here reaches the equipment editor instead of doing nothing. */
  onOpenEquipment?: (id: string) => void
  /** Same write gate EquipmentIndex/LocationsSection/ProfilesSection already
   * take. A read-only session still gets to run a stocktake - a scan still
   * reports what it found - but nothing is written, and Move (the one press
   * that writes) is hidden rather than shown and then rejected. */
  canWrite?: boolean
}

export function StocktakeSection({ onOpenEquipment = () => {}, canWrite = true }: StocktakeSectionProps) {
  // Review finding: only `zones` used to be read here - a genuine fetch
  // failure left it at its initial `[]` with no way to tell that apart from
  // "the tree loaded fine and this code just doesn't exist", so
  // findBinByCode found nothing either way and every bin scan during an
  // outage was reported as an unrecognised tag (AGENTS.md fallback policy:
  // the real reason has to surface, not fold into a bucket that means
  // something else).
  const { zones, loading: zonesLoading, error: zonesError } = useInventoryZones()
  const [currentBin, setCurrentBin] = useState<CurrentBin | null>(null)
  // Review finding: handleScan is a plain closure over `currentBin` (a
  // render-scoped variable), and that specific closure is what a stale
  // handleScanRef (below) can still be holding when a follow-up scan
  // arrives faster than a render can commit. Even setting that aside, a
  // SINGLE handleScan call reads `currentBin` before its own `await
  // fetchEquipment` and again after it to decide confirmed vs elsewhere -
  // a closure variable can't pick up a bin scanned by a LATER call while
  // this one is still suspended, no matter how fresh that later call's own
  // closure is. A ref's `.current` is shared by every closure and mutated
  // synchronously the instant a bin is scanned, so reading THIS after the
  // await - not the closure's own `currentBin` - is what makes the decision
  // correct regardless of which closure is asking or when.
  const currentBinRef = useRef<CurrentBin | null>(null)
  const [events, setEvents] = useState<ScanEvent[]>([])
  const [movingId, setMovingId] = useState<string | null>(null)
  const [scanFieldValue, setScanFieldValue] = useState('')
  const [scanning, setScanning] = useState(false)
  const [scanError, setScanError] = useState<string | null>(null)
  const abortRef = useRef<AbortController | null>(null)

  // null (rather than a bin id chosen to never match a real one) fetches
  // nothing at all until the operator has actually scanned a bin this pass -
  // useEquipment(null)'s own contract.
  const { items: binItems } = useEquipment(currentBin ? { bin: currentBin.bin.id } : null)

  // Derived from events rather than a second piece of state kept in step
  // with it by hand - every 'confirmed' event's own item.id IS the set of
  // ids confirmed so far, so there is exactly one thing to keep correct
  // (pushEvent/setEvents below) instead of two.
  const confirmedIds = useMemo(
    () => new Set(
      events
        .filter((e): e is Extract<ScanEvent, { kind: 'confirmed' }> => e.kind === 'confirmed')
        .map((e) => e.item.id),
    ),
    [events],
  )

  // Takes a fully-formed ScanEvent (id included) rather than an Omit<...,
  // 'id'> - Omit collapses a discriminated union to the INTERSECTION of its
  // members' keys, which would reject every branch's own extra fields
  // (code, item, text...) at every call site below.
  const pushEvent = (event: ScanEvent) => {
    setEvents((prev) => [event, ...prev])
  }

  const handleScan = async (rawText: string) => {
    const text = rawText.trim()
    if (text === '') return
    setScanError(null)

    // "Accepts a keyboard-wedge or pasted URL or bin code" - `new URL(text)`
    // only succeeds for an ABSOLUTE url (a scheme included), which a scan
    // typed into a boat's own tailnet address bar without "https://" is not
    // (e.g. "boat.tailnet.ts.net/inventory/bins/LAZ-02"). Treating THAT
    // whole string as a bare bin code (the naive fallback this replaced)
    // resolved to the wrong bin - or, worse, silently to none at all. So a
    // scheme-less scan is read three ways, in order: an /inventory/ path
    // pulled out of wherever it starts in the string; failing that, a bare
    // bin code ONLY if there's no slash in it at all (a real bin code never
    // has one); anything else is reported as unrecognised rather than
    // guessed at.
    let pathname: string
    try {
      pathname = new URL(text).pathname
    } catch {
      const inventoryIndex = text.indexOf('/inventory/')
      if (inventoryIndex !== -1) {
        pathname = text.slice(inventoryIndex)
      } else if (!text.includes('/')) {
        pathname = `/inventory/bins/${text}`
      } else {
        pushEvent({ id: crypto.randomUUID(), kind: 'unrecognised', text })
        return
      }
    }
    const parsed = parseAppLocation(pathname)

    if (parsed.panel === 'inventory' && parsed.inventorySection === 'locations' && parsed.binCode) {
      // Review finding: neither of these is "this code doesn't exist" - a
      // bin scan taken while the zone tree hasn't loaded yet (or failed to)
      // can't be resolved either way, so it is reported for what it
      // actually is instead of being folded into "unrecognised". The zones
      // error itself is also shown plainly, below (AGENTS.md fallback
      // policy).
      if (zonesLoading) {
        setScanError('Locations still loading - scan that bin again in a moment.')
        return
      }
      if (zonesError) return
      const match = findBinByCode(zones, parsed.binCode)
      if (!match) {
        pushEvent({ id: crypto.randomUUID(), kind: 'unrecognised', text })
        return
      }
      // Set synchronously, before setCurrentBin - a plain assignment isn't
      // batched the way a state update is, so any handleScan call already
      // suspended on its own await (elsewhere) sees this the instant it
      // resumes, not whenever React next commits.
      currentBinRef.current = match
      setCurrentBin(match)
      pushEvent({ id: crypto.randomUUID(), kind: 'bin', code: match.bin.code, zoneName: match.zone.name })
      return
    }

    if (parsed.panel === 'inventory' && parsed.inventorySection === 'equipment' && parsed.equipmentEditId) {
      let item: EquipmentItem
      try {
        item = await fetchEquipment(parsed.equipmentEditId)
      } catch (err) {
        // Review finding: every fetchEquipment failure used to be logged as
        // an unrecognised scan - a genuine server error (a 500, a dropped
        // connection) looked identical to "not an inventory tag", hiding
        // the real problem instead of showing it (AGENTS.md fallback
        // policy). Only a 404 - EquipmentNotFoundError, thrown specifically
        // for that status - actually means "no such item."
        if (err instanceof EquipmentNotFoundError) {
          pushEvent({ id: crypto.randomUUID(), kind: 'unrecognised', text })
          return
        }
        setScanError(err instanceof Error ? err.message : String(err))
        return
      }

      // Review finding: reads the REF here, not the `currentBin` closed over
      // above - a bin scanned by a later handleScan call, while this one was
      // still suspended on the fetchEquipment await just above, has already
      // updated it synchronously even though this call's own `currentBin`
      // variable is frozen at whatever it was when this closure was created.
      const binNow = currentBinRef.current

      // ADR 0127: "An item scanned with no current bin is confirmed in
      // place" - the same branch as "recorded in the current bin", just
      // with nothing to compare the bin against.
      if (binNow === null || item.bin_id === binNow.bin.id) {
        pushEvent({ id: crypto.randomUUID(), kind: 'confirmed', item })
        if (canWrite && !item.verified_aboard) {
          try {
            // Fetched fresh immediately before the write (review finding:
            // reusing the copy fetched above, moments earlier in this same
            // call, is close to safe but not - see handleMove's own comment
            // for why this whole function never trusts an in-hand copy for
            // a write body) and only verified_aboard is changed on it.
            const fresh = await fetchEquipment(item.id)
            await updateEquipment(item.id, { ...toEquipmentInput(fresh), verified_aboard: true })
          } catch (err) {
            setScanError(err instanceof Error ? err.message : String(err))
          }
        }
        return
      }

      pushEvent({ id: crypto.randomUUID(), kind: 'elsewhere', item, recordedBinCode: item.bin_code || null, targetBin: binNow })
      return
    }

    pushEvent({ id: crypto.randomUUID(), kind: 'unrecognised', text })
  }

  const handleMove = async (eventId: string, item: EquipmentItem, targetBin: CurrentBin) => {
    setMovingId(item.id)
    setScanError(null)
    try {
      // AGENTS.md fallback policy / ADR 0065 §3: this PUT runs ONLY on the
      // explicit press below - never as a side effect of the scan that
      // first reported the item as elsewhere.
      //
      // Fetched fresh right here, immediately before the write, rather than
      // reusing the copy the 'elsewhere' event has held since the scan - a
      // review finding: an operator can scan several more tags, or another
      // session can edit the record, in the time between "Recorded in
      // SAL-04" appearing and Move actually being pressed, and the stale
      // copy's OTHER fields (name, quantity, notes...) would overwrite
      // whatever changed. Only bin_id/zone_id are ever changed on the fresh
      // copy.
      const fresh = await fetchEquipment(item.id)
      const updated = await updateEquipment(item.id, { ...toEquipmentInput(fresh), bin_id: targetBin.bin.id, zone_id: targetBin.zone.id })
      setEvents((prev) => prev.map((e) => (e.id === eventId ? { id: e.id, kind: 'confirmed', item: updated } : e)))
    } catch (err) {
      setScanError(err instanceof Error ? err.message : String(err))
    } finally {
      setMovingId(null)
    }
  }

  const handleScanFieldKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== 'Enter') return
    e.preventDefault()
    const value = scanFieldValue
    setScanFieldValue('')
    void handleScan(value)
  }

  // Review finding: scanTags' own onUrl callback is created ONCE, when Start
  // scanning is pressed, and Web NFC keeps calling that exact function for
  // every tap for the rest of the session - it never re-subscribes the way
  // a React prop would. handleScan is a fresh closure every render (it
  // reads currentBin/zones directly, not via a ref), so a bare `(url) => {
  // void handleScan(url) }` passed straight to scanTags froze whichever
  // currentBin/zones were current AT THAT MOMENT - null, since scanning
  // always starts before any bin has been scanned - for every scan for the
  // rest of the session, even though ordinary state updates (setCurrentBin)
  // kept the screen itself showing the right bin throughout. Routing
  // through a ref that's kept current on every render is what makes the
  // NFC callback see whichever handleScan closure is actually current.
  const handleScanRef = useRef(handleScan)
  // Review finding: an ordinary (passive) effect runs after the browser has
  // had a chance to paint, which a fast enough follow-up NFC tap can beat -
  // useLayoutEffect runs synchronously right after React commits the DOM
  // update, closing that window as far as a re-subscription can.
  // currentBinRef above is what actually makes a SINGLE in-flight call
  // correct regardless of timing; this just gets the NEXT call a fresh
  // closure sooner.
  useLayoutEffect(() => { handleScanRef.current = handleScan })

  const handleStartScanning = async () => {
    setScanError(null)
    setScanning(true)
    const controller = new AbortController()
    abortRef.current = controller
    try {
      await scanTags((url) => { void handleScanRef.current(url) }, controller.signal)
    } catch (err) {
      setScanError(err instanceof Error ? err.message : String(err))
      setScanning(false)
    }
  }

  const handleStopScanning = () => {
    abortRef.current?.abort()
    abortRef.current = null
    setScanning(false)
  }

  // Web NFC's scan keeps running until its own AbortController is aborted -
  // leaving this section without pressing Stop first (a nav-away, closing
  // the tab) would otherwise leave the reader open, still invoking
  // handleScanRef.current against a component that no longer exists.
  useEffect(() => () => { abortRef.current?.abort() }, [])

  const notSeen = currentBin ? binItems.filter((item) => !confirmedIds.has(item.id)) : []

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-4">
      <div className="flex flex-col gap-2 rounded-md border border-border bg-card p-4">
        <div className="flex flex-wrap items-center gap-2">
          {nfcSupported() ? (
            <Button type="button" onClick={() => { if (scanning) { handleStopScanning() } else { void handleStartScanning() } }}>
              {scanning ? 'Stop scanning' : 'Start scanning'}
            </Button>
          ) : (
            <p className="text-[11px] text-muted-foreground">
              Scanning with a phone needs Chrome on Android. A keyboard-wedge reader works below either way.
            </p>
          )}
          <Input
            aria-label="Scan"
            placeholder="Scan, or type a bin code and press Enter"
            value={scanFieldValue}
            onChange={(e) => setScanFieldValue(e.target.value)}
            onKeyDown={handleScanFieldKeyDown}
            className="min-w-0 flex-1"
          />
        </div>
        {!canWrite && (
          <p className="text-[11px] text-muted-foreground">This session is read-only - scans are checked but nothing is written.</p>
        )}
        {scanError && (
          <p role="alert" className="text-sm text-destructive">{scanError}</p>
        )}
        {zonesError && (
          <p role="alert" className="text-sm text-destructive">{zonesError}</p>
        )}
      </div>

      {currentBin && (
        <div className="flex flex-col gap-1 rounded-md border border-border bg-card p-4">
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{currentBin.zone.name}</p>
          <h2 className="font-mono text-lg font-semibold">{currentBin.bin.code}</h2>
        </div>
      )}

      {events.length > 0 && (
        <div className="flex flex-col gap-2 rounded-md border border-border bg-card p-3">
          {events.map((event) => (
            <div key={event.id} className="flex min-w-0 items-center justify-between gap-2 border-b border-border pb-2 text-sm last:border-0 last:pb-0">
              {event.kind === 'bin' && (
                <span className="min-w-0 truncate">
                  Bin <span className="font-mono">{event.code}</span> ({event.zoneName})
                </span>
              )}
              {event.kind === 'confirmed' && (
                <>
                  <span className="min-w-0 flex-1 truncate">{event.item.name}</span>
                  <span className="shrink-0 text-[11px] font-medium uppercase tracking-wider text-primary">Confirmed</span>
                </>
              )}
              {event.kind === 'elsewhere' && (
                <>
                  <span className="min-w-0 flex-1 truncate">
                    {event.item.name} - {event.recordedBinCode ? `Recorded in ${event.recordedBinCode}` : 'Not filed'}
                  </span>
                  {canWrite && (
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="shrink-0"
                      disabled={movingId === event.item.id}
                      onClick={() => { void handleMove(event.id, event.item, event.targetBin) }}
                    >
                      {movingId === event.item.id ? 'Moving...' : `Move to ${event.targetBin.bin.code}`}
                    </Button>
                  )}
                </>
              )}
              {event.kind === 'unrecognised' && (
                <span className="min-w-0 truncate text-muted-foreground">Not an inventory tag: {event.text}</span>
              )}
            </div>
          ))}
        </div>
      )}

      {currentBin && <BinPhotoGrid items={binItems} onOpenEquipment={onOpenEquipment} />}

      {currentBin && notSeen.length > 0 && (
        <div className="flex flex-col gap-1 rounded-md border border-border bg-card p-3">
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Not seen this pass</p>
          {notSeen.map((item) => (
            <p key={item.id} className="truncate text-sm text-muted-foreground">{item.name}</p>
          ))}
        </div>
      )}
    </div>
  )
}

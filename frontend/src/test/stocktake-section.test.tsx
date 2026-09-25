import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { StocktakeSection } from '@/components/inventory/stocktake-section'
import { scanTags } from '@/lib/nfc'
import type { EquipmentItem, InventoryZone } from '@/hooks/use-inventory'

// ADR 0127 (the plan's Phase B): the manual scan field is what drives every
// test here - "a keyboard-wedge or pasted URL or bin code, ending in
// Enter" is deliberately also what makes this section testable on desktop
// with no NFC hardware (the plan's own words). lib/nfc.ts's own behaviour
// is covered by nfc.test.ts/tag-row.test.tsx, not re-tested here.
//
// lib/nfc.ts IS mocked (module-wide, not per-test) for the "Start
// scanning" describe block near the bottom of this file, which needs
// nfcSupported() true to reach the button at all and a controllable
// scanTags to drive the NFC callback directly - a real NDEFReader has no
// jsdom stand-in. The manual-scan-field tests above never touch either
// mock (nfcSupported's own default below only changes whether the button
// is ALSO on screen; they drive the text field regardless).
vi.mock('@/lib/nfc', () => ({
  nfcSupported: vi.fn(() => true),
  scanTags: vi.fn(),
}))
const mockedScanTags = vi.mocked(scanTags)

function makeItem(overrides: Partial<EquipmentItem> = {}): EquipmentItem {
  return {
    id: 'eq-1',
    name: 'Spare impeller',
    category: 'general',
    system: 'other',
    manufacturer: '',
    model: '',
    serial: '',
    quantity: 1,
    status: 'stored',
    zone_id: 'z1',
    bin_id: 'b1',
    zone_name: 'Lazarette',
    bin_code: 'LAZ-02',
    location_detail: '',
    install_date: '',
    hour_meter_path: '',
    profile_id: '',
    aliases: [],
    verified_aboard: false,
    notes: '',
    link_count: 0,
    created_at: '',
    updated_at: '',
    photo_ids: [],
    exclusive_photo_ids: [],
    ...overrides,
  }
}

let zones: InventoryZone[]
let binItemsByBinId: Record<string, EquipmentItem[]>
let equipmentById: Record<string, EquipmentItem>
let putCalls: { id: string; body: Record<string, unknown> }[]
const fetchMock = vi.fn()

beforeEach(() => {
  zones = [
    {
      id: 'z1', name: 'Lazarette', sort_index: 0,
      bins: [{ id: 'b1', zone_id: 'z1', code: 'LAZ-02', name: '', sort_index: 0 }],
    },
  ]
  binItemsByBinId = {}
  equipmentById = {}
  putCalls = []
  fetchMock.mockReset()
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    const u = String(url)
    const method = init?.method ?? 'GET'

    if (u.endsWith('/api/inventory/zones') && method === 'GET') {
      return Promise.resolve({ ok: true, json: async () => ({ zones }) })
    }
    const binMatch = u.match(/\/api\/inventory\/equipment\?bin=([^&]+)/)
    if (binMatch && method === 'GET') {
      const binId = decodeURIComponent(binMatch[1])
      return Promise.resolve({ ok: true, json: async () => ({ items: binItemsByBinId[binId] ?? [] }) })
    }
    const idMatch = u.match(/\/api\/inventory\/equipment\/([^/?]+)$/)
    if (idMatch && method === 'GET') {
      const item = equipmentById[idMatch[1]]
      if (!item) return Promise.resolve({ ok: false, status: 404, json: async () => ({ error: 'equipment not found' }) })
      return Promise.resolve({ ok: true, json: async () => ({ item }) })
    }
    if (idMatch && method === 'PUT') {
      const body = JSON.parse(String(init?.body)) as Record<string, unknown>
      putCalls.push({ id: idMatch[1], body })
      const updated = { ...equipmentById[idMatch[1]], ...body } as EquipmentItem
      equipmentById[idMatch[1]] = updated
      return Promise.resolve({ ok: true, json: async () => ({ item: updated }) })
    }
    return Promise.resolve({ ok: false, json: async () => ({ error: 'not found' }) })
  })
  vi.stubGlobal('fetch', fetchMock)
})

async function scan(text: string) {
  const input = screen.getByLabelText('Scan')
  fireEvent.change(input, { target: { value: text } })
  fireEvent.keyDown(input, { key: 'Enter' })
}

describe('StocktakeSection', () => {
  it('confirms an item recorded in the current bin and PUTs verified_aboard=true', async () => {
    const item = makeItem({ id: 'eq-1', bin_id: 'b1', verified_aboard: false })
    equipmentById['eq-1'] = item
    binItemsByBinId['b1'] = [item]
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })

    await scan('https://boat.example/inventory/equipment/eq-1')

    await screen.findByText('Confirmed')
    await waitFor(() => {
      const call = putCalls.find((c) => c.id === 'eq-1')
      expect(call).toBeDefined()
      expect(call?.body.verified_aboard).toBe(true)
    })
  })

  // Review finding: the confirm path fetched the item once to classify the
  // scan and again, milliseconds later in the same call, for the write body -
  // a doubled round trip on the busiest path of a stocktake. The classifying
  // fetch is itself fresh, so one GET per confirmed scan is enough (Move,
  // pressed later, keeps its own re-fetch).
  it('confirms a scanned item with one GET for it, not two', async () => {
    const item = makeItem({ id: 'eq-1', bin_id: 'b1', verified_aboard: false })
    equipmentById['eq-1'] = item
    binItemsByBinId['b1'] = [item]
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })

    await scan('https://boat.example/inventory/equipment/eq-1')
    await waitFor(() => expect(putCalls.find((c) => c.id === 'eq-1')).toBeDefined())

    const itemGets = fetchMock.mock.calls.filter(([url, init]) =>
      /\/api\/inventory\/equipment\/eq-1$/.test(String(url)) && (init?.method ?? 'GET') === 'GET')
    expect(itemGets).toHaveLength(1)
  })

  it('offers Move for an item recorded elsewhere, and writes nothing until it is pressed', async () => {
    const item = makeItem({ id: 'eq-2', bin_id: 'b2', bin_code: 'SAL-04', zone_id: 'z2' })
    equipmentById['eq-2'] = item
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })

    await scan('https://boat.example/inventory/equipment/eq-2')

    await screen.findByText(/Recorded in SAL-04/)
    expect(putCalls.find((c) => c.id === 'eq-2')).toBeUndefined()

    fireEvent.click(screen.getByRole('button', { name: 'Move to LAZ-02' }))

    await waitFor(() => {
      const call = putCalls.find((c) => c.id === 'eq-2')
      expect(call).toBeDefined()
      expect(call?.body.bin_id).toBe('b1')
      expect(call?.body.zone_id).toBe('z1')
    })
  })

  it('Move targets the bin that was current when the item was scanned, not whatever is current when pressed', async () => {
    zones = [
      {
        id: 'z1', name: 'Lazarette', sort_index: 0,
        bins: [
          { id: 'b1', zone_id: 'z1', code: 'LAZ-02', name: '', sort_index: 0 },
          { id: 'b3', zone_id: 'z1', code: 'FWD-01', name: '', sort_index: 1 },
        ],
      },
    ]
    const item = makeItem({ id: 'eq-2', bin_id: 'b2', bin_code: 'SAL-04', zone_id: 'z2' })
    equipmentById['eq-2'] = item
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })

    await scan('https://boat.example/inventory/equipment/eq-2')
    await screen.findByText(/Recorded in SAL-04/)
    expect(screen.getByRole('button', { name: 'Move to LAZ-02' })).toBeInTheDocument()

    // Scanning a DIFFERENT bin afterward must not retarget an already
    // reported card - its own Move button still reads (and writes) LAZ-02.
    await scan('https://boat.example/inventory/bins/FWD-01')
    await screen.findByRole('heading', { name: 'FWD-01' })

    expect(screen.getByRole('button', { name: 'Move to LAZ-02' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Move to LAZ-02' }))

    await waitFor(() => {
      const call = putCalls.find((c) => c.id === 'eq-2')
      expect(call).toBeDefined()
      expect(call?.body.bin_id).toBe('b1')
      expect(call?.body.zone_id).toBe('z1')
    })
  })

  it('Move fetches the item fresh immediately before writing, keeping a name changed server-side since the scan', async () => {
    const item = makeItem({ id: 'eq-2', bin_id: 'b2', bin_code: 'SAL-04', zone_id: 'z2', name: 'Old name' })
    equipmentById['eq-2'] = item
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })

    await scan('https://boat.example/inventory/equipment/eq-2')
    await screen.findByText(/Recorded in SAL-04/)

    // The record changes server-side AFTER the scan, before Move is pressed.
    equipmentById['eq-2'] = { ...equipmentById['eq-2'], name: 'New name' }

    fireEvent.click(screen.getByRole('button', { name: 'Move to LAZ-02' }))

    await waitFor(() => {
      const call = putCalls.find((c) => c.id === 'eq-2')
      expect(call).toBeDefined()
      expect(call?.body.name).toBe('New name')
    })
  })

  it('lists an unseen item under "Not seen this pass" and writes nothing for it', async () => {
    const seen = makeItem({ id: 'eq-1', name: 'Spare impeller', bin_id: 'b1' })
    const unseen = makeItem({ id: 'eq-2', name: 'Fuel filter', bin_id: 'b1' })
    equipmentById['eq-1'] = seen
    equipmentById['eq-2'] = unseen
    binItemsByBinId['b1'] = [seen, unseen]
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })
    // The bin's own contents load once scanned in - both items appear
    // (photo grid) before either has been confirmed.
    await waitFor(() => expect(screen.getAllByText('Fuel filter').length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/equipment/eq-1')
    await screen.findByText('Confirmed')

    await screen.findByText('Not seen this pass')
    expect(putCalls.find((c) => c.id === 'eq-2')).toBeUndefined()
  })

  it('resolves a scan with no https:// scheme via its /inventory/ path', async () => {
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    // A tailnet host pasted or scanned without its scheme - `new URL(...)`
    // throws on this, and the bug being fixed here used to fall back to
    // treating the WHOLE string (host and all) as a bin code.
    await scan('boat.tailnet.ts.net/inventory/bins/LAZ-02')

    await screen.findByRole('heading', { name: 'LAZ-02' })
    expect(screen.queryByText(/Not an inventory tag/)).not.toBeInTheDocument()
  })

  it('reports a schemeless scan with a slash but no /inventory/ path as not an inventory tag', async () => {
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('some/other/path')

    await screen.findByText('Not an inventory tag: some/other/path')
  })

  // Release-fixes code-review finding: only `zones` was destructured from
  // useInventoryZones() - a genuine fetch failure left `zones` at its
  // initial `[]`, so findBinByCode found nothing and every bin scan was
  // reported as "Not an inventory tag", indistinguishable from an actually
  // unknown code (AGENTS.md fallback policy: the real reason has to surface,
  // not fold into a bucket that means something else).
  it('shows a zones fetch failure plainly, and does not classify a bin scan as unrecognised because of it', async () => {
    fetchMock.mockImplementation((url: string) => {
      const u = String(url)
      if (u.endsWith('/api/inventory/zones')) {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({ error: 'locations unavailable' }) })
      }
      return Promise.resolve({ ok: false, status: 404, json: async () => ({ error: 'not found' }) })
    })

    render(<StocktakeSection />)

    await screen.findByText('locations unavailable')

    await scan('https://boat.example/inventory/bins/LAZ-02')
    expect(screen.queryByText(/Not an inventory tag/)).not.toBeInTheDocument()
  })

  it('reports a bin scan as "Locations still loading" rather than unrecognised while zones are still fetching', async () => {
    let resolveZones!: (value: unknown) => void
    fetchMock.mockImplementation((url: string) => {
      const u = String(url)
      if (u.endsWith('/api/inventory/zones')) {
        return new Promise((resolve) => { resolveZones = resolve })
      }
      return Promise.resolve({ ok: false, status: 404, json: async () => ({ error: 'not found' }) })
    })

    render(<StocktakeSection />)

    await scan('https://boat.example/inventory/bins/LAZ-02')

    await screen.findByText(/Locations still loading/)
    expect(screen.queryByText(/Not an inventory tag/)).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'LAZ-02' })).not.toBeInTheDocument()

    await act(async () => {
      resolveZones({ ok: true, json: async () => ({ zones }) })
      await Promise.resolve()
    })

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })
  })

  it('reports an unrecognised scan without writing anything', async () => {
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('not a url or a known bin code')

    await screen.findByText('Not an inventory tag: not a url or a known bin code')
    expect(putCalls).toHaveLength(0)
  })

  it('reports a well-formed equipment scan for an id that genuinely does not exist (404) as unrecognised', async () => {
    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    // No 'eq-ghost' in equipmentById - the fixture's default GET answers 404.
    await scan('https://boat.example/inventory/equipment/eq-ghost')

    await screen.findByText('Not an inventory tag: https://boat.example/inventory/equipment/eq-ghost')
  })

  // Review finding: any fetchEquipment failure used to be logged as an
  // unrecognised scan - a genuine server error (a 500, a dropped
  // connection) looked identical to "you scanned something that isn't an
  // inventory tag", silently hiding the real problem instead of showing it
  // (AGENTS.md fallback policy).
  it('shows a non-404 fetch failure as an error, not an unrecognised scan', async () => {
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (u.endsWith('/api/inventory/zones') && method === 'GET') {
        return Promise.resolve({ ok: true, json: async () => ({ zones }) })
      }
      if (u.match(/\/api\/inventory\/equipment\/eq-1$/) && method === 'GET') {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({ error: 'database unavailable' }) })
      }
      return Promise.resolve({ ok: false, status: 404, json: async () => ({ error: 'not found' }) })
    })

    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/equipment/eq-1')

    await screen.findByText('database unavailable')
    expect(screen.queryByText(/Not an inventory tag/)).not.toBeInTheDocument()
  })

  // Release-fixes code-review finding: App.tsx's Open/Full item used to
  // switch straight to the Equipment section with no guard, silently
  // clearing whatever a stocktake pass had already confirmed. This section
  // reports that work upward the way the equipment editor reports dirty -
  // App.tsx's own guard is exercised in app-inventory-navigation.test.tsx.
  // A bare bin scan (no item confirmed against it yet) is deliberately NOT
  // "work" - it's the normal first step of scanning INTO a bin, trivially
  // repeated by rescanning the same tag, and the existing "Open from
  // Stocktake's photo grid" flow (app-inventory-navigation.test.tsx) relies
  // on that scan-then-Open sequence staying frictionless.
  it('reports hasWork once an item is confirmed, but not from a bare bin scan', async () => {
    const item = makeItem({ id: 'eq-1', bin_id: 'b1', verified_aboard: true })
    equipmentById['eq-1'] = item
    const onHasWorkChange = vi.fn()
    render(<StocktakeSection onHasWorkChange={onHasWorkChange} />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))
    expect(onHasWorkChange).toHaveBeenLastCalledWith(false)

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })
    expect(onHasWorkChange).toHaveBeenLastCalledWith(false)

    await scan('https://boat.example/inventory/equipment/eq-1')
    await screen.findByText('Confirmed')
    expect(onHasWorkChange).toHaveBeenLastCalledWith(true)
  })

  it('wires the bin photo grid\'s Open button to onOpenEquipment', async () => {
    const item = makeItem({ id: 'eq-1', bin_id: 'b1', photo_ids: [] })
    equipmentById['eq-1'] = item
    binItemsByBinId['b1'] = [item]
    const onOpenEquipment = vi.fn()
    render(<StocktakeSection onOpenEquipment={onOpenEquipment} />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await waitFor(() => expect(screen.getAllByText('Spare impeller').length).toBeGreaterThan(0))

    fireEvent.click(screen.getByRole('button', { name: 'Open' }))
    expect(onOpenEquipment).toHaveBeenCalledWith('eq-1')
  })

  it('is read-only when canWrite is false: confirms without writing and hides Move', async () => {
    const item = makeItem({ id: 'eq-1', bin_id: 'b1', verified_aboard: false })
    const other = makeItem({ id: 'eq-2', bin_id: 'b2', bin_code: 'SAL-04', zone_id: 'z2' })
    equipmentById['eq-1'] = item
    equipmentById['eq-2'] = other
    render(<StocktakeSection canWrite={false} />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    expect(screen.getByText(/read-only/i)).toBeInTheDocument()

    await scan('https://boat.example/inventory/bins/LAZ-02')
    await screen.findByRole('heading', { name: 'LAZ-02' })

    await scan('https://boat.example/inventory/equipment/eq-1')
    await screen.findByText('Confirmed')
    expect(putCalls.find((c) => c.id === 'eq-1')).toBeUndefined()

    await scan('https://boat.example/inventory/equipment/eq-2')
    await screen.findByText(/Recorded in SAL-04/)
    expect(screen.queryByRole('button', { name: /Move to/ })).not.toBeInTheDocument()
  })
})

// ── NFC scanning (Start scanning) ───────────────────────────────────────
// Review finding: scanTags' own onUrl callback - `(url) => { void
// handleScan(url) }` - is created ONCE, when Start scanning is pressed, and
// Web NFC keeps invoking that exact function for every tap for the rest of
// the session; it never re-subscribes the way a React prop would. Since
// handleScan is a fresh closure every render (not memoized), that callback
// froze whichever currentBin/zones were current AT THAT MOMENT - null,
// since scanning always starts before any bin has been scanned - so every
// item scanned afterward was judged against a currentBin that never
// updated, even though the screen itself (driven by ordinary state) showed
// the right bin throughout.
describe('StocktakeSection: NFC scanning', () => {
  beforeEach(() => {
    mockedScanTags.mockReset()
  })

  it('resolves an item scanned mid-session against the CURRENT bin, not the bin at Start scanning time', async () => {
    const item = makeItem({ id: 'eq-2', bin_id: 'b2', bin_code: 'SAL-04', zone_id: 'z2' })
    equipmentById['eq-2'] = item
    let onUrl: ((url: string) => void) | null = null
    mockedScanTags.mockImplementation(async (cb) => { onUrl = cb })

    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    fireEvent.click(screen.getByRole('button', { name: 'Start scanning' }))
    await waitFor(() => expect(onUrl).not.toBeNull())

    // Both taps go through the SAME callback scanTags was given at Start
    // scanning - exactly what a real NFC session does.
    act(() => { onUrl!('https://boat.example/inventory/bins/LAZ-02') })
    await screen.findByRole('heading', { name: 'LAZ-02' })

    act(() => { onUrl!('https://boat.example/inventory/equipment/eq-2') })

    // The stale closure bug always saw currentBin as null here, so this
    // wrongly took the "confirmed in place" branch instead of "elsewhere".
    await screen.findByText(/Recorded in SAL-04/)
    expect(screen.getByRole('button', { name: 'Move to LAZ-02' })).toBeInTheDocument()
    expect(screen.queryByText('Confirmed')).not.toBeInTheDocument()
  })

  // Release-fixes code-review finding: handleScan reads currentBin from its
  // OWN closure (a plain render-scoped variable), and that closure is what's
  // still sitting in handleScanRef.current until the passive effect below
  // re-subscribes it - a real double-tap (bin, then item, faster than a
  // render can commit) calls the SAME stale closure for both, so the item
  // scan's own currentBin read - even AFTER its fetchEquipment await - still
  // sees whatever currentBin was when that closure was first created, not
  // the bin just scanned a moment before it.
  it('judges an item scan against the bin scanned immediately before it, even when both share the one stale NFC closure', async () => {
    // Filed in a DIFFERENT bin than the one about to be scanned - a stale
    // "currentBin was null when this closure was created" read takes the
    // `currentBin === null` branch regardless (wrongly "confirmed in
    // place"), so only a genuinely fresh read of the just-scanned bin
    // produces the correct "elsewhere" verdict this test pins.
    const item = makeItem({ id: 'eq-2', bin_id: 'b2', bin_code: 'SAL-04', zone_id: 'z2' })
    equipmentById['eq-2'] = item
    let resolveItemGet!: (value: unknown) => void
    fetchMock.mockImplementation((url: string, init?: RequestInit) => {
      const u = String(url)
      const method = init?.method ?? 'GET'
      if (u.endsWith('/api/inventory/zones') && method === 'GET') {
        return Promise.resolve({ ok: true, json: async () => ({ zones }) })
      }
      const binMatch = u.match(/\/api\/inventory\/equipment\?bin=([^&]+)/)
      if (binMatch && method === 'GET') {
        const binId = decodeURIComponent(binMatch[1])
        return Promise.resolve({ ok: true, json: async () => ({ items: binItemsByBinId[binId] ?? [] }) })
      }
      if (u.match(/\/api\/inventory\/equipment\/eq-2$/) && method === 'GET') {
        return new Promise((resolve) => { resolveItemGet = resolve })
      }
      return Promise.resolve({ ok: false, status: 404, json: async () => ({ error: 'not found' }) })
    })

    let onUrl: ((url: string) => void) | null = null
    mockedScanTags.mockImplementation(async (cb) => { onUrl = cb })

    render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    fireEvent.click(screen.getByRole('button', { name: 'Start scanning' }))
    await waitFor(() => expect(onUrl).not.toBeNull())

    // Both calls go through the ONE handleScanRef.current from mount -
    // nothing in between gives React a chance to commit the bin scan's own
    // re-render and run the passive effect that would otherwise refresh it.
    act(() => {
      onUrl!('https://boat.example/inventory/bins/LAZ-02')
      onUrl!('https://boat.example/inventory/equipment/eq-2')
    })
    await screen.findByRole('heading', { name: 'LAZ-02' })

    // eq-2's own fetchEquipment only resolves now - well after the bin scan
    // above set the new current bin.
    await act(async () => {
      resolveItemGet({ ok: true, json: async () => ({ item }) })
      await Promise.resolve()
    })

    // Judged against LAZ-02 (the bin just scanned), not the null currentBin
    // the stale closure was created with.
    await screen.findByText(/Recorded in SAL-04/)
    expect(screen.queryByText('Confirmed')).not.toBeInTheDocument()
  })

  it('reports hasWork while NFC scanning is live, and clears it on Stop scanning', async () => {
    let onUrl: ((url: string) => void) | null = null
    mockedScanTags.mockImplementation(async (cb) => { onUrl = cb })
    const onHasWorkChange = vi.fn()
    render(<StocktakeSection onHasWorkChange={onHasWorkChange} />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))
    onHasWorkChange.mockClear()

    fireEvent.click(screen.getByRole('button', { name: 'Start scanning' }))
    await waitFor(() => expect(onUrl).not.toBeNull())
    expect(onHasWorkChange).toHaveBeenLastCalledWith(true)

    fireEvent.click(screen.getByRole('button', { name: 'Stop scanning' }))
    expect(onHasWorkChange).toHaveBeenLastCalledWith(false)
  })

  it('aborts the scan\'s AbortController on unmount', async () => {
    let capturedSignal: AbortSignal | undefined
    mockedScanTags.mockImplementation(async (_onUrl, signal) => { capturedSignal = signal })

    const { unmount } = render(<StocktakeSection />)
    await waitFor(() => expect(zones.length).toBeGreaterThan(0))

    fireEvent.click(screen.getByRole('button', { name: 'Start scanning' }))
    await waitFor(() => expect(capturedSignal).toBeDefined())
    expect(capturedSignal!.aborted).toBe(false)

    unmount()

    expect(capturedSignal!.aborted).toBe(true)
  })
})

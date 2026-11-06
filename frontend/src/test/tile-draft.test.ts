import { act, renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { CLUSTER_DEFAULT_SIZE } from '@/components/dashboard-bento-grid'
import {
  copyTileBelow,
  duplicateTileDraft,
  newTileDraft,
  placeBelow,
  saveTileDraft,
  tileKindOf,
  useTileDraft,
  type TileKind,
} from '@/hooks/use-tile-draft'
import type { DashboardLayoutItem, GaugeWidgetConfig } from '@/lib/dashboard-widgets'
import { POI_CATEGORY_IDS } from '@/lib/poi'

const gaugeConfig: GaugeWidgetConfig = {
  path: 'navigation.speedOverGround', label: 'SOG', display: 'numeric', quantity: 'raw', unit: 'raw',
}
const page: DashboardLayoutItem[] = [
  { id: 'battery-power', x: 0, y: 0, w: 4, h: 6 },
  { id: 'gauge:old', x: 4, y: 2, w: 3, h: 6, gauge: gaugeConfig },
]
// The lowest edge of `page`: gauge:old ends at row 8.
const BELOW = 8

describe('a new tile starts as a draft', () => {
  const kinds: TileKind[] = ['gauge', 'gaugeGroup', 'cluster', 'lamps', 'embed', 'poiMap']

  it.each(kinds)('%s sits at x=0 below every tile on the page with an id of its own kind', (kind) => {
    const draft = newTileDraft(kind, page)
    expect(draft.x).toBe(0)
    expect(draft.y).toBe(BELOW)
    expect(tileKindOf(draft.id)).toBe(kind)
    expect(page.some((w) => w.id === draft.id)).toBe(false)
  })

  it('sits at row 0 on an empty page', () => {
    expect(newTileDraft('embed', []).y).toBe(0)
  })

  it('gives an embed a wide blank footprint', () => {
    const d = newTileDraft('embed', [])
    expect([d.w, d.h, d.embed]).toEqual([6, 8, { title: '', url: '' }])
  })

  it('gives a gauge a small blank numeric footprint', () => {
    const d = newTileDraft('gauge', [])
    expect([d.w, d.h]).toEqual([3, 6])
    expect(d.gauge).toEqual({ path: '', label: '', display: 'numeric', quantity: 'raw', unit: 'raw' })
  })

  it('gives a gauge group one blank gauge, at the Alternator footprint', () => {
    const d = newTileDraft('gaugeGroup', [])
    expect([d.w, d.h]).toEqual([4, 7])
    expect(d.gaugeGroup?.title).toBe('')
    expect(d.gaugeGroup?.gauges).toEqual([{ path: '', label: '', display: 'numeric', quantity: 'raw', unit: 'raw' }])
  })

  it('gives an engine cluster its tall default size and an RPM ring', () => {
    const d = newTileDraft('cluster', [])
    expect([d.w, d.h]).toEqual([CLUSTER_DEFAULT_SIZE.w, CLUSTER_DEFAULT_SIZE.h])
    expect(d.cluster?.ring.label).toBe('RPM')
    expect(d.cluster?.centre.label).toBe('Hours')
    expect(d.cluster?.corners).toEqual([])
  })

  it('gives an indicator strip the full page width and one blank lamp', () => {
    const d = newTileDraft('lamps', [])
    expect([d.w, d.h]).toEqual([12, 3])
    expect(d.lamps).toEqual({ title: 'Status', lamps: [{ path: '', label: '' }], showCheck: true })
  })

  it('gives a Nearby map the wide split layout with every category', () => {
    const d = newTileDraft('poiMap', [])
    expect([d.w, d.h]).toEqual([12, 7])
    expect(d.poiMap).toEqual({
      title: '', rangeNm: 5, categories: [...POI_CATEGORY_IDS], layout: 'split', showAis: true, showTrail: false,
    })
  })
})

describe('placing below the page', () => {
  it('puts a tile at the left edge under the lowest tile', () => {
    expect(placeBelow({ id: 'clock', w: 2, h: 2 }, page)).toEqual({ id: 'clock', w: 2, h: 2, x: 0, y: BELOW })
  })
})

describe('saving a draft', () => {
  it('appends a draft that is not on the page yet, carrying the config', () => {
    const draft = newTileDraft('gauge', page)
    const next = saveTileDraft(page, draft, 'gauge', gaugeConfig)
    expect(next).toHaveLength(page.length + 1)
    expect(next.at(-1)).toMatchObject({ id: draft.id, x: 0, y: BELOW, gauge: gaugeConfig })
    expect(page).toHaveLength(2)
  })

  it('places an appended tile below the page as it is at save, not as it was at open', () => {
    const draft = newTileDraft('gauge', page)
    const grown = [...page, { id: 'clock', x: 0, y: 8, w: 4, h: 5 } as DashboardLayoutItem]
    expect(saveTileDraft(grown, draft, 'gauge', gaugeConfig).at(-1)?.y).toBe(13)
  })

  it('replaces the config in place, at the same spot, when the tile is already on the page', () => {
    const next = saveTileDraft(page, page[1], 'gauge', { ...gaugeConfig, label: 'Renamed' })
    expect(next).toHaveLength(2)
    expect(next[1]).toMatchObject({ id: 'gauge:old', x: 4, y: 2, gauge: { label: 'Renamed' } })
    expect(next[0]).toBe(page[0])
  })

  it('raises a gauge to the height its display needs, new or existing', () => {
    const radial = { ...gaugeConfig, display: 'radial' as const }
    const fresh = saveTileDraft(page, { ...newTileDraft('gauge', page), h: 1 }, 'gauge', radial).at(-1)
    const existing = saveTileDraft(page, { ...page[1], h: 1 }, 'gauge', radial)[1]
    expect(fresh?.h).toBeGreaterThan(1)
    expect(existing.h).toBeGreaterThan(1)
  })

  it('leaves a gauge group at the height it had', () => {
    const draft = newTileDraft('gaugeGroup', page)
    const next = saveTileDraft(page, draft, 'gaugeGroup', { title: 'Port', gauges: [gaugeConfig] })
    expect(next.at(-1)).toMatchObject({ h: 7, gaugeGroup: { title: 'Port' } })
  })

  it('puts a lamp strip config under lamps', () => {
    const config = { title: 'Lamps', lamps: [{ path: 'a', label: 'A' }], showCheck: false }
    expect(saveTileDraft(page, newTileDraft('lamps', page), 'lamps', config).at(-1)?.lamps).toEqual(config)
  })

  it.each([
    ['cluster', { title: 'Port', ring: gaugeConfig, centre: gaugeConfig, corners: [] }],
    ['embed', { title: 'Chart', url: 'https://example.com', frameless: false }],
    ['poiMap', { title: 'Near', rangeNm: 3, categories: [], layout: 'map', showAis: false, showTrail: true }],
  ] as const)('puts a %s config under its own key', (kind, config) => {
    const saved = saveTileDraft(page, newTileDraft(kind, page), kind, config as never).at(-1)
    expect(saved?.[kind]).toEqual(config)
  })

  it('refuses a config for another kind of tile', () => {
    expect(() => saveTileDraft(page, page[1], 'embed', { title: 't', url: 'u' })).toThrow(/not a embed tile/)
  })

  it('refuses a built-in tile rather than writing a key named undefined', () => {
    expect(() => saveTileDraft(page, page[0], 'gauge', gaugeConfig)).toThrow()
  })
})

describe('duplicating from the dialog', () => {
  it('gives one tile carrying the edits for a draft that was never saved', () => {
    const draft = newTileDraft('gaugeGroup', [])
    const next = duplicateTileDraft([], draft, 'gaugeGroup', { title: 'Port', gauges: [gaugeConfig] })
    expect(next).toHaveLength(1)
    expect(next[0].gaugeGroup?.title).toBe('Port')
    expect(next[0].id).not.toBe(draft.id)
  })

  it('leaves the saved original as it was and places the copy below everything', () => {
    const group: DashboardLayoutItem = {
      id: 'gauge-group:one', x: 0, y: 0, w: 4, h: 7, gaugeGroup: { title: 'Port', gauges: [gaugeConfig] },
    }
    const next = duplicateTileDraft([group], group, 'gaugeGroup', { title: 'Starboard', gauges: [gaugeConfig] })
    expect(next).toHaveLength(2)
    expect(next[0]).toBe(group)
    expect(next[1]).toMatchObject({ x: 0, y: 7, gaugeGroup: { title: 'Starboard' } })
  })

  it('does not share the gauges array between the copy and the original', () => {
    const group: DashboardLayoutItem = {
      id: 'gauge-group:one', x: 0, y: 0, w: 4, h: 7, gaugeGroup: { title: 'Port', gauges: [gaugeConfig] },
    }
    const next = duplicateTileDraft([group], group, 'gaugeGroup', group.gaugeGroup!)
    expect(next[1].gaugeGroup?.gauges).not.toBe(group.gaugeGroup?.gauges)
  })

  it('refuses a config for another kind of tile', () => {
    expect(() => duplicateTileDraft(page, page[1], 'cluster', {} as never)).toThrow()
  })
})

describe('copying a saved tile', () => {
  it('places a copy below the page and returns it to open', () => {
    const result = copyTileBelow(page, 'gauge:old')
    expect(result?.widgets).toHaveLength(3)
    expect(result?.copy).toMatchObject({ x: 0, y: BELOW, gauge: gaugeConfig })
    expect(result?.copy.id).not.toBe('gauge:old')
    expect(result?.widgets.at(-1)).toBe(result?.copy)
  })

  it('does nothing for a built-in tile or an unknown id', () => {
    expect(copyTileBelow(page, 'battery-power')).toBeNull()
    expect(copyTileBelow(page, 'gauge:missing')).toBeNull()
  })
})

type Patch = { widgets: DashboardLayoutItem[] }
const lastPatch = (fn: { mock: { calls: unknown[][] } }, n = 0) => fn.mock.calls[n][1] as Patch

describe('useTileDraft', () => {
  function setup(widgets: DashboardLayoutItem[] = page, hasPage = true) {
    const updatePage = vi.fn<(id: string, patch: Patch) => Promise<object | null>>(() => Promise.resolve({}))
    const pageRef = { current: hasPage ? { id: 'p1' } : null }
    const widgetsRef = { current: widgets }
    const hook = renderHook(() => useTileDraft({ pageRef, widgetsRef, updatePage }))
    return { ...hook, updatePage }
  }
  /** An updatePage whose answer the test gives later. */
  function slowSetup() {
    let answer: (v: object | null) => void = () => {}
    const updatePage = vi.fn<(id: string, patch: Patch) => Promise<object | null>>(() => new Promise((r) => { answer = r }))
    const widgetsRef = { current: page }
    const hook = renderHook(() => useTileDraft({ pageRef: { current: { id: 'p1' } }, widgetsRef, updatePage }))
    return { ...hook, updatePage, answer: (v: object | null) => act(async () => { answer(v) }) }
  }
  const clusterConfig = { title: 'Port', ring: gaugeConfig, centre: gaugeConfig, corners: [] }

  it('holds nothing until a tile is added', () => {
    expect(setup().result.current.draft).toBeNull()
  })

  it('writes nothing for a new tile until it is saved, and shows it to its own dialog only', () => {
    const { result, updatePage } = setup()
    act(() => result.current.addTile('embed'))
    expect(result.current.draft?.id.startsWith('embed:')).toBe(true)
    expect(result.current.draftOfKind('embed')).toBe(result.current.draft)
    expect(result.current.draftOfKind('gauge')).toBeNull()
    expect(updatePage).not.toHaveBeenCalled()
  })

  it('cancelling a new tile leaves nothing behind', () => {
    const { result, updatePage } = setup()
    act(() => result.current.addTile('poiMap'))
    act(() => result.current.cancelTile())
    expect(result.current.draft).toBeNull()
    expect(updatePage).not.toHaveBeenCalled()
  })

  it('saving appends the tile, writes the page and closes the draft', async () => {
    const { result, updatePage } = setup()
    act(() => result.current.addTile('gauge'))
    const id = result.current.draft!.id
    await act(() => result.current.saveTile('gauge', gaugeConfig))
    expect(result.current.draft).toBeNull()
    expect(updatePage).toHaveBeenCalledTimes(1)
    expect(updatePage.mock.calls[0][0]).toBe('p1')
    expect(lastPatch(updatePage).widgets).toHaveLength(3)
    expect(lastPatch(updatePage).widgets.at(-1)).toMatchObject({ id, y: BELOW })
  })

  it('opening a saved tile and saving edits it in place', async () => {
    const { result, updatePage } = setup()
    act(() => result.current.openTile(page[1]))
    expect(result.current.draftOfKind('gauge')).toBe(page[1])
    await act(() => result.current.saveTile('gauge', { ...gaugeConfig, label: 'Edited' }))
    expect(lastPatch(updatePage).widgets).toHaveLength(2)
    expect(lastPatch(updatePage).widgets[1].gauge?.label).toBe('Edited')
  })

  it('does not open a built-in tile', () => {
    const { result } = setup()
    act(() => result.current.openTile(page[0]))
    expect(result.current.draft).toBeNull()
  })

  it('a second add replaces the first draft', () => {
    const { result } = setup()
    act(() => result.current.addTile('embed'))
    act(() => result.current.addTile('lamps'))
    expect(result.current.draftOfKind('lamps')).not.toBeNull()
    expect(result.current.draftOfKind('embed')).toBeNull()
  })

  it('duplicating from the dialog writes the copy and closes the dialog', async () => {
    const { result, updatePage } = setup([])
    act(() => result.current.addTile('gaugeGroup'))
    await act(() => result.current.duplicateTile('gaugeGroup', { title: 'Port', gauges: [gaugeConfig] }))
    expect(result.current.draft).toBeNull()
    expect(lastPatch(updatePage).widgets).toHaveLength(1)
  })

  it('duplicating a saved tile writes the copy, then opens it', () => {
    const { result, updatePage } = setup()
    act(() => result.current.duplicateTileById('gauge:old'))
    expect(updatePage).toHaveBeenCalledTimes(1)
    expect(result.current.draftOfKind('gauge')?.id).not.toBe('gauge:old')
    expect(result.current.draft?.y).toBe(BELOW)
  })

  it('duplicating a built-in tile does nothing', () => {
    const { result, updatePage } = setup()
    act(() => result.current.duplicateTileById('battery-power'))
    expect(updatePage).not.toHaveBeenCalled()
    expect(result.current.draft).toBeNull()
  })

  it('with no page, writes nothing, leaves the draft open and says so', async () => {
    const { result, updatePage } = setup(page, false)
    act(() => result.current.addTile('embed'))
    await act(async () => {
      await expect(result.current.saveTile('embed', { title: 't', url: 'u' })).rejects.toThrow('Could not save the tile')
    })
    expect(updatePage).not.toHaveBeenCalled()
    expect(result.current.draftOfKind('embed')).not.toBeNull()
  })

  it('a refused write (updatePage answers null) rejects, so the dialog can say so, and the draft stays open', async () => {
    const { result, updatePage } = setup()
    updatePage.mockResolvedValueOnce(null)
    act(() => result.current.addTile('gauge'))
    await act(async () => {
      await expect(result.current.saveTile('gauge', gaugeConfig)).rejects.toThrow('Could not save the tile')
    })
    expect(result.current.draft).not.toBeNull()
    await act(() => result.current.saveTile('gauge', gaugeConfig))
    expect(result.current.draft).toBeNull()
  })

  it('a refused Save As rejects too', async () => {
    const { result, updatePage } = setup()
    updatePage.mockResolvedValueOnce(null)
    act(() => result.current.addTile('gaugeGroup'))
    await act(async () => {
      await expect(result.current.duplicateTile('gaugeGroup', { title: 'P', gauges: [gaugeConfig] })).rejects.toThrow('Could not save the tile')
    })
    expect(result.current.draft).not.toBeNull()
  })

  describe('a dialog that finishes late', () => {
    it('does not close the dialog that is open now when an older save lands', async () => {
      const { result, updatePage } = setup()
      act(() => result.current.addTile('cluster'))
      const old = result.current // the cluster dialog, mid zone commit
      act(() => old.cancelTile())
      act(() => result.current.openTile(page[1]))
      await act(() => old.saveTile('cluster', clusterConfig))
      expect(updatePage).not.toHaveBeenCalled()
      expect(result.current.draft).toBe(page[1])
    })

    it('does not close the dialog that is open now when an older duplicate and cancel land', async () => {
      const { result, updatePage } = setup()
      act(() => result.current.addTile('gaugeGroup'))
      const old = result.current
      act(() => old.cancelTile())
      act(() => result.current.openTile(page[1]))
      await act(async () => {
        await old.duplicateTile('gaugeGroup', { title: 'Port', gauges: [gaugeConfig] })
        old.cancelTile()
      })
      expect(updatePage).not.toHaveBeenCalled()
      expect(result.current.draft).toBe(page[1])
    })

    it('does not save a tile that was cancelled while it waited', async () => {
      const { result, updatePage } = setup()
      act(() => result.current.addTile('cluster'))
      const old = result.current
      act(() => old.cancelTile())
      await act(() => old.saveTile('cluster', clusterConfig))
      expect(updatePage).not.toHaveBeenCalled()
      expect(result.current.draft).toBeNull()
    })

    it('does not apply an old save to the same tile reopened', async () => {
      const { result, updatePage } = setup()
      act(() => result.current.openTile(page[1]))
      const old = result.current
      act(() => old.cancelTile())
      act(() => result.current.openTile(page[1]))
      await act(() => old.saveTile('gauge', gaugeConfig))
      expect(updatePage).not.toHaveBeenCalled()
      expect(result.current.draft).toBe(page[1])
    })
  })

  describe('a write that is out or fails', () => {
    it('keeps the draft open when the write fails, and a retry can succeed', async () => {
      const { result, updatePage } = setup()
      updatePage.mockResolvedValueOnce(null)
      act(() => result.current.addTile('cluster'))
      await act(async () => { await expect(result.current.saveTile('cluster', clusterConfig)).rejects.toThrow() })
      expect(result.current.draftOfKind('cluster')).not.toBeNull()
      await act(() => result.current.saveTile('cluster', clusterConfig))
      expect(updatePage).toHaveBeenCalledTimes(2)
      expect(result.current.draft).toBeNull()
    })

    it('keeps the draft open when a duplicate fails to write', async () => {
      const { result, updatePage } = setup()
      updatePage.mockResolvedValueOnce(null)
      act(() => result.current.addTile('gaugeGroup'))
      await act(async () => { await expect(result.current.duplicateTile('gaugeGroup', { title: 'P', gauges: [gaugeConfig] })).rejects.toThrow() })
      expect(result.current.draftOfKind('gaugeGroup')).not.toBeNull()
    })

    it('writes once for a double submit', async () => {
      const { result, updatePage, answer } = slowSetup()
      act(() => result.current.addTile('gauge'))
      const { saveTile } = result.current
      act(() => { void saveTile('gauge', gaugeConfig); void saveTile('gauge', gaugeConfig) })
      expect(updatePage).toHaveBeenCalledTimes(1)
      await answer({})
      expect(result.current.draft).toBeNull()
    })

    it('ignores the dialog closing itself while the write is out, then closes on success', async () => {
      const { result, answer } = slowSetup()
      act(() => result.current.addTile('embed'))
      const { saveTile, cancelTile } = result.current
      act(() => { void saveTile('embed', { title: 't', url: 'u' }); cancelTile() })
      expect(result.current.draftOfKind('embed')).not.toBeNull()
      await answer({})
      expect(result.current.draft).toBeNull()
    })

    it('stays open after a failed write that a closing dialog tried to dismiss', async () => {
      const { result, answer } = slowSetup()
      act(() => result.current.addTile('embed'))
      const { saveTile, cancelTile } = result.current
      let outcome!: Promise<unknown>
      act(() => { outcome = saveTile('embed', { title: 't', url: 'u' }).catch((e: unknown) => e); cancelTile() })
      await answer(null)
      expect(await outcome).toBeInstanceOf(Error)
      expect(result.current.draftOfKind('embed')).not.toBeNull()
    })
  })
})

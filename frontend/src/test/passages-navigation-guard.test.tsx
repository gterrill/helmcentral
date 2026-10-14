import { fireEvent, render, screen } from '@testing-library/react'
import { act } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { CruiseEditPanel } from '@/components/passages/cruise-edit-panel'
import { PassageEditPanel } from '@/components/passages/passage-edit-panel'
import type { PassageDetail } from '@/lib/passages'
import { testRouter } from './helpers/router'

const api = vi.hoisted(() => ({ patch: vi.fn(), patchCruise: vi.fn() }))
vi.mock('@/lib/passages-api', () => ({ passagesApi: api }))
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

const passage: PassageDetail = {
  id: 'p1', start: '2026-03-13T23:30:00Z', end: '2026-03-14T02:00:00Z', from_name: 'Airlie', to_name: 'Hamilton',
  title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '', start_lon: 149,
  stats: {
    distance_nm: 1, underway_seconds: 60, mean_sog_kts: null, max_sog_kts: null, fuel_burned_l: null, tws_mean_kts: null,
    max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
  },
  track: [], track_times: [], replay_track: [], replay_times: [],
}

const passagePanel = (
  <PassageEditPanel
    passage={passage} cruises={[]} olderNeighbour={null} newerNeighbour={null} cursorRef={{ current: null }}
    canWrite onMerged={vi.fn()} onHidden={vi.fn()}
  />
)

beforeEach(() => { vi.clearAllMocks() })

describe('passage details navigation guard', () => {
  it('holds nothing until a field is edited', () => {
    const { wrapper, router } = testRouter('/passages/p1')
    render(passagePanel, { wrapper })
    expect(router.hasGuards()).toBe(false)
  })

  it('holds a move away while edits are unsaved, but not a move to the same passage', () => {
    const { wrapper, router } = testRouter('/passages/p1')
    render(passagePanel, { wrapper })
    fireEvent.change(screen.getByLabelText('Notes'), { target: { value: 'Rough crossing' } })
    expect(router.getSnapshot().guardOwners).toEqual(['passage-details'])
    expect(router.navigate({ panel: 'passages', route: { view: 'passage', id: 'p1', filter: { kind: 'all' } } })).toBe(true)
    expect(router.navigate({ panel: 'passages', route: { view: 'index', filter: { kind: 'all' } } })).toBe(false)
    expect(router.getSnapshot().pending?.label).toBe('Details')
  })

  it('Save and Continue saves the edits through the guard', async () => {
    api.patch.mockResolvedValue({})
    const { wrapper, router } = testRouter('/passages/p1')
    render(passagePanel, { wrapper })
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Delivery' } })
    router.navigate({ panel: 'radar' })
    const pending = router.getSnapshot().pending
    expect(pending?.save).not.toBeNull()
    await act(async () => { await pending!.save!() })
    expect(api.patch).toHaveBeenCalledWith('p1', { title: 'Delivery', from_name: 'Airlie', to_name: 'Hamilton', notes: '' })
  })
})

describe('cruise details navigation guard', () => {
  const cruise = { id: 'c1', name: 'Whitsundays', notes: '', created_at: '' }
  const panel = <CruiseEditPanel cruise={cruise} canWrite onDeleted={vi.fn()} />

  it('holds a move away while the name or notes are unsaved and saves on request', async () => {
    api.patchCruise.mockResolvedValue({})
    const { wrapper, router } = testRouter('/passages/cruises/c1')
    render(panel, { wrapper })
    expect(router.hasGuards()).toBe(false)
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Whitsundays 2026' } })
    expect(router.navigate({ panel: 'passages', route: { view: 'passage', id: 'p1', filter: { kind: 'cruise', id: 'c1' } } })).toBe(false)
    await act(async () => { await router.getSnapshot().pending!.save!() })
    expect(api.patchCruise).toHaveBeenCalledWith('c1', { name: 'Whitsundays 2026', notes: '' })
  })
})

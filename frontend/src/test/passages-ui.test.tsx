import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { PassageEditPanel } from '@/components/passages/passage-edit-panel'
import { CruiseStatsPanel, WaterRiseRow } from '@/components/passages/passage-stats'
import { PassagesPanel } from '@/components/passages/passages-panel'
import type { CruiseStats, PassageDetail, PassageSummary } from '@/lib/passages'

const api = vi.hoisted(() => ({
  split: vi.fn(),
  trim: vi.fn(),
  patch: vi.fn(),
  merge: vi.fn(),
  createCruise: vi.fn(),
  importGPX: vi.fn(),
}))
vi.mock('@/lib/passages-api', () => ({ passagesApi: api }))

const toastError = vi.hoisted(() => vi.fn())
vi.mock('sonner', () => ({ toast: { error: toastError, success: vi.fn() } }))

const stats = {
  distance_nm: 12.3,
  underway_seconds: 7500,
  mean_sog_kts: null,
  max_sog_kts: null,
  fuel_burned_l: null,
  tws_mean_kts: null,
  max_gust_kts: null,
  soc_start_pct: null,
  soc_end_pct: null,
  soc_min_pct: null,
}

function passage(id: string, over: Partial<PassageSummary> = {}): PassageSummary {
  return {
    id, start: '2026-03-13T23:30:00Z', end: '2026-03-14T02:00:00Z', from_name: 'Airlie', to_name: 'Hamilton',
    title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '2026-03-14T03:00:00Z',
    stats, start_lon: 149, ...over,
  }
}

const baseProps = {
  filter: { kind: 'all' } as const,
  cruises: [],
  loading: false,
  error: null,
  canWrite: true,
  onRetry: vi.fn(),
  onFilter: vi.fn(),
  onOpenPassage: vi.fn(),
  onOpenCruise: vi.fn(),
  onCombine: vi.fn(),
  onChanged: vi.fn(),
}

beforeEach(() => {
  vi.clearAllMocks()
  window.matchMedia = window.matchMedia ?? (((q: string) => ({ matches: false, media: q, addEventListener: vi.fn(), removeEventListener: vi.fn() })) as never)
})

describe('PassagesPanel', () => {
  it('says what to expect when nothing has been recorded', () => {
    render(<PassagesPanel {...baseProps} passages={[]} />)
    expect(screen.getByText('No passages yet')).toBeInTheDocument()
    expect(screen.getByText(/first one appears after you next get underway/)).toBeInTheDocument()
  })

  it('lists a passage with boat-local date, route, distance and time underway', () => {
    render(<PassagesPanel {...baseProps} passages={[passage('p1')]} />)
    expect(screen.getAllByText('14 Mar').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Airlie → Hamilton').length).toBeGreaterThan(0)
    expect(screen.getAllByText('12.3 NM').length).toBeGreaterThan(0)
    expect(screen.getAllByText('2 h 05 m').length).toBeGreaterThan(0)
  })

  it('narrows the list to a cruise and to unfiled passages', () => {
    const passages = [passage('p1', { cruise_id: 'c1', from_name: 'Alpha' }), passage('p2', { from_name: 'Bravo' })]
    const cruises = [{ id: 'c1', name: 'Whitsundays', notes: '', created_at: '' }]
    const { rerender } = render(<PassagesPanel {...baseProps} passages={passages} cruises={cruises} filter={{ kind: 'cruise', id: 'c1' }} />)
    expect(screen.queryByText('Bravo → Hamilton')).not.toBeInTheDocument()
    expect(screen.getAllByText('Alpha → Hamilton').length).toBeGreaterThan(0)
    rerender(<PassagesPanel {...baseProps} passages={passages} cruises={cruises} filter={{ kind: 'unfiled' }} />)
    expect(screen.queryByText('Alpha → Hamilton')).not.toBeInTheDocument()
    expect(screen.getAllByText('Bravo → Hamilton').length).toBeGreaterThan(0)
  })

  it('views a selection together once two are chosen', () => {
    render(<PassagesPanel {...baseProps} passages={[passage('p1'), passage('p2', { start: '2026-03-10T00:00:00Z' })]} />)
    const boxes = screen.getAllByRole('checkbox', { name: /Select Airlie/ })
    fireEvent.click(boxes[0])
    expect(screen.getByRole('button', { name: /View together/ })).toBeDisabled()
    fireEvent.click(screen.getAllByRole('checkbox', { name: /Select Airlie/ })[1])
    const together = screen.getByRole('button', { name: /View together/ })
    expect(together).toBeEnabled()
    fireEvent.click(together)
    expect(baseProps.onCombine).toHaveBeenCalledWith(expect.arrayContaining(['p1', 'p2']))
  })

  it('shows the import summary after an upload', async () => {
    api.importGPX.mockResolvedValue({
      files: [{ name: 'a.gpx', points: 10 }], points_read: 10, points_added: 10, points_rejected: 0,
      points_already_imported: 0, points_in_recorded_log: 0, passages_created: 2, passages_skipped: 0,
    })
    render(<PassagesPanel {...baseProps} passages={[passage('p1')]} />)
    const input = screen.getByTestId('passages-file-input')
    fireEvent.change(input, { target: { files: [new File(['<gpx/>'], 'a.gpx')] } })
    expect(await screen.findByTestId('import-summary')).toHaveTextContent('2 new passages found in 1 file.')
    expect(baseProps.onChanged).toHaveBeenCalled()
  })
})

describe('CruiseStatsPanel', () => {
  const empty: CruiseStats = {
    passage_count: 1, departure: null, arrival: null, span_seconds: 0, underway_seconds: 0, nights: 0, distance_nm: 0,
    fuel_burned_l: null, underway_tws_mean_kts: null, underway_max_gust_kts: null, anchor_tws_mean_kts: null,
    anchor_max_gust_kts: null, generator_hours: null, lowest_soc_pct: null, night_soc: [], water_used_l: null,
    water_taken_on_l: null, water_rises: [],
  }

  it('prints -- for every figure the log does not hold', () => {
    render(<CruiseStatsPanel stats={empty} units="metric" lon={149} canWrite onLabelRise={vi.fn()} />)
    const grid = screen.getByTestId('cruise-stats-grid')
    expect(grid).toHaveTextContent('Fuel burned--')
    expect(grid).toHaveTextContent('Generator--')
    expect(grid).toHaveTextContent('Lowest battery--')
    expect(grid).toHaveTextContent('Wind at anchor--Gust --')
    expect(grid).toHaveTextContent('Water used--')
  })

  it('lists dawn battery per night and lets the operator confirm a suggested water source', () => {
    const onLabelRise = vi.fn()
    const stats: CruiseStats = {
      ...empty,
      night_soc: [{ night: '2026-03-13', sunset: '', sunrise: '', min_soc_pct: 50, dawn_soc_pct: 62.4 }],
      water_rises: [{
        start: '2026-03-14T01:00:00Z', end: '2026-03-14T01:20:00Z', litres: 180, lat: 0, lon: 149,
        suggested_label: 'Dock fill', suggestion_status: 'found', looked_up_at: '2026-03-14T02:00:00Z', label: null,
      }],
    }
    const { rerender } = render(<CruiseStatsPanel stats={stats} units="metric" lon={149} canWrite onLabelRise={onLabelRise} />)
    expect(screen.getByTestId('night-soc')).toHaveTextContent('13 Mar62%')
    const row = screen.getByTestId('water-rise-row')
    expect(row).toHaveTextContent('Suggested: Dock fill')
    expect(row).toHaveTextContent('+180 L')
    rerender(<CruiseStatsPanel stats={stats} units="imperial" lon={149} canWrite onLabelRise={onLabelRise} />)
    expect(screen.getByTestId('water-rise-row')).toHaveTextContent('+48 gal')
    fireEvent.click(screen.getByRole('button', { name: 'Dock fill' }))
    expect(onLabelRise).toHaveBeenCalledWith(expect.objectContaining({ litres: 180 }), 'Dock fill')
  })
})

describe('PassageEditPanel', () => {
  const detail: PassageDetail = { ...passage('p1'), track: [], track_times: [], replay_track: [], replay_times: [] }
  const make = (cursor: { id: string; at: number } | null) => {
    const props = {
      passage: detail, cruises: [], olderNeighbour: null, newerNeighbour: null,
      cursorRef: { current: cursor }, canWrite: true,
      onChanged: vi.fn(), onMerged: vi.fn(), onHidden: vi.fn(),
    }
    render(<PassageEditPanel {...props} />)
    return props
  }

  it('splits at the replay time', async () => {
    api.split.mockResolvedValue([])
    const props = make({ id: 'p1', at: Date.UTC(2026, 2, 14, 0, 30) / 1000 })
    fireEvent.click(screen.getByRole('button', { name: /Split here/ }))
    await waitFor(() => expect(api.split).toHaveBeenCalledWith('p1', '2026-03-14T00:30:00.000Z'))
    await waitFor(() => expect(props.onChanged).toHaveBeenCalled())
  })

  it('asks for a replay position instead of guessing one', () => {
    make(null)
    fireEvent.click(screen.getByRole('button', { name: /Trim start to here/ }))
    expect(api.trim).not.toHaveBeenCalled()
    expect(toastError).toHaveBeenCalledWith('Could not trim passage', expect.objectContaining({ description: expect.stringContaining('replay') }))
  })

  it('saves edited names and notes', async () => {
    api.patch.mockResolvedValue({})
    make(null)
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Delivery' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.patch).toHaveBeenCalledWith('p1', { title: 'Delivery', from_name: 'Airlie', to_name: 'Hamilton', notes: '' }))
  })

  it('hides only after confirmation', async () => {
    api.patch.mockResolvedValue({})
    const props = make(null)
    fireEvent.click(screen.getByRole('button', { name: 'Hide passage' }))
    expect(api.patch).not.toHaveBeenCalled()
    const buttons = await screen.findAllByRole('button', { name: 'Hide passage' })
    fireEvent.click(buttons[buttons.length - 1])
    await waitFor(() => expect(api.patch).toHaveBeenCalledWith('p1', { hidden: true }))
    await waitFor(() => expect(props.onHidden).toHaveBeenCalled())
  })
})

describe('PassageEditPanel with unsaved edits', () => {
  const detail: PassageDetail = { ...passage('p1'), track: [], track_times: [], replay_track: [], replay_times: [] }
  const props = {
    passage: detail, cruises: [], olderNeighbour: passage('p0'), newerNeighbour: passage('p2'),
    cursorRef: { current: { id: 'p1', at: 1000 } }, canWrite: true,
    onChanged: vi.fn(), onMerged: vi.fn(), onHidden: vi.fn(),
  }

  it('keeps typed notes when the server record changes underneath', () => {
    const { rerender } = render(<PassageEditPanel {...props} />)
    fireEvent.change(screen.getByLabelText('Notes'), { target: { value: 'Rough crossing' } })
    rerender(<PassageEditPanel {...props} passage={{ ...detail, to_name: '' }} />)
    expect(screen.getByLabelText('Notes')).toHaveValue('Rough crossing')
    expect(screen.getByLabelText('To')).toHaveValue('')
  })

  it('takes the new server values when nothing has been edited', () => {
    const { rerender } = render(<PassageEditPanel {...props} />)
    rerender(<PassageEditPanel {...props} passage={{ ...detail, to_name: 'Mackay' }} />)
    expect(screen.getByLabelText('To')).toHaveValue('Mackay')
  })

  it('refuses split, trim and merge until the edits are saved, and says why', () => {
    render(<PassageEditPanel {...props} />)
    fireEvent.change(screen.getByLabelText('Notes'), { target: { value: 'x' } })
    for (const name of [/Split here/, /Trim start/, /Trim end/, /Merge with previous/, /Merge with next/]) {
      expect(screen.getByRole('button', { name })).toBeDisabled()
    }
    expect(screen.getByText('Save your changes before splitting, trimming or merging.')).toBeInTheDocument()
  })
})

describe('water rise suggestion status', () => {
  const rise = (suggestion_status: 'found' | 'none' | 'failed' | 'pending', suggested_label: string | null = null) => ({
    start: '2026-03-14T01:00:00Z', end: '2026-03-14T01:20:00Z', litres: 100, lat: 0, lon: 149,
    suggested_label, suggestion_status, looked_up_at: null, label: null,
  })
  const row = (r: ReturnType<typeof rise>) => {
    render(<WaterRiseRow rise={r} lon={149} units="metric" canWrite onLabel={vi.fn()} />)
    return screen.getByTestId('water-rise-row')
  }

  it('says the check failed and offers no suggestion', () => {
    const el = row(rise('failed', 'Dock fill'))
    expect(el).toHaveTextContent("Couldn't check for a marina")
    expect(el).not.toHaveTextContent('Suggested')
  })

  it('says nothing about a check that is pending or found nothing', () => {
    for (const status of ['pending', 'none'] as const) {
      const { unmount } = render(<WaterRiseRow rise={rise(status)} lon={149} units="metric" canWrite onLabel={vi.fn()} />)
      const el = screen.getByTestId('water-rise-row')
      expect(el).not.toHaveTextContent('Suggested')
      expect(el).not.toHaveTextContent("Couldn't")
      unmount()
    }
  })
})

describe('a passage with no names', () => {
  it('reads as a dated passage in the list, not as dashes', () => {
    render(<PassagesPanel {...baseProps} passages={[passage('p1', { from_name: '', to_name: '' })]} />)
    expect(screen.getAllByText('Passage · 14 Mar').length).toBeGreaterThan(0)
    expect(screen.queryByText('-- → --')).not.toBeInTheDocument()
  })
})

describe('a titled passage that just repeats its route', () => {
  it('does not print the route twice, whatever the arrow style', () => {
    render(<PassagesPanel {...baseProps} passages={[passage('p1', { title: 'airlie  ->  hamilton' })]} />)
    expect(screen.queryByText('Airlie → Hamilton')).not.toBeInTheDocument()
    expect(screen.getAllByText('airlie -> hamilton').length).toBeGreaterThan(0)
  })

  it('keeps the route line under a title that says something else', () => {
    render(<PassagesPanel {...baseProps} passages={[passage('p1', { title: 'Delivery leg' })]} />)
    expect(screen.getAllByText('Airlie → Hamilton').length).toBeGreaterThan(0)
  })
})

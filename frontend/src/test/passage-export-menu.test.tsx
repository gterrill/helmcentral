import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { PassageExportMenu } from '@/components/passages/passage-export-menu'
import type { PassageDetail, PassageStats } from '@/lib/passages'

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), loading: vi.fn(), dismiss: vi.fn() }))
vi.mock('sonner', () => ({ toast }))

const STATS: PassageStats = {
  distance_nm: 10, underway_seconds: 3600, mean_sog_kts: 5, max_sog_kts: 6, fuel_burned_l: null,
  tws_mean_kts: null, max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
}

function passage(track: [number, number][][]): PassageDetail {
  const t0 = Date.UTC(2026, 8, 14) / 1000
  return {
    id: 'p', start: new Date(t0 * 1000).toISOString(), end: new Date((t0 + 600) * 1000).toISOString(),
    from_name: 'A', to_name: 'B', title: 'Out and back', notes: '', hidden: false, edited: false, cruise_id: null,
    created_at: '', stats: STATS, start_lon: 148,
    track, track_times: track.map((s) => s.map((_, i) => t0 + i * 60)), replay_track: [], replay_times: [],
  }
}

const TRACK: [number, number][][] = [[[-20, 148], [-20.01, 148.01], [-20.02, 148.02]]]

function openMenu() {
  const trigger = screen.getByRole('button', { name: 'Export' })
  fireEvent.pointerDown(trigger)
  fireEvent.mouseDown(trigger)
  fireEvent.click(trigger)
}

beforeEach(() => { vi.clearAllMocks() })

describe('PassageExportMenu', () => {
  it('copies the blog embed and confirms', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
    await vi.waitFor(() => expect(toast.success).toHaveBeenCalled())
    expect(writeText).toHaveBeenCalledTimes(1)
    expect(writeText.mock.calls[0][0]).toContain('cdn.jsdelivr.net/npm/maplibre-gl@5.24.0')
    expect(toast.success).toHaveBeenCalledWith('Blog embed copied', expect.anything())
  })

  it('says what to do instead when the clipboard is withheld, as on plain http', async () => {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
    await vi.waitFor(() => expect(toast.error).toHaveBeenCalled())
    expect(toast.success).not.toHaveBeenCalled()
    expect(toast.error.mock.calls[0][1].description).toContain('Download embed as HTML')
  })

  it('offers the play lengths, both images and the HTML download', async () => {
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    for (const name of ['15 seconds', '30 seconds', '60 seconds']) {
      expect(await screen.findByRole('menuitemradio', { name })).toBeTruthy()
    }
    expect(screen.getByRole('menuitem', { name: 'Download embed as HTML' })).toBeTruthy()
    expect(screen.getByRole('menuitem', { name: 'Download image 1200 × 630' })).toBeTruthy()
    expect(screen.getByRole('menuitem', { name: 'Download image 2:1' })).toBeTruthy()
    expect(screen.getByRole('menuitemradio', { name: '30 seconds' }).getAttribute('aria-checked')).toBe('true')
  })

  it('is disabled when no track was recorded', () => {
    render(<PassageExportMenu kind="passage" title="Empty" passages={[passage([])]} units="metric" />)
    expect((screen.getByRole('button', { name: 'Export' }) as HTMLButtonElement).disabled).toBe(true)
  })
})

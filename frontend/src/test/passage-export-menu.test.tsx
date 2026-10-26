import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { saveInsetPreference } from '@/lib/passage-inset-preference'
import { saveImageryPreference } from '@/lib/passage-imagery-preference'
import { PassageExportMenu } from '@/components/passages/passage-export-menu'
import type { PassageDetail, PassageStats } from '@/lib/passages'

const render_ = vi.hoisted(() => ({ renderPassageImage: vi.fn<(data: unknown, kind: unknown, satellite: boolean) => Promise<Blob>>(async () => new Blob(['x'], { type: 'image/png' })) }))
vi.mock('@/lib/passage-export-image', () => render_)
vi.mock('@/lib/save-blob', () => ({ saveBlob: vi.fn() }))
const shotsHook = vi.hoisted(() => ({ value: { shots: [] as unknown[], loading: false, error: null as string | null } }))
vi.mock('@/hooks/use-camera-shots', () => ({ useCameraShots: () => shotsHook.value }))
const insetArgs = vi.hoisted(() => ({ calls: [] as unknown[] }))
const insetHook = vi.hoisted(() => ({ value: { view: { corner: 'top-right', zoomOffset: 0 } as { corner: string; zoomOffset: number } | null, loading: false, error: null as string | null } }))
vi.mock('@/hooks/use-inset-view', () => ({ useInsetView: (range: unknown) => { insetArgs.calls.push(range); return insetHook.value } }))
vi.mock('@/hooks/use-boat-hull', () => ({ useBoatHull: () => ({ type: 'sail_mono', loaM: 12, beamM: 4 }) }))
const signsHook = vi.hoisted(() => ({ value: { signs: [] as unknown[], loading: false, error: null as string | null } }))
vi.mock('@/hooks/use-replay-signs', () => ({ useReplaySigns: () => signsHook.value }))
const photos = vi.hoisted(() => ({ prepare: vi.fn<(signs: { id: string }[], o?: { onProgress?: (d: number, t: number) => void }) => Promise<Map<string, string>>>() }))
vi.mock('@/lib/passage-export-photos', () => ({ prepareSignPhotos: photos.prepare }))
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

beforeEach(() => {
  vi.clearAllMocks(); saveImageryPreference(true); localStorage.clear()
  shotsHook.value = { shots: [], loading: false, error: null }
  insetHook.value = { view: { corner: 'top-right', zoomOffset: 0 }, loading: false, error: null }
  signsHook.value = { signs: [], loading: false, error: null }
  photos.prepare.mockImplementation(async (signs) => new Map(signs.map((x) => [x.id, 'data:image/jpeg;base64,QUJD'])))
})

const T0S = Date.UTC(2026, 8, 14) / 1000
const aSign = (id: string, at: number, over: Record<string, unknown> = {}) => ({
  id, t_ms: (T0S + at) * 1000, title: `Sign ${id}`, note: '', document_id: null, photo_url: null, visibility: 'passing', show_s: 4, ...over,
})
const SIGNS = [aSign('a', 100), aSign('b', 200, { title: 'Whale', photo_url: '/api/documents/b/content', document_id: 'b' }), aSign('far', 99999)]

describe('PassageExportMenu', () => {
  it('opens the blog embed in a dialog, selected and ready to copy', async () => {
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
    const box = (await screen.findByRole('textbox', { name: 'Blog embed' })) as HTMLTextAreaElement
    expect(box.value).toContain('cdn.jsdelivr.net/npm/maplibre-gl@5.24.0')
    expect(box.selectionEnd - box.selectionStart).toBe(box.value.length)
    expect(toast.error).not.toHaveBeenCalled()
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

  it('draws the image with the passage map satellite choice, on unless turned off', async () => {
    localStorage.clear()
    const { unmount } = render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Download image 2:1' }))
    await vi.waitFor(() => expect(render_.renderPassageImage).toHaveBeenCalledTimes(1))
    expect(render_.renderPassageImage.mock.calls[0][2]).toBe(true)
    unmount()

    localStorage.setItem('passages.satelliteImagery', '0')
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Download image 2:1' }))
    await vi.waitFor(() => expect(render_.renderPassageImage).toHaveBeenCalledTimes(2))
    expect(render_.renderPassageImage.mock.calls[1][2]).toBe(false)
  })
})

describe('PassageExportMenu, cinematic card', () => {
  const embedText = async () => ((await screen.findByRole('textbox', { name: 'Blog embed' })) as HTMLTextAreaElement).value
  const dataOf = (html: string) => JSON.parse(html.match(/<script type="application\/json"[^>]*>([\s\S]*?)<\/script>/)?.[1] ?? '{}') as {
    d3: boolean; hull: unknown; shots: unknown[]
  }

  it('offers a 3D switch that follows the passage map, on unless it was turned off there', async () => {
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    expect((await screen.findByRole('menuitemcheckbox', { name: '3D' })).getAttribute('aria-checked')).toBe('true')
  })

  it('starts with 3D off when the passage map has it off', async () => {
    localStorage.setItem('passages.replay3d', '0')
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    expect((await screen.findByRole('menuitemcheckbox', { name: '3D' })).getAttribute('aria-checked')).toBe('false')
  })

  it('puts the 3D choice, the hull and the saved shots into the embed', async () => {
    const t0 = Date.UTC(2026, 8, 14) / 1000
    shotsHook.value = {
      shots: [{ id: 's', t_ms: (t0 + 100) * 1000, mode: 'orbit', zoom: 14, pitch: 55, bearing_offset_deg: 0, orbit_deg_per_s: 6, pace: 0.5 }],
      loading: false, error: null,
    }
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
    const data = dataOf(await embedText())
    expect(data.d3).toBe(true)
    expect(data.hull).toEqual({ type: 'sail_mono', loa: 12, beam: 4 })
    expect(data.shots).toHaveLength(1)
  })

  it('puts the saved overview corner and zoom step into the embed', async () => {
    const embedFor = async (view: { corner: string; zoomOffset: number }) => {
      insetHook.value = { view, loading: false, error: null }
      const { unmount } = render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
      const data = JSON.parse((await embedText()).match(/<script type="application\/json"[^>]*>([\s\S]*?)<\/script>/)?.[1] ?? '{}') as { insetCorner: string; insetZoom: number }
      unmount()
      return data
    }
    const auto = await embedFor({ corner: 'top-right', zoomOffset: 0 })
    const moved = await embedFor({ corner: 'bottom-left', zoomOffset: -2 })
    expect(auto.insetCorner).toBe('top-right')
    expect(moved.insetCorner).toBe('bottom-left')
    expect(moved.insetZoom).toBeCloseTo(auto.insetZoom - 2, 2)
  })

  it('reads the placement for the range of every passage shown, track or not', async () => {
    const t0 = Date.UTC(2026, 8, 14)
    const noTrack = { ...passage([]), id: 'q', start: new Date(t0 - 3600_000).toISOString(), end: new Date(t0 - 1800_000).toISOString() }
    const last = { ...passage(TRACK), id: 'r', start: new Date(t0 + 86400_000).toISOString(), end: new Date(t0 + 86400_000 + 600_000).toISOString() }
    insetArgs.calls = []
    render(<PassageExportMenu kind="combined" title="Both" passages={[passage(TRACK), noTrack, last]} units="metric" />)
    expect(insetArgs.calls.at(-1)).toEqual({ from: t0 - 3600_000, to: t0 + 86400_000 + 600_000 })
  })

  it('stops an image, saying why, when the overview placement could not be read', async () => {
    saveInsetPreference(true)
    insetHook.value = { view: null, loading: false, error: 'HTTP 500' }
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Download image 1200 × 630' }))
    await vi.waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not read where the overview map goes', { description: 'HTTP 500' }))
    expect(render_.renderPassageImage).not.toHaveBeenCalled()
  })

  it('waits for the saved overview placement before it exports', async () => {
    saveInsetPreference(true)
    insetHook.value = { view: null, loading: true, error: null }
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    expect(await screen.findByRole('menuitem', { name: 'Copy blog embed' })).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByRole('menuitem', { name: 'Download image 1200 × 630' })).toHaveAttribute('aria-disabled', 'true')
  })

  it('stops, saying why, when the overview placement could not be read', async () => {
    saveInsetPreference(true)
    insetHook.value = { view: null, loading: false, error: 'HTTP 500' }
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
    expect(toast.error).toHaveBeenCalledWith('Could not read where the overview map goes', { description: 'HTTP 500' })
    expect(screen.queryByRole('textbox', { name: 'Blog embed' })).toBeNull()
  })

  it('does not wait for or stop on the overview placement while the overview map is off', async () => {
    saveInsetPreference(false)
    insetHook.value = { view: null, loading: true, error: null }
    const { unmount } = render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    expect(await screen.findByRole('menuitem', { name: 'Copy blog embed' })).not.toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByRole('menuitem', { name: 'Download image 1200 × 630' })).not.toHaveAttribute('aria-disabled', 'true')
    unmount()

    insetHook.value = { view: null, loading: false, error: 'HTTP 500' }
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
    expect(await screen.findByRole('textbox', { name: 'Blog embed' })).toBeTruthy()
    expect(toast.error).not.toHaveBeenCalled()
  })

  it('leaves the model and the tilt out when 3D is switched off', async () => {
    localStorage.setItem('passages.replay3d', '0')
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
    const data = dataOf(await embedText())
    expect(data.d3).toBe(false)
    expect(data.hull).toBeNull()
  })

  it('stops, saying why, when the camera shots could not be read', async () => {
    shotsHook.value = { shots: [], loading: false, error: 'HTTP 500' }
    render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
    openMenu()
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
    expect(toast.error).toHaveBeenCalledWith('Could not read the camera shots', { description: 'HTTP 500' })
    expect(screen.queryByRole('textbox', { name: 'Blog embed' })).toBeNull()
  })

  describe('signs', () => {
    beforeEach(() => { signsHook.value = { signs: SIGNS, loading: false, error: null } })
    const embedText = async () => ((await screen.findByRole('textbox', { name: 'Blog embed' })) as HTMLTextAreaElement).value

    it('offers Include signs, on, with the count for the exported range', async () => {
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      expect((await screen.findByRole('menuitemcheckbox', { name: 'Include signs' })).getAttribute('aria-checked')).toBe('true')
      expect(screen.getByTestId('sign-summary').textContent).toContain('2 signs, 1 photo')
    })

    it('says so when the range has no signs', async () => {
      signsHook.value = { signs: [], loading: false, error: null }
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      await screen.findByRole('menuitemcheckbox', { name: 'Include signs' })
      expect(screen.getByTestId('sign-summary').textContent).toContain('No signs in this range')
    })

    it('puts the signs and their photos in the blog embed', async () => {
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
      const text = await embedText()
      expect(text).toContain('Whale')
      expect(text).toContain('data:image/jpeg;base64,QUJD')
      expect(text).not.toContain('far')
      expect(toast.error).not.toHaveBeenCalled()
    })

    it('leaves them out when Include signs is off, and fetches no photo', async () => {
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      fireEvent.click(await screen.findByRole('menuitemcheckbox', { name: 'Include signs' }))
      photos.prepare.mockClear()
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
      const text = await embedText()
      expect(text).not.toContain('Whale')
      expect(text).not.toContain('QUJD')
      expect(photos.prepare).not.toHaveBeenCalled()
    })

    it('stops and names the sign when its photo cannot be prepared', async () => {
      photos.prepare.mockRejectedValue(new Error('The photo for the sign "Whale" could not be read (HTTP 404)'))
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
      await vi.waitFor(() => expect(toast.error).toHaveBeenCalled())
      expect(toast.error.mock.calls[0][1]).toEqual({ description: expect.stringContaining('"Whale"') })
      expect(screen.queryByRole('textbox', { name: 'Blog embed' })).toBeNull()
    })

    it('stops over 3 MB with the plain message and writes no file', async () => {
      photos.prepare.mockImplementation(async (signs) => new Map(signs.map((x) => [x.id, `data:image/jpeg;base64,${'A'.repeat(3_500_000)}`])))
      const { saveBlob } = await import('@/lib/save-blob')
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Download embed as HTML' }))
      await vi.waitFor(() => expect(toast.error).toHaveBeenCalled())
      const description = (toast.error.mock.calls[0][1] as { description: string }).description
      expect(description).toMatch(/over the 3 MB limit/)
      expect(description).toContain('"Whale"')
      expect(description).toContain('Include signs')
      expect(saveBlob).not.toHaveBeenCalled()
      openMenu()
      await vi.waitFor(() => expect(screen.getByTestId('card-size').textContent).toMatch(/over the 3 MB limit/))
    })

    it('stops when the signs could not be read, unless they are off', async () => {
      signsHook.value = { signs: [], loading: false, error: 'HTTP 500' }
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
      await vi.waitFor(() => expect(toast.error).toHaveBeenCalled())
      expect(toast.error.mock.calls[0][0]).toBe('Could not read the signs')
    })

    it('shows the card size once the photos are ready', async () => {
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      await vi.waitFor(() => expect(screen.getByTestId('card-size').textContent).toMatch(/^Card \d+ (KB|MB)$|^Card \d+\.\d MB$/))
    })

    const photoSign = (id: string, at: number) => aSign(id, at, { title: `Photo ${id}`, photo_url: `/api/documents/${id}/content`, document_id: id })
    const uriOf = (id: string) => `data:image/jpeg;base64,QUJD${id.toUpperCase()}`
    const preparedFor = async (signs: { id: string }[]) => new Map(signs.map((x) => [x.id, uriOf(x.id)]))

    it('waits for the run that matches the signs now, when the set changed mid-run', async () => {
      let release: (m: Map<string, string>) => void = () => {}
      photos.prepare.mockImplementationOnce(() => new Promise((resolve) => { release = resolve }))
      photos.prepare.mockImplementation(preparedFor)
      signsHook.value = { signs: [photoSign('b', 200)], loading: false, error: null }
      const { rerender } = render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      await vi.waitFor(() => expect(photos.prepare).toHaveBeenCalledTimes(1))
      signsHook.value = { signs: [photoSign('b', 200), photoSign('c', 300)], loading: false, error: null }
      rerender(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      await vi.waitFor(() => expect(photos.prepare).toHaveBeenCalledTimes(2))
      // The first run finishes late, with only the first set.
      release(new Map([['b', uriOf('b')]]))
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
      const text = await embedText()
      expect(text).toContain(uriOf('b'))
      expect(text).toContain(uriOf('c'))
      expect(toast.error).not.toHaveBeenCalled()
    })

    it('does not prepare the photos again once they are ready', async () => {
      photos.prepare.mockImplementation(preparedFor)
      signsHook.value = { signs: [photoSign('b', 200)], loading: false, error: null }
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      await vi.waitFor(() => expect(screen.getByTestId('card-size').textContent).not.toContain('about'))
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Copy blog embed' }))
      await embedText()
      fireEvent.click(screen.getAllByRole('button', { name: 'Close' }).find((b) => b.textContent === 'Close') as HTMLElement)
      openMenu()
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Download embed as HTML' }))
      await vi.waitFor(() => expect(toast.error).not.toHaveBeenCalled())
      expect(photos.prepare).toHaveBeenCalledTimes(1)
    })

    it('warns before anything is prepared that the card will probably be too big', async () => {
      photos.prepare.mockImplementation(() => new Promise(() => {}))
      signsHook.value = { signs: Array.from({ length: 12 }, (_, i) => photoSign(`p${i}`, 100 + i)), loading: false, error: null }
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      await vi.waitFor(() => expect(screen.getByTestId('card-size').textContent).toMatch(/^Card about \d\.\d MB, over the 3 MB limit$/))
    })

    it('says "about" until every photo is ready, then the exact size', async () => {
      let release: (m: Map<string, string>) => void = () => {}
      photos.prepare.mockImplementation(() => new Promise((resolve) => { release = resolve }))
      signsHook.value = { signs: [photoSign('b', 200)], loading: false, error: null }
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      await vi.waitFor(() => expect(screen.getByTestId('card-size').textContent).toMatch(/^Card about /))
      release(new Map([['b', uriOf('b')]]))
      await vi.waitFor(() => expect(screen.getByTestId('card-size').textContent).not.toContain('about'))
    })

    it('holds every export while signs load, even with Include signs off', async () => {
      signsHook.value = { signs: [], loading: true, error: null }
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      fireEvent.click(await screen.findByRole('menuitemcheckbox', { name: 'Include signs' }))
      for (const name of ['Copy blog embed', 'Download embed as HTML']) {
        expect(screen.getByRole('menuitem', { name }).hasAttribute('data-disabled')).toBe(true)
      }
      for (const item of screen.getAllByRole('menuitem').filter((i) => /^Download image/.test(i.textContent ?? ''))) {
        expect(item.hasAttribute('data-disabled')).toBe(true)
      }
    })

    it('refuses an export when signs failed to load, even with Include signs off', async () => {
      signsHook.value = { signs: [], loading: false, error: 'HTTP 500' }
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      fireEvent.click(await screen.findByRole('menuitemcheckbox', { name: 'Include signs' }))
      fireEvent.click(screen.getByRole('menuitem', { name: 'Copy blog embed' }))
      await vi.waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not read the signs', expect.anything()))
    })

    it('holds Copy and Download while signs load, when they are included', async () => {
      signsHook.value = { signs: [], loading: true, error: null }
      render(<PassageExportMenu kind="passage" title="Out and back" passages={[passage(TRACK)]} units="metric" />)
      openMenu()
      expect((await screen.findByRole('menuitem', { name: 'Copy blog embed' })).hasAttribute('data-disabled')).toBe(true)
      expect(screen.queryByTestId('card-size')).toBeNull()
    })
  })
})

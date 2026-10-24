import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { forwardRef, useImperativeHandle } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ReplayPanel } from '@/components/passages/replay-panel'
import type { PassageDetail } from '@/lib/passages'
import { resetQueryStore } from '@/lib/api-query'
import { exifJpeg } from './helpers/exif-jpeg'
import { jsonResponse, stubApi, type StubRequest } from './helpers/stub-api'

vi.mock('@/hooks/use-boat-hull', () => ({ useBoatHull: () => null }))
vi.mock('react-map-gl/maplibre', () => ({
  Marker: ({ children, longitude, latitude }: { children: ReactNode; longitude: number; latitude: number }) => (
    <div data-testid="marker" data-lon={longitude} data-lat={latitude}>{children}</div>
  ),
}))
vi.mock('@/components/passages/passage-map', () => ({
  passageColour: () => 'x',
  PassageMap: forwardRef(function PassageMapMock(p: { markers?: ReactNode }, ref) {
    useImperativeHandle(ref, () => ({ flyIn: () => Promise.resolve(), getView: () => ({ zoom: 13, pitch: 40, bearing: 0 }) }))
    return <div data-testid="map">{p.markers}</div>
  }),
  InsetMap: () => <div />,
}))

const start = Date.UTC(2026, 9, 21, 23, 0) / 1000 // 22 Oct 09:00 UTC+10
const passage: PassageDetail = {
  id: 'p1', start: new Date(start * 1000).toISOString(), end: new Date((start + 1000) * 1000).toISOString(),
  from_name: '', to_name: '', title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '',
  stats: {
    distance_nm: 0, underway_seconds: 0, mean_sog_kts: null, max_sog_kts: null, fuel_burned_l: null, tws_mean_kts: null,
    max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
  },
  start_lon: 149,
  track: [[[0, 149], [0, 150]]],
  track_times: [[start, start + 1000]],
  replay_track: [[[0, 149], [0, 149.5], [0, 150]]],
  replay_times: [[start, start + 500, start + 1000]],
}

const sign = (id: string, offset: number, extra: Record<string, unknown> = {}) => ({
  id, t_ms: (start + offset) * 1000, title: `Sign ${id}`, note: `Note ${id}`, document_id: null, photo_url: null, visibility: 'passing', show_s: 4, ...extra,
})

let signs: ReturnType<typeof sign>[] = []
let writes: { method: string; url: string; body: unknown }[] = []

function stub(extra: Record<string, unknown> = {}) {
  stubApi({
    'GET /api/passages/camera-shots': { shots: [] },
    'GET /api/passages/signs': () => ({ signs }),
    'POST /api/passages/signs': (req: StubRequest) => {
      writes.push({ method: 'POST', url: req.url, body: req.body })
      const created = { ...(req.body as object), id: 'new', photo_url: (req.body as { document_id: string | null }).document_id ? '/api/documents/doc1/content' : null } as ReturnType<typeof sign>
      signs = [...signs, created]
      return { sign: created }
    },
    'PATCH /api/passages/signs/s1': (req: StubRequest) => {
      writes.push({ method: 'PATCH', url: req.url, body: req.body })
      signs = signs.map((s) => (s.id === 's1' ? { ...s, ...(req.body as object) } : s))
      return { sign: signs[0] }
    },
    'DELETE /api/passages/signs/s1': (req: StubRequest) => {
      writes.push({ method: 'DELETE', url: req.url, body: undefined })
      signs = signs.filter((s) => s.id !== 's1')
      return { deleted: true }
    },
    ...extra,
  })
}

const scrub = (clock: number) => fireEvent.change(screen.getByLabelText('Replay position'), { target: { value: String(clock) } })

beforeEach(() => {
  resetQueryStore()
  localStorage.clear()
  signs = []
  writes = []
  stub()
})

describe('ReplayPanel signs: on the chart', () => {
  it('shows every sign as a pin before the replay has moved, and a tap opens its card', async () => {
    signs = [sign('s1', 250), sign('s2', 600)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    const pins = await screen.findAllByTestId('sign-pin')
    expect(pins).toHaveLength(2)
    expect(screen.queryByTestId('sign-card')).toBeNull()
    fireEvent.click(pins[0])
    const card = screen.getByTestId('sign-card')
    expect(within(card).getByText('Sign s1')).toBeInTheDocument()
    expect(within(card).getByText('Note s1')).toBeInTheDocument()
    expect(screen.getAllByTestId('sign-pin')).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', { name: 'Close sign: Sign s1' }))
    expect(screen.queryByTestId('sign-card')).toBeNull()
    expect(screen.getAllByTestId('sign-pin')).toHaveLength(2)
  })

  it('an opened pin is a paused preview: pressing play closes it and only visible cards remain', async () => {
    signs = [sign('s1', 250), sign('s2', 600, { visibility: 'after' })]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    const pins = await screen.findAllByTestId('sign-pin')
    fireEvent.click(pins[0])
    expect(screen.getByRole('button', { name: 'Close sign: Sign s1' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Play replay' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Pause replay' })).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /^Close sign:/ })).toBeNull()
    expect(screen.queryAllByTestId('sign-card')).toHaveLength(0)
  })

  it('a pin pressed while playing leaves no card up', async () => {
    signs = [sign('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    await screen.findByTestId('sign-pin')
    fireEvent.click(screen.getByRole('button', { name: 'Signs' }))
    fireEvent.click(screen.getByRole('button', { name: 'Play replay' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Pause replay' })).toBeInTheDocument())
    fireEvent.click(screen.getByTestId('sign-pin'))
    expect(screen.queryByRole('button', { name: /^Close sign:/ })).toBeNull()
  })

  it('puts a sign on the track where the boat is at its time', async () => {
    signs = [sign('s1', 500)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    const pin = await screen.findByTestId('sign-pin')
    const marker = pin.closest('[data-testid="marker"]')!
    expect(Number(marker.getAttribute('data-lon'))).toBeCloseTo(149.5)
    expect(Number(marker.getAttribute('data-lat'))).toBeCloseTo(0)
  })

  it('while replaying, cards come and go with visibility and the pins go away', async () => {
    signs = [sign('s1', 250), sign('s2', 600, { visibility: 'after' }), sign('s3', 900, { visibility: 'always' })]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    await screen.findAllByTestId('sign-pin')
    scrub(100)
    const ids = () => screen.queryAllByTestId('sign-card').map((c) => c.getAttribute('data-sign-id'))
    expect(screen.queryAllByTestId('sign-pin')).toHaveLength(0)
    expect(ids()).toEqual(['s3'])
    scrub(250)
    expect(ids()).toEqual(['s3', 's1'])
    scrub(550)
    expect(ids()).toEqual(['s3'])
    scrub(650)
    // s3 always, plus the after sign; never more than two.
    expect(ids()).toEqual(['s3', 's2'])
    scrub(1000)
    expect(ids()).toEqual(['s3', 's2'])
  })

  it('opens the photo full size on a tap while paused; Escape closes it; not while playing', async () => {
    signs = [sign('s1', 250, { document_id: 'doc1', photo_url: '/api/documents/doc1/content' })]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    await screen.findByTestId('sign-pin')
    scrub(250)
    fireEvent.click(await screen.findByRole('button', { name: 'Open photo: Sign s1' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('img', { name: 'Sign s1' })).toHaveAttribute('src', '/api/documents/doc1/content')
    fireEvent.keyDown(dialog, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    // Playing: the card is still there but the tap does nothing.
    fireEvent.click(screen.getByRole('button', { name: 'Play replay' }))
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Open photo: Sign s1' })).toBeNull())
    expect(screen.getByTestId('sign-card')).toBeInTheDocument()
  })

  it('a sign whose photo is gone keeps its words and shows no image', async () => {
    signs = [sign('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    await screen.findByTestId('sign-pin')
    scrub(250)
    const card = await screen.findByTestId('sign-card')
    expect(within(card).getByText('Note s1')).toBeInTheDocument()
    expect(card.querySelector('img')).toBeNull()
    expect(screen.queryByRole('button', { name: /Open photo/ })).toBeNull()
  })

  it('a photo that will not load says so, with a muted icon, and is logged once', async () => {
    const log = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      signs = [sign('s1', 250, { document_id: 'doc1', photo_url: '/api/documents/doc1/content' })]
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      await screen.findByTestId('sign-pin')
      scrub(250)
      const card = await screen.findByTestId('sign-card')
      fireEvent.error(card.querySelector('img')!)
      expect(card.querySelector('img')).toBeNull()
      expect(within(card).getByText('Photo not available')).toBeInTheDocument()
      expect(within(card).getByTestId('sign-photo-missing')).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: /Open photo/ })).toBeNull()
      // Leaving and coming back mounts a new card; the console hears of it once.
      scrub(900)
      scrub(250)
      fireEvent.error((await screen.findByTestId('sign-card')).querySelector('img')!)
      expect(log.mock.calls.filter(([m]) => String(m).includes('did not load'))).toHaveLength(1)
    } finally {
      log.mockRestore()
    }
  })

  it('a deleted photo (the server sends none) shows no photo line at all', async () => {
    signs = [sign('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    await screen.findByTestId('sign-pin')
    scrub(250)
    const card = await screen.findByTestId('sign-card')
    expect(within(card).queryByText('Photo not available')).toBeNull()
  })

  it('under reduced motion a card is there at once, with no fade', async () => {
    vi.stubGlobal('matchMedia', (query: string) => ({ matches: query === '(prefers-reduced-motion: reduce)', addEventListener: () => {}, removeEventListener: () => {} }))
    try {
      signs = [sign('s1', 250)]
      render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
      await screen.findByTestId('sign-pin')
      scrub(250)
      const card = await screen.findByTestId('sign-card')
      expect(card).toHaveClass('opacity-100')
      expect(card.className).not.toMatch(/transition/)
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('leaves out a sign outside every replayed passage', async () => {
    signs = [sign('s1', 250), sign('gone', -86_400)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    await screen.findAllByTestId('sign-pin')
    expect(screen.getAllByTestId('sign-pin')).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', { name: 'Signs' }))
    expect(screen.getAllByRole('button', { name: /^Sign / }).filter((b) => b.getAttribute('aria-pressed') !== null)).toHaveLength(1)
  })
})

describe('ReplayPanel signs: the editor', () => {
  it('draws a diamond tick for each sign and a click seeks there and opens its fields', async () => {
    signs = [sign('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    const tick = await screen.findByRole('button', { name: /^Sign Sign s1 at 22 Oct 09:04/ })
    expect(tick).toHaveStyle({ left: '25%' })
    fireEvent.click(tick)
    expect((screen.getByLabelText('Replay position') as HTMLInputElement).value).toBe('250')
    const editor = screen.getByTestId('sign-editor')
    expect(within(editor).getByLabelText('Sign title')).toHaveValue('Sign s1')
    expect(within(editor).getByLabelText('Sign note')).toHaveValue('Note s1')
    expect(within(editor).getByRole('button', { name: 'While passing' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(editor).getByLabelText('Seconds shown')).toHaveValue(4)
    // Pins show in the editor even though the replay has moved.
    expect(screen.queryAllByTestId('sign-pin').length + screen.queryAllByTestId('sign-card').length).toBeGreaterThan(0)
  })

  it('lists the signs as chips and a chip seeks to its sign and selects it', async () => {
    signs = [sign('s2', 600), sign('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    const tick = await screen.findByRole('button', { name: /^Sign Sign s1 at/ })
    expect(tick).toBeInTheDocument()
    fireEvent.click(await screen.findByRole('button', { name: /^Signs$/ }))
    const list = await screen.findByRole('group', { name: 'Signs on this replay' })
    const chips = within(list).getAllByRole('button')
    expect(chips.map((c) => c.getAttribute('aria-label'))).toEqual([
      expect.stringMatching(/^Edit sign Sign s1 at/),
      expect.stringMatching(/^Edit sign Sign s2 at/),
    ])
    expect(chips[0]).toHaveAttribute('aria-pressed', 'false')
    fireEvent.click(chips[1])
    expect((screen.getByLabelText('Replay position') as HTMLInputElement).value).toBe('600')
    expect(screen.getByLabelText('Sign title')).toHaveValue('Sign s2')
    expect(within(screen.getByRole('group', { name: 'Signs on this replay' })).getAllByRole('button')[1]).toHaveAttribute('aria-pressed', 'true')
  })

  it('adds a note-only sign at the cursor', async () => {
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    await screen.findByRole('button', { name: 'Signs' })
    scrub(400)
    fireEvent.click(screen.getByRole('button', { name: 'Signs' }))
    fireEvent.click(screen.getByRole('button', { name: 'Add sign here' }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toEqual({ t_ms: (start + 400) * 1000, title: 'New sign', note: '', document_id: null, visibility: 'passing', show_s: 4 })
    expect(await screen.findByLabelText('Sign title')).toHaveValue('New sign')
  })

  it('edits title, note, visibility and seconds, each sent to the server, and deletes', async () => {
    signs = [sign('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: /^Sign Sign s1 at/ }))
    const title = screen.getByLabelText('Sign title')
    fireEvent.change(title, { target: { value: 'Humpback' } })
    fireEvent.blur(title)
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toEqual({ title: 'Humpback' })
    const note = screen.getByLabelText('Sign note')
    fireEvent.change(note, { target: { value: 'Breaching twice' } })
    fireEvent.blur(note)
    await waitFor(() => expect(writes).toHaveLength(2))
    expect(writes[1].body).toEqual({ note: 'Breaching twice' })
    const secs = screen.getByLabelText('Seconds shown')
    fireEvent.change(secs, { target: { value: '99' } })
    fireEvent.keyDown(secs, { key: 'Enter' })
    await waitFor(() => expect(writes).toHaveLength(3))
    expect(writes[2].body).toEqual({ show_s: 30 })
    fireEvent.click(screen.getByRole('button', { name: 'After passing' }))
    await waitFor(() => expect(writes).toHaveLength(4))
    expect(writes[3].body).toEqual({ visibility: 'after' })
    await waitFor(() => expect(screen.queryByLabelText('Seconds shown')).toBeNull())
    fireEvent.click(screen.getByRole('button', { name: 'Delete sign' }))
    await waitFor(() => expect(writes[4]?.method).toBe('DELETE'))
    await waitFor(() => expect(screen.queryByRole('button', { name: /^Sign Sign s1 at/ })).toBeNull())
  })

  it('an emptied title goes back rather than being sent', async () => {
    signs = [sign('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: /^Sign Sign s1 at/ }))
    const title = screen.getByLabelText('Sign title')
    fireEvent.change(title, { target: { value: '  ' } })
    fireEvent.blur(title)
    expect(writes).toHaveLength(0)
    expect(title).toHaveValue('Sign s1')
  })

  it('removes a photo with null', async () => {
    signs = [sign('s1', 250, { document_id: 'doc1', photo_url: '/api/documents/doc1/content' })]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: /^Sign Sign s1 at/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Photo' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Remove' }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toEqual({ document_id: null })
  })

  it('shows the server\'s reason when a sign is refused', async () => {
    stub({ 'POST /api/passages/signs': () => jsonResponse({ error: 'invalid passage request: t_ms is not inside any passage' }, 400) })
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Signs' }))
    fireEvent.click(screen.getByRole('button', { name: 'Add sign here' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('not inside any passage')
  })
})

describe('ReplayPanel signs: the photo controls', () => {
  it('offers one menu to add a photo sign, and one Photo control on a sign', async () => {
    signs = [sign('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: /^Sign Sign s1 at/ }))
    const editor = screen.getByTestId('sign-editor')
    expect(within(editor).getAllByRole('button').map((b) => b.getAttribute('aria-label') ?? b.textContent).filter((n) => /photo/i.test(n ?? ''))).toEqual(['Add photo sign', 'Photo'])
    fireEvent.click(within(editor).getByRole('button', { name: 'Add photo sign' }))
    expect(await screen.findByRole('menuitem', { name: 'Upload photo' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'From Documents' })).toBeInTheDocument()
  })

  it('on a sign without a photo the menu has no Remove; with one it shows the thumbnail', async () => {
    signs = [sign('s1', 250), sign('s2', 600, { document_id: 'doc1', photo_url: '/api/documents/doc1/content' })]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: /^Sign Sign s1 at/ }))
    expect(screen.queryByTestId('sign-editor-thumb')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Photo' }))
    expect(await screen.findByRole('menuitem', { name: 'Upload' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: 'Remove' })).toBeNull()
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    fireEvent.click(screen.getByRole('button', { name: /^Sign Sign s2 at/ }))
    expect(await screen.findByTestId('sign-editor-thumb')).toHaveAttribute('src', '/api/documents/doc1/content')
  })
})

describe('ReplayPanel signs: saving on blur', () => {
  it('editing the note then clicking Delete works with one click', async () => {
    signs = [sign('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: /^Sign Sign s1 at/ }))
    const note = screen.getByLabelText('Sign note')
    fireEvent.change(note, { target: { value: 'Changed' } })
    const del = screen.getByRole('button', { name: 'Delete sign' })
    // What a real click does: the field blurs first, then the click arrives.
    fireEvent.blur(note)
    expect(del).not.toBeDisabled()
    fireEvent.click(del)
    await waitFor(() => expect(writes.map((w) => w.method)).toEqual(['PATCH', 'DELETE']))
    expect(writes[0].body).toEqual({ note: 'Changed' })
  })

  it('editing the note then clicking Always works with one click', async () => {
    signs = [sign('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: /^Sign Sign s1 at/ }))
    const note = screen.getByLabelText('Sign note')
    fireEvent.change(note, { target: { value: 'Changed' } })
    fireEvent.blur(note)
    const always = screen.getByRole('button', { name: 'Always' })
    expect(always).not.toBeDisabled()
    fireEvent.click(always)
    await waitFor(() => expect(writes).toHaveLength(2))
    expect(writes.map((w) => w.body)).toEqual([{ note: 'Changed' }, { visibility: 'always' }])
  })

  it('keeps the typed text on show until the saved value comes back', async () => {
    signs = [sign('s1', 250)]
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: /^Sign Sign s1 at/ }))
    const note = screen.getByLabelText('Sign note')
    fireEvent.change(note, { target: { value: 'Changed' } })
    fireEvent.blur(note)
    expect(note).toHaveValue('Changed')
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(note).toHaveValue('Changed')
  })
})

describe('ReplayPanel signs: writes in order', () => {
  it('a slow photo upload and a title saved meanwhile run one after the other, and the failure is the upload\'s', async () => {
    signs = [sign('s1', 250)]
    let releaseUpload: (r: Response) => void = () => {}
    stub({
      'GET /api/document-folders/tree': { folders: [{ id: 'pp', name: 'Passage photos', parent_id: null }] },
      'POST /api/documents': () => new Promise<Response>((resolve) => { releaseUpload = resolve }),
    })
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: /^Sign Sign s1 at/ }))
    const editor = screen.getByTestId('sign-editor')
    fireEvent.change(screen.getByLabelText('Photo file for a new sign'), { target: { files: [new File([new Uint8Array([1])], 'a.jpg', { type: 'image/jpeg' })] } })
    await waitFor(() => expect(editor).toHaveAttribute('aria-busy', 'true'))
    const title = screen.getByLabelText('Sign title')
    fireEvent.change(title, { target: { value: 'Humpback' } })
    fireEvent.blur(title)
    // The title waits its turn behind the upload.
    await new Promise((r) => setTimeout(r, 50))
    expect(writes).toHaveLength(0)
    expect(editor).toHaveAttribute('aria-busy', 'true')
    releaseUpload(jsonResponse({ error: 'document too large' }, 413))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toEqual({ title: 'Humpback' })
    await waitFor(() => expect(editor).toHaveAttribute('aria-busy', 'false'))
    // The title saved fine and did not hide the upload's failure.
    expect(screen.getByRole('alert')).toHaveTextContent('document too large')
  })
})

describe('ReplayPanel signs: the placement note', () => {
  const jpegFile = () => new File([new Uint8Array([0xff, 0xd8, 0xff, 0xd9])], 'a.jpg', { type: 'image/jpeg' })
  const withFolder = (extra: Record<string, unknown> = {}) => stub({
    'GET /api/document-folders/tree': { folders: [{ id: 'pp', name: 'Passage photos', parent_id: null }] },
    'POST /api/documents': () => jsonResponse({ document: { id: 'doc1' }, duplicate: false }, 201),
    ...extra,
  })

  it('goes when another sign is picked, when a field changes, and when a photo add fails', async () => {
    signs = [sign('s1', 250), sign('s2', 600)]
    withFolder()
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Signs' }))
    const add = () => fireEvent.change(screen.getByLabelText('Photo file for a new sign'), { target: { files: [jpegFile()] } })
    add()
    expect(await screen.findByTestId('sign-notice')).toBeInTheDocument()
    // another sign picked
    fireEvent.click(await screen.findByRole('button', { name: /^Sign Sign s2 at/ }))
    expect(screen.queryByTestId('sign-notice')).toBeNull()
    // a field changes
    add()
    expect(await screen.findByTestId('sign-notice')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'After passing' }))
    expect(screen.queryByTestId('sign-notice')).toBeNull()
    // a failed add leaves no old note
    add()
    expect(await screen.findByTestId('sign-notice')).toBeInTheDocument()
    withFolder({ 'POST /api/documents': () => jsonResponse({ error: 'document too large' }, 413) })
    add()
    expect(await screen.findByRole('alert')).toHaveTextContent('document too large')
    expect(screen.queryByTestId('sign-notice')).toBeNull()
  })
})

describe('ReplayPanel signs: photos', () => {
  const jpegFile = () => new File([new Uint8Array([0xff, 0xd8, 0xff, 0xd9])], 'whale_tail.jpg', { type: 'image/jpeg' })

  function stubUpload() {
    stub({
      'GET /api/document-folders/tree': { folders: [{ id: 'pp', name: 'Passage photos', parent_id: null }] },
      'POST /api/documents': () => jsonResponse({ document: { id: 'doc1' }, duplicate: false }, 201),
    })
  }

  it('uploads a photo with no capture time to the cursor and says so', async () => {
    stubUpload()
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Signs' }))
    scrub(300)
    fireEvent.change(screen.getByLabelText('Photo file for a new sign'), { target: { files: [jpegFile()] } })
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toEqual({ t_ms: (start + 300) * 1000, title: 'whale tail', note: '', document_id: 'doc1', visibility: 'passing', show_s: 4 })
    expect(await screen.findByTestId('sign-notice')).toHaveTextContent('The photo has no capture time, so the sign is at the current position.')
  })

  const photo = (dt: string, offset?: string) => new File([exifJpeg({ dt, offset }).buffer as ArrayBuffer], 'IMG_0412.jpg', { type: 'image/jpeg' })

  it('places a photo at its EXIF capture time inside the replay, reading it in the boat zone, and seeks there', async () => {
    stubUpload()
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Signs' }))
    scrub(300)
    // 09:10 on 22 Oct at the boat's UTC+10 is 600 s after the passage began.
    fireEvent.change(screen.getByLabelText('Photo file for a new sign'), { target: { files: [photo('2026:10:22 09:10:00')] } })
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toMatchObject({ t_ms: (start + 600) * 1000, title: 'IMG 0412', document_id: 'doc1' })
    expect(await screen.findByTestId('sign-notice')).toHaveTextContent('Placed at the time the photo was taken: 22 Oct 09:10 UTC+10 (the photo has no time zone, so boat time is used).')
    await waitFor(() => expect((screen.getByLabelText('Replay position') as HTMLInputElement).value).toBe('600'))
  })

  it('reads the photo\'s own zone when it has one', async () => {
    stubUpload()
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Signs' }))
    fireEvent.change(screen.getByLabelText('Photo file for a new sign'), { target: { files: [photo('2026:10:21 23:10:00', '+00:00')] } })
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toMatchObject({ t_ms: (start + 600) * 1000 })
  })

  it('a photo taken outside the replay goes to the cursor and says when it was taken', async () => {
    stubUpload()
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Signs' }))
    scrub(300)
    fireEvent.change(screen.getByLabelText('Photo file for a new sign'), { target: { files: [photo('2026:10:25 09:10:00')] } })
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toMatchObject({ t_ms: (start + 300) * 1000 })
    expect(await screen.findByTestId('sign-notice')).toHaveTextContent('The photo was taken 25 Oct 09:10 UTC+10, outside this replay, so the sign is at the current position.')
  })

  it('changing the photo of a sign keeps it where it is', async () => {
    signs = [sign('s1', 250)]
    stubUpload()
    render(<ReplayPanel passages={[passage]} isDarkTheme={false} />)
    fireEvent.click(await screen.findByRole('button', { name: /^Sign Sign s1 at/ }))
    fireEvent.change(screen.getByLabelText('Photo file for this sign'), { target: { files: [photo('2026:10:22 09:10:00')] } })
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0]).toMatchObject({ method: 'PATCH', body: { document_id: 'doc1' } })
  })
})

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { InboxAssignDialog } from '@/components/passages/inbox-assign-dialog'
import { InboxFilingError, type InboxItem } from '@/lib/inbox-api'
import type { PassageSummary } from '@/lib/passages'

const api = vi.hoisted(() => ({ suggestedTitles: vi.fn(), fileToPassage: vi.fn() }))
vi.mock('@/lib/inbox-api', async (orig) => ({ ...(await orig<typeof import('@/lib/inbox-api')>()), inboxApi: api }))

const T0 = Date.UTC(2026, 9, 14)
const min = 60_000
const passage = (id: string, start: number, end: number, title: string) =>
  ({
    id, start: new Date(start).toISOString(), end: new Date(end).toISOString(), title, from_name: '', to_name: '', notes: '', hidden: false,
    edited: false, cruise_id: null, created_at: '', timezone: 'Australia/Brisbane', stats: {},
  }) as unknown as PassageSummary
const passages = [passage('p1', T0, T0 + 120 * min, 'Out'), passage('p2', T0 + 600 * min, T0 + 700 * min, 'Back')]

const photo = (id: string, kind: InboxItem['group']['kind'], capture: number | null): InboxItem => ({
  id, filename: `${id}.jpg`, mime: 'image/jpeg', size_bytes: 1, capture_ms: capture, bearing_deg: null,
  fingerprint: '', sharpness: 0, created_ms: 0, content_url: `/${id}`, thumb_url: `/${id}/thumb`, group: { kind }, gps_mismatch: false,
})

const noTitles = { titles: {}, errors: {} }

beforeEach(() => vi.clearAllMocks())

describe('InboxAssignDialog', () => {
  it('offers a photo between passages the nearest passage start or end, and the others to choose', async () => {
    api.suggestedTitles.mockResolvedValue(noTitles)
    render(<InboxAssignDialog items={[photo('a', 'off_passage', T0 + 130 * min), photo('b', 'off_passage', T0 + 590 * min)]} passages={passages} onClose={vi.fn()} />)
    const a = screen.getByLabelText('Where a.jpg goes') as HTMLSelectElement
    const b = screen.getByLabelText('Where b.jpg goes') as HTMLSelectElement
    expect(a.value).toBe(String(T0 + 120 * min))
    expect(b.value).toBe(String(T0 + 600 * min))
    expect(Array.from(a.options).map((o) => o.textContent)).toEqual([
      'Choose where it goes…', expect.stringContaining('Start of Out'), expect.stringContaining('End of Out'),
      expect.stringContaining('Start of Back'), expect.stringContaining('End of Back'),
    ])
    await waitFor(() => expect(screen.queryByText('Looking up place names…')).toBeNull())
  })

  it('says in one line that the titles are place names when Mate\'s settings could not be read', async () => {
    api.suggestedTitles.mockResolvedValue({ titles: { a: 'Hook Island' }, errors: {}, mateError: "Mate's settings couldn't be read; these are place-name titles" })
    render(<InboxAssignDialog items={[photo('a', 'passage', T0 + min)]} passages={passages} mateTitles onClose={vi.fn()} />)
    expect(await screen.findByText("Mate's settings couldn't be read; these are place-name titles")).toBeInTheDocument()
    expect((screen.getByLabelText('Title for a.jpg') as HTMLInputElement).value).toBe('Hook Island')
  })

  it('shows the photo itself when it has no thumbnail', async () => {
    api.suggestedTitles.mockResolvedValue(noTitles)
    const { container } = render(
      <InboxAssignDialog items={[{ ...photo('a', 'passage', T0 + min), thumb_url: undefined }]} passages={passages} onClose={vi.fn()} />,
    )
    expect(container.ownerDocument.querySelector('img')).toHaveAttribute('src', '/a')
    await waitFor(() => expect(screen.queryByText('Looking up place names…')).toBeNull())
  })

  it('makes a photo with no time pick a place', async () => {
    api.suggestedTitles.mockResolvedValue(noTitles)
    render(<InboxAssignDialog items={[photo('n', 'no_time', null)]} passages={passages} onClose={vi.fn()} />)
    expect((screen.getByLabelText('Where n.jpg goes') as HTMLSelectElement).value).toBe('')
    fireEvent.change(screen.getByLabelText('Title for n.jpg'), { target: { value: 'Dock' } })
    expect(screen.getByRole('button', { name: 'Add sign' })).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Where n.jpg goes'), { target: { value: String(T0) } })
    expect(screen.getByRole('button', { name: 'Add sign' })).toBeEnabled()
  })

  it('puts a late suggestion on its own photo after a partial failure has dropped a row', async () => {
    let answer: (v: { titles: Record<string, string>; errors: Record<string, string> }) => void = () => {}
    api.suggestedTitles.mockReturnValue(new Promise((resolve) => { answer = resolve }))
    api.fileToPassage.mockResolvedValue([{ id: 'a', ok: true }, { id: 'b', ok: false, error: 'nope' }])
    render(<InboxAssignDialog items={[photo('a', 'passage', T0 + min), photo('b', 'passage', T0 + 2 * min)]} passages={passages} onClose={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Title for a.jpg'), { target: { value: 'A' } })
    fireEvent.change(screen.getByLabelText('Title for b.jpg'), { target: { value: 'B' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add 2 signs' }))
    await waitFor(() => expect(screen.queryByLabelText('Title for a.jpg')).toBeNull())
    fireEvent.change(screen.getByLabelText('Title for b.jpg'), { target: { value: '' } })
    answer({ titles: { a: 'Place A', b: 'Place B' }, errors: {} })
    await waitFor(() => expect(screen.queryByText('Looking up place names…')).toBeNull())
    expect((screen.getByLabelText('Title for b.jpg') as HTMLInputElement).value).toBe('Place B')
  })

  it('keeps only the unsent photos, with the reason, when a later request fails', async () => {
    api.suggestedTitles.mockResolvedValue(noTitles)
    api.fileToPassage.mockRejectedValue(new InboxFilingError(new Error('network dropped'), [{ id: 'a', ok: true }]))
    render(<InboxAssignDialog items={[photo('a', 'passage', T0 + min), photo('b', 'passage', T0 + 2 * min)]} passages={passages} onClose={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Title for a.jpg'), { target: { value: 'A' } })
    fireEvent.change(screen.getByLabelText('Title for b.jpg'), { target: { value: 'B' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add 2 signs' }))
    await waitFor(() => expect(screen.queryByLabelText('Title for a.jpg')).toBeNull())
    expect(screen.getByLabelText('Title for b.jpg')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('network dropped')
  })

  it('stops saying it is looking up names once the server answers with blanks, and keeps what was typed', async () => {
    let answer: (v: { titles: Record<string, string>; errors: Record<string, string> }) => void = () => {}
    api.suggestedTitles.mockReturnValue(new Promise((resolve) => { answer = resolve }))
    render(<InboxAssignDialog items={[photo('a', 'passage', T0 + min)]} passages={passages} onClose={vi.fn()} />)
    expect(screen.getByText('Looking up place names…')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Title for a.jpg'), { target: { value: 'Typed' } })
    answer({ titles: { a: '' }, errors: {} })
    await waitFor(() => expect(screen.queryByText('Looking up place names…')).toBeNull())
    expect((screen.getByLabelText('Title for a.jpg') as HTMLInputElement).value).toBe('Typed')
  })

  it('says Mate is writing the titles when it is, and shows the reason a title could not be suggested', async () => {
    let answer: (v: { titles: Record<string, string>; errors: Record<string, string> }) => void = () => {}
    api.suggestedTitles.mockReturnValue(new Promise((resolve) => { answer = resolve }))
    render(
      <InboxAssignDialog mateTitles items={[photo('a', 'passage', T0 + min), photo('b', 'passage', T0 + 2 * min)]} passages={passages} onClose={vi.fn()} />,
    )
    expect(screen.getByText('Mate is writing titles…')).toBeInTheDocument()
    answer({ titles: { a: 'Dolphins off the bow' }, errors: { b: 'Mate could not suggest a title: 429' } })
    await waitFor(() => expect(screen.queryByText('Mate is writing titles…')).toBeNull())
    expect((screen.getByLabelText('Title for a.jpg') as HTMLInputElement).value).toBe('Dolphins off the bow')
    expect((screen.getByLabelText('Title for b.jpg') as HTMLInputElement).value).toBe('')
    expect(screen.getByText('Mate could not suggest a title: 429')).toBeInTheDocument()
  })
})

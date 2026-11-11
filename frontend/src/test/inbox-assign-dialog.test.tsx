import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { InboxAssignDialog } from '@/components/passages/inbox-assign-dialog'
import type { InboxItem } from '@/lib/inbox-api'
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
  id, filename: `${id}.jpg`, mime: 'image/jpeg', size_bytes: 1, capture_ms: capture, capture_offset_min: null, bearing_deg: null, has_bearing: false,
  fingerprint: '', sharpness: 0, created_ms: 0, content_url: `/${id}`, group: { kind }, gps_mismatch: false,
})

beforeEach(() => vi.clearAllMocks())

describe('InboxAssignDialog', () => {
  it('offers a photo between passages the nearest passage start or end, and the others to choose', async () => {
    api.suggestedTitles.mockResolvedValue({})
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

  it('makes a photo with no time pick a place', async () => {
    api.suggestedTitles.mockResolvedValue({})
    render(<InboxAssignDialog items={[photo('n', 'no_time', null)]} passages={passages} onClose={vi.fn()} />)
    expect((screen.getByLabelText('Where n.jpg goes') as HTMLSelectElement).value).toBe('')
    fireEvent.change(screen.getByLabelText('Title for n.jpg'), { target: { value: 'Dock' } })
    expect(screen.getByRole('button', { name: 'Add sign' })).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Where n.jpg goes'), { target: { value: String(T0) } })
    expect(screen.getByRole('button', { name: 'Add sign' })).toBeEnabled()
  })

  it('stops saying it is looking up names once the server answers with blanks, and keeps what was typed', async () => {
    let answer: (v: Record<string, string>) => void = () => {}
    api.suggestedTitles.mockReturnValue(new Promise((resolve) => { answer = resolve }))
    render(<InboxAssignDialog items={[photo('a', 'passage', T0 + min)]} passages={passages} onClose={vi.fn()} />)
    expect(screen.getByText('Looking up place names…')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Title for a.jpg'), { target: { value: 'Typed' } })
    answer({ a: '' })
    await waitFor(() => expect(screen.queryByText('Looking up place names…')).toBeNull())
    expect((screen.getByLabelText('Title for a.jpg') as HTMLInputElement).value).toBe('Typed')
  })
})

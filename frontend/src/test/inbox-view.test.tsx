import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { InboxView } from '@/components/passages/inbox-view'
import type { InboxItem } from '@/lib/inbox-api'
import type { PassageSummary } from '@/lib/passages'

const env = vi.hoisted(() => ({ items: [] as InboxItem[], folders: [{ id: 'folder-1' }] as { id: string }[], keys: [] as unknown[] }))

vi.mock('@/lib/api-query', async (orig) => ({
  ...(await orig<typeof import('@/lib/api-query')>()),
  useApiQuery: (opts: { queryKey: unknown }) => (env.keys.push(opts.queryKey), { data: env.folders, isLoading: false, error: null, isFetching: false, refetch: vi.fn() }),
}))

vi.mock('@/hooks/use-inbox', () => ({
  useInbox: () => ({ items: env.items, loading: false, error: null, refresh: vi.fn() }),
}))
vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselStateSelector: (select: (state: { timeZone: string | null }) => unknown) => select({ timeZone: 'Australia/Brisbane' }),
}))
const api = vi.hoisted(() => ({ bin: vi.fn(), fileToPassage: vi.fn(), fileToDocuments: vi.fn(), suggestedTitles: vi.fn() }))
vi.mock('@/lib/inbox-api', async (orig) => ({ ...(await orig<typeof import('@/lib/inbox-api')>()), inboxApi: api }))
// The folder picker reads the library; this test is about the Inbox.
vi.mock('@/components/documents/folder-picker-dialog', () => ({
  FolderPickerDialog: ({ open, onPick, confirmLabel, initialFolderId }: { open: boolean; onPick: (id: string | null) => void; confirmLabel?: string; initialFolderId?: string | null }) =>
    open ? <button type="button" data-initial={initialFolderId ?? 'root'} onClick={() => onPick('folder-1')}>{confirmLabel}</button> : null,
}))

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), message: vi.fn() } }))

const T0 = Date.UTC(2026, 9, 14)
const min = 60_000
let n = 0
function item(over: Partial<InboxItem>): InboxItem {
  n += 1
  return {
    id: `i${n}`, filename: `IMG_${n}.jpg`, mime: 'image/jpeg', size_bytes: 1, capture_ms: T0 + n * min,
    bearing_deg: null, fingerprint: '', sharpness: 0, created_ms: n, content_url: `/api/inbox/i${n}/content`, thumb_url: `/api/inbox/i${n}/thumb`,
    group: { kind: 'passage', passage_id: 'p1' }, gps_mismatch: false, ...over,
  }
}
const passage = {
  id: 'p1', start: new Date(T0).toISOString(), end: new Date(T0 + 120 * min).toISOString(), title: 'Out to Hook', from_name: '', to_name: '',
  notes: '', hidden: false, edited: false, cruise_id: null, created_at: '', timezone: 'Australia/Brisbane', stats: {},
} as unknown as PassageSummary

const view = (canWrite = true) => render(<InboxView canWrite={canWrite} passages={[passage]} onBack={vi.fn()} onOpenUpload={vi.fn()} />)

beforeEach(() => {
  vi.clearAllMocks()
  n = 0
  env.items = []
  env.keys = []
  env.folders = [{ id: 'folder-1' }]
  window.localStorage.clear()
})

describe('InboxView', () => {
  it('shows an empty Inbox with a way to share', () => {
    view()
    expect(screen.getByText('Nothing waiting')).toBeInTheDocument()
  })

  it('groups the contact sheet and says why underway photos cannot be assigned yet', () => {
    env.items = [item({}), item({ group: { kind: 'underway' } }), item({ group: { kind: 'no_time' }, capture_ms: null })]
    view()
    expect(screen.getByRole('region', { name: 'Underway now' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: /Out to Hook/ })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'No time' })).toBeInTheDocument()
    expect(screen.getByText(/recorded once it ends/)).toBeInTheDocument()
  })

  it('marks pictures by tapping and keeps Add to passage off for photos taken underway', () => {
    env.items = [item({}), item({ group: { kind: 'underway' } })]
    view()
    const add = screen.getByRole('button', { name: /Add to passage/ })
    expect(add).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: /IMG_1\.jpg/ }))
    expect(add).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: /IMG_2\.jpg/ }))
    expect(add).toBeDisabled()
    expect(add).toHaveAttribute('title', expect.stringMatching(/once the passage is recorded/))
    expect(screen.getByRole('status')).toHaveTextContent('2 marked')
  })

  it('asks before deleting, then deletes what is marked', async () => {
    env.items = [item({}), item({})]
    api.bin.mockResolvedValue({ deleted: 1, busy: [] })
    view()
    fireEvent.click(screen.getAllByRole('button').filter((b) => b.getAttribute('aria-pressed') !== null)[0])
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText(/originals stay on your phone/)).toBeInTheDocument()
    expect(api.bin).not.toHaveBeenCalled()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(api.bin).toHaveBeenCalledWith(['i1']))
  })

  it('says how many items were left because they are being filed', async () => {
    env.items = [item({}), item({}), item({})]
    api.bin.mockResolvedValue({ deleted: 2, busy: [{ id: 'i3', ok: false, error: 'this item is already being filed' }] })
    view()
    for (const b of screen.getAllByRole('button').filter((b) => b.getAttribute('aria-pressed') !== null)) fireEvent.click(b)
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('2 deleted, 1 is being filed'))
  })

  it('shows a photo with no thumbnail as the picture itself, and a PDF as an icon', () => {
    env.items = [
      item({ thumb_url: undefined, filename: 'plain.jpg' }),
      item({ thumb_url: undefined, filename: 'chart.pdf', mime: 'application/pdf' }),
    ]
    view()
    const photoButton = screen.getByRole('button', { name: /plain\.jpg/ })
    expect(photoButton.querySelector('img')).toHaveAttribute('src', '/api/inbox/i1/content')
    expect(screen.getByRole('button', { name: /chart\.pdf/ }).querySelector('img')).toBeNull()
  })

  it('flags duplicates and soft frames as suggestions and marks them only on request', () => {
    env.items = [
      item({ capture_ms: T0, fingerprint: '00ff00ff00ff00ff', sharpness: 90 }),
      item({ capture_ms: T0 + 2000, fingerprint: '00ff00ff00ff00ff', sharpness: 50 }),
      ...[100, 110, 105].map((s, i) => item({ capture_ms: T0 + (10 + i) * min, fingerprint: ['0000000000000000', 'ffffffffffffffff', 'aaaaaaaaaaaaaaaa'][i], sharpness: s })),
      item({ capture_ms: T0 + 30 * min, fingerprint: 'abababababababab', sharpness: 4 }),
    ]
    view()
    expect(screen.getByText('Duplicate')).toBeInTheDocument()
    expect(screen.getByText('Best of 2')).toBeInTheDocument()
    expect(screen.getByText('Soft')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Tap pictures to mark them')
    fireEvent.click(screen.getByRole('button', { name: /Mark suggested deletions \(2\)/ }))
    expect(screen.getByRole('status')).toHaveTextContent('2 marked')
  })

  it('shows the compass mark for a real bearing and the warning for a position mismatch', () => {
    env.items = [item({ bearing_deg: 270.4 }), item({ gps_mismatch: true })]
    view()
    expect(screen.getByLabelText('Facing 270 degrees')).toBeInTheDocument()
    expect(screen.getByLabelText('Phone position does not match the track')).toBeInTheDocument()
  })

  it('sends the marked items to the chosen Documents folder and remembers it', async () => {
    env.items = [item({})]
    api.fileToDocuments.mockResolvedValue([{ id: 'i1', ok: true, document_id: 'd1' }])
    view()
    fireEvent.click(screen.getAllByRole('button').filter((b) => b.getAttribute('aria-pressed') !== null)[0])
    fireEvent.click(screen.getByRole('button', { name: /Send to Documents/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Send here' }))
    await waitFor(() => expect(api.fileToDocuments).toHaveBeenCalledWith('folder-1', [{ id: 'i1' }]))
    expect(window.localStorage.getItem('inbox.lastFolder')).toBe('folder-1')
  })

  it('opens the folder picker on the remembered folder only while that folder still exists', async () => {
    env.items = [item({})]
    window.localStorage.setItem('inbox.lastFolder', 'folder-1')
    view()
    fireEvent.click(screen.getAllByRole('button').filter((b) => b.getAttribute('aria-pressed') !== null)[0])
    fireEvent.click(screen.getByRole('button', { name: /Send to Documents/ }))
    expect((await screen.findByRole('button', { name: 'Send here' })).getAttribute('data-initial')).toBe('folder-1')
  })

  it('reads the folder tree under the Documents key, so changes made there reach the Inbox', () => {
    env.items = [item({})]
    view()
    expect(env.keys).toContainEqual(['/api/document-folders/tree'])
    expect(env.keys).not.toContainEqual(['inbox-folders'])
  })

  it('opens the folder picker at the root when the remembered folder has gone', async () => {
    env.items = [item({})]
    env.folders = [{ id: 'other' }]
    window.localStorage.setItem('inbox.lastFolder', 'folder-1')
    view()
    fireEvent.click(screen.getAllByRole('button').filter((b) => b.getAttribute('aria-pressed') !== null)[0])
    fireEvent.click(screen.getByRole('button', { name: /Send to Documents/ }))
    expect((await screen.findByRole('button', { name: 'Send here' })).getAttribute('data-initial')).toBe('root')
  })

  it('shows the small preview, and a file icon with the name where an item has none', () => {
    env.items = [item({}), item({ mime: 'application/pdf', filename: 'receipt.pdf', thumb_url: undefined })]
    const { container } = view()
    const imgs = Array.from(container.querySelectorAll('img')).map((i) => i.getAttribute('src'))
    expect(imgs).toEqual(['/api/inbox/i1/thumb'])
    expect(screen.getByText('receipt.pdf')).toBeInTheDocument()
  })

  it('shows Clear only while something is marked', () => {
    env.items = [item({})]
    view()
    expect(screen.queryByRole('button', { name: 'Clear' })).toBeNull()
    const cell = screen.getAllByRole('button').filter((b) => b.getAttribute('aria-pressed') !== null)[0]
    fireEvent.click(cell)
    expect(screen.getByRole('button', { name: 'Clear' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    expect(screen.queryByRole('button', { name: 'Clear' })).toBeNull()
  })

  it('offers no actions to a sign-in that can only read', () => {
    env.items = [item({})]
    view(false)
    expect(screen.queryByRole('toolbar')).toBeNull()
    expect(screen.getAllByRole('button').filter((b) => b.getAttribute('aria-pressed') !== null)[0]).toBeDisabled()
  })
})

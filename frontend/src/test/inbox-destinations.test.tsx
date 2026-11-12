import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { InboxView } from '@/components/passages/inbox-view'
import { InboxFilingError, type InboxItem } from '@/lib/inbox-api'
import type { PassageSummary } from '@/lib/passages'

const env = vi.hoisted(() => ({ items: [] as InboxItem[] }))

vi.mock('@/hooks/use-inbox', () => ({
  useInbox: () => ({ items: env.items, loading: false, error: null, refresh: vi.fn() }),
}))
vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselStateSelector: (select: (state: { timeZone: string | null }) => unknown) => select({ timeZone: 'Australia/Brisbane' }),
}))
vi.mock('@/hooks/use-document-folder-tree', () => ({ useDocumentFolderTree: () => ({ folders: [], loading: false }) }))
vi.mock('@/hooks/use-inventory', async (orig) => ({
  ...(await orig<typeof import('@/hooks/use-inventory')>()),
  useEquipment: () => ({ items: [{ id: 'e1', name: 'Bilge pump', manufacturer: '', model: '', aliases: [], status: 'deployed', zone_name: '' }], loading: false, error: null, refresh: vi.fn() }),
}))
vi.mock('@/hooks/use-open-jobs', async (orig) => ({
  ...(await orig<typeof import('@/hooks/use-open-jobs')>()),
  useOpenJobs: () => ({ jobs: [{ id: 'j1', title: 'Leaking hose', equipment_name: '', status: 'open' }], loading: false, error: null, refresh: vi.fn() }),
}))
const api = vi.hoisted(() => ({ bin: vi.fn(), fileToPassage: vi.fn(), fileToDocuments: vi.fn(), fileToEquipment: vi.fn(), fileToOpenJob: vi.fn(), suggestedTitles: vi.fn() }))
vi.mock('@/lib/inbox-api', async (orig) => ({ ...(await orig<typeof import('@/lib/inbox-api')>()), inboxApi: api }))
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), message: vi.fn() } }))

let n = 0
function item(over: Partial<InboxItem> = {}): InboxItem {
  n += 1
  return {
    id: `i${n}`, filename: `IMG_${n}.jpg`, mime: 'image/jpeg', size_bytes: 1, capture_ms: null, bearing_deg: null, fingerprint: '', sharpness: 0,
    created_ms: n, content_url: `/api/inbox/i${n}/content`, thumb_url: `/api/inbox/i${n}/thumb`, group: { kind: 'no_time' }, gps_mismatch: false, ...over,
  }
}
const pdf = (): InboxItem => item({ mime: 'application/pdf', filename: `scan_${n + 1}.pdf`, thumb_url: undefined })
const mark = (name: RegExp) => fireEvent.click(screen.getByRole('button', { name }))
const view = () => render(<InboxView canWrite passages={[]} onOpenUpload={vi.fn()} />)

beforeEach(() => {
  vi.clearAllMocks()
  n = 0
  env.items = []
  window.localStorage.clear()
})

describe('InboxView equipment and job destinations', () => {
  it('keeps both actions off until something is marked, and when every marked item is a PDF', () => {
    env.items = [item(), pdf()]
    view()
    const equipment = screen.getByRole('button', { name: /Add to equipment/ })
    const jobBtn = screen.getByRole('button', { name: /Add to a job/ })
    expect(equipment).toBeDisabled()
    expect(jobBtn).toBeDisabled()
    mark(/scan_2\.pdf/)
    expect(equipment).toBeDisabled()
    expect(equipment).toHaveAttribute('title', 'Only JPEG or PNG photos can go on equipment')
    expect(jobBtn).toHaveAttribute('title', 'Only JPEG or PNG photos can go on a job')
    mark(/IMG_1\.jpg/)
    expect(equipment).toBeEnabled()
    expect(jobBtn).toBeEnabled()
  })

  it('counts a GIF as not a photo, as the box does, before anything is sent', async () => {
    env.items = [item({ mime: 'image/gif', filename: 'anim.gif' }), item()]
    api.fileToEquipment.mockResolvedValue([{ id: 'i2', ok: true }])
    view()
    mark(/anim\.gif/)
    const equipment = screen.getByRole('button', { name: /Add to equipment/ })
    expect(equipment).toBeDisabled()
    expect(equipment).toHaveAttribute('title', 'Only JPEG or PNG photos can go on equipment')
    mark(/IMG_2\.jpg/)
    fireEvent.click(equipment)
    expect(screen.getByText('Add 1 photo to equipment')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('radio', { name: /Bilge pump/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add here' }))
    await waitFor(() => expect(api.fileToEquipment).toHaveBeenCalledWith('e1', [{ id: 'i2' }]))
    expect(toast.message).toHaveBeenCalledWith('1 file stayed in the Inbox', { description: 'Only JPEG or PNG photos can go on equipment. Send the rest to Documents.' })
  })

  it('files the marked photos to the chosen equipment item and says so', async () => {
    env.items = [item(), item()]
    api.fileToEquipment.mockResolvedValue([{ id: 'i1', ok: true }, { id: 'i2', ok: true }])
    view()
    mark(/IMG_1\.jpg/)
    mark(/IMG_2\.jpg/)
    fireEvent.click(screen.getByRole('button', { name: /Add to equipment/ }))
    fireEvent.click(screen.getByRole('radio', { name: /Bilge pump/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add here' }))
    await waitFor(() => expect(api.fileToEquipment).toHaveBeenCalledWith('e1', [{ id: 'i1' }, { id: 'i2' }]))
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('Added 2 photos to Bilge pump'))
    expect(window.localStorage.getItem('inbox.lastEquipment')).toBe('e1')
  })

  it('opens on the equipment item used last time', async () => {
    window.localStorage.setItem('inbox.lastEquipment', 'e1')
    env.items = [item()]
    view()
    mark(/IMG_1\.jpg/)
    fireEvent.click(screen.getByRole('button', { name: /Add to equipment/ }))
    expect(screen.getByRole('radio', { name: /Bilge pump/ })).toBeChecked()
  })

  it('files the photos of a mixed selection and says how many other files stayed', async () => {
    env.items = [item(), pdf(), pdf()]
    api.fileToOpenJob.mockResolvedValue([{ id: 'i1', ok: true }])
    view()
    mark(/IMG_1\.jpg/)
    mark(/scan_2\.pdf/)
    mark(/scan_3\.pdf/)
    fireEvent.click(screen.getByRole('button', { name: /Add to a job/ }))
    fireEvent.click(screen.getByRole('radio', { name: /Leaking hose/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add here' }))
    await waitFor(() => expect(api.fileToOpenJob).toHaveBeenCalledWith('j1', [{ id: 'i1' }]))
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('Added 1 photo to Leaking hose'))
    expect(toast.message).toHaveBeenCalledWith('2 files stayed in the Inbox', { description: 'Only JPEG or PNG photos can go on a job. Send the rest to Documents.' })
    expect(window.localStorage.getItem('inbox.lastJob')).toBe('j1')
  })

  it('names the item that stayed when the box refuses one', async () => {
    env.items = [item(), item()]
    api.fileToEquipment.mockResolvedValue([{ id: 'i1', ok: true }, { id: 'i2', ok: false, error: 'that equipment item no longer exists' }])
    view()
    mark(/IMG_1\.jpg/)
    mark(/IMG_2\.jpg/)
    fireEvent.click(screen.getByRole('button', { name: /Add to equipment/ }))
    fireEvent.click(screen.getByRole('radio', { name: /Bilge pump/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add here' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('1 item stayed in the Inbox', { description: 'that equipment item no longer exists' }))
    expect(toast.success).toHaveBeenCalledWith('Added 1 photo to Bilge pump')
  })

  it('does not remember an item that refused every photo', async () => {
    env.items = [item()]
    api.fileToEquipment.mockResolvedValue([{ id: 'i1', ok: false, error: 'that equipment item no longer exists' }])
    view()
    mark(/IMG_1\.jpg/)
    fireEvent.click(screen.getByRole('button', { name: /Add to equipment/ }))
    fireEvent.click(screen.getByRole('radio', { name: /Bilge pump/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add here' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalled())
    expect(window.localStorage.getItem('inbox.lastEquipment')).toBeNull()
  })

  it('shows each reason beside its picture until the next action', async () => {
    env.items = [item(), item(), pdf()]
    api.fileToEquipment.mockResolvedValue([{ id: 'i1', ok: true }, { id: 'i2', ok: false, error: 'that equipment item no longer exists' }])
    view()
    mark(/IMG_1\.jpg/)
    mark(/IMG_2\.jpg/)
    mark(/scan_3\.pdf/)
    fireEvent.click(screen.getByRole('button', { name: /Add to equipment/ }))
    fireEvent.click(screen.getByRole('radio', { name: /Bilge pump/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add here' }))
    const reason = await screen.findByText('that equipment item no longer exists')
    expect(reason.closest('li')).toContainElement(screen.getByRole('button', { name: /IMG_2\.jpg/ }))
    expect(screen.getByText('Only JPEG or PNG photos can go on equipment').closest('li')).toContainElement(screen.getByRole('button', { name: /scan_3\.pdf/ }))
    // The next filing replaces them.
    api.fileToEquipment.mockResolvedValue([{ id: 'i2', ok: true }])
    fireEvent.click(screen.getByRole('button', { name: /Add to equipment/ }))
    fireEvent.click(screen.getByRole('radio', { name: /Bilge pump/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add here' }))
    await waitFor(() => expect(api.fileToEquipment).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.queryByText('that equipment item no longer exists')).not.toBeInTheDocument())
  })

  it('clears the reasons when the operator starts filing through Add to passage', async () => {
    env.items = [item(), item()]
    api.fileToEquipment.mockResolvedValue([{ id: 'i1', ok: true }, { id: 'i2', ok: false, error: 'that equipment item no longer exists' }])
    const passage = { id: 'p1', start: '2026-10-14T00:00:00Z', end: '2026-10-14T02:00:00Z', title: 'Out to Hook', from_name: '', to_name: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '', timezone: 'Australia/Brisbane', stats: {} } as unknown as PassageSummary
    api.suggestedTitles.mockResolvedValue({ titles: {}, errors: {} })
    render(<InboxView canWrite passages={[passage]} onOpenUpload={vi.fn()} />)
    mark(/IMG_1\.jpg/)
    mark(/IMG_2\.jpg/)
    fireEvent.click(screen.getByRole('button', { name: /Add to equipment/ }))
    fireEvent.click(screen.getByRole('radio', { name: /Bilge pump/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add here' }))
    await screen.findByText('that equipment item no longer exists')
    fireEvent.click(screen.getByRole('button', { name: /Add to passage/ }))
    expect(screen.queryByText('that equipment item no longer exists')).not.toBeInTheDocument()
    await waitFor(() => expect(api.suggestedTitles).toHaveBeenCalled())
  })

  it('reports what was filed and keeps the rest marked when a later request fails', async () => {
    env.items = [item(), item(), item()]
    api.fileToEquipment.mockRejectedValue(new InboxFilingError(new Error('network dropped'), [{ id: 'i1', ok: true }]))
    view()
    mark(/IMG_1\.jpg/)
    mark(/IMG_2\.jpg/)
    mark(/IMG_3\.jpg/)
    fireEvent.click(screen.getByRole('button', { name: /Add to equipment/ }))
    fireEvent.click(screen.getByRole('radio', { name: /Bilge pump/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add here' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Added 1 photo to Bilge pump; the rest could not be sent', { description: 'network dropped' }))
    expect(window.localStorage.getItem('inbox.lastEquipment')).toBe('e1')
    expect(screen.getAllByText('network dropped')).toHaveLength(2)
    expect(screen.getByRole('status')).toHaveTextContent('2 marked')
  })

  it('shows the server message when the call itself fails', async () => {
    env.items = [item()]
    api.fileToEquipment.mockRejectedValue(new Error('the Inbox is not running'))
    view()
    mark(/IMG_1\.jpg/)
    fireEvent.click(screen.getByRole('button', { name: /Add to equipment/ }))
    fireEvent.click(screen.getByRole('radio', { name: /Bilge pump/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add here' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not add to equipment', { description: 'the Inbox is not running' }))
  })
})

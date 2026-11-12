import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { InboxView } from '@/components/passages/inbox-view'
import type { InboxItem } from '@/lib/inbox-api'

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
    expect(equipment).toHaveAttribute('title', 'Only photos can go on equipment')
    expect(jobBtn).toHaveAttribute('title', 'Only photos can go on a job')
    mark(/IMG_1\.jpg/)
    expect(equipment).toBeEnabled()
    expect(jobBtn).toBeEnabled()
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

  it('files the photos of a mixed selection and says how many PDFs stayed', async () => {
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
    expect(toast.message).toHaveBeenCalledWith('2 PDFs stayed in the Inbox', expect.anything())
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

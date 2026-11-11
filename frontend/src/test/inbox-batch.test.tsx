import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { InboxUploadPage } from '@/components/passages/inbox-upload-page'
import { inboxApi, type InboxUploadMeta } from '@/lib/inbox-api'
import { uploadFiles, type PreparedFile } from '@/lib/inbox-upload'

// Asking the box before shrinking, refreshing once a batch, and a thumbnail
// sent beside every photo. (One decode per photo: inbox-image.test.ts.)

const NO_META: InboxUploadMeta = { captureMs: null, bearingDeg: null, phoneLat: null, phoneLon: null, fingerprint: '', sharpness: 0, sourceSha256: '' }
const prepared = (name: string): PreparedFile => ({ blob: new Blob([name]), thumb: new Blob(['t']), filename: name, meta: NO_META, warnings: [] })
const files = (...names: string[]) => names.map((n) => new File([n], n, { type: 'image/jpeg' }))
const stored = async () => ({ status: 'stored' as const, item: {} as never })

describe('uploadFiles asks the box which photos it already has', () => {
  it('hashes every file first, asks once, and only prepares and sends the rest', async () => {
    const order: string[] = []
    const tally = await uploadFiles(files('a.jpg', 'b.jpg', 'c.jpg'), {
      hash: async (f) => { order.push(`hash:${f.name}`); return `h-${f.name}` },
      known: async (hashes) => { order.push(`known:${hashes.join(',')}`); return ['h-b.jpg'] },
      prepare: async (f, sha) => { order.push(`prepare:${f.name}:${sha}`); return prepared(f.name) },
      upload: async (p) => { order.push(`upload:${p.filename}`); return stored() },
    })
    expect(order).toEqual([
      'hash:a.jpg', 'hash:b.jpg', 'hash:c.jpg',
      'known:h-a.jpg,h-b.jpg,h-c.jpg',
      'prepare:a.jpg:h-a.jpg', 'upload:a.jpg', 'prepare:c.jpg:h-c.jpg', 'upload:c.jpg',
    ])
    expect(tally).toMatchObject({ total: 3, done: 3, uploaded: 2, skipped: 1, failed: [] })
  })

  it('does not ask about a file with no hash, and does not call at all when none has one', async () => {
    const known = vi.fn(async () => [])
    const tally = await uploadFiles(files('a.pdf'), { hash: async () => '', known, prepare: async (f) => prepared(f.name), upload: stored })
    expect(known).not.toHaveBeenCalled()
    expect(tally.uploaded).toBe(1)
  })

  it('asks in groups of at most 500, the most the box takes', async () => {
    const many = files(...Array.from({ length: 1200 }, (_, i) => `p${i}.jpg`))
    const sizes: number[] = []
    const tally = await uploadFiles(many, {
      hash: async (f) => `h-${f.name}`,
      known: async (hashes) => { sizes.push(hashes.length); return hashes },
      prepare: vi.fn(),
      upload: vi.fn(),
    })
    expect(sizes).toEqual([500, 500, 200])
    expect(tally).toMatchObject({ skipped: 1200, uploaded: 0, done: 1200 })
  })

  it('fails the batch when the box cannot be asked, rather than sending everything blind', async () => {
    const prepare = vi.fn()
    await expect(uploadFiles(files('a.jpg'), {
      hash: async () => 'h', known: async () => { throw new Error('HTTP 503') }, prepare, upload: vi.fn(),
    })).rejects.toThrow('HTTP 503')
    expect(prepare).not.toHaveBeenCalled()
  })

  it('reports a file that cannot be read and goes on with the rest', async () => {
    const tally = await uploadFiles(files('a.jpg', 'bad.jpg'), {
      hash: async (f) => { if (f.name === 'bad.jpg') throw new Error('unreadable'); return 'h' },
      known: async () => [], prepare: async (f) => prepared(f.name), upload: stored,
    })
    expect(tally.uploaded).toBe(1)
    expect(tally.failed).toEqual([{ name: 'bad.jpg', error: 'unreadable' }])
  })

  it('calls the checkpoint after every ten stored files', async () => {
    const checkpoint = vi.fn()
    await uploadFiles(files(...Array.from({ length: 25 }, (_, i) => `p${i}.jpg`)), {
      hash: async (f) => f.name, known: async () => [], prepare: async (f) => prepared(f.name), upload: stored, onCheckpoint: checkpoint,
    })
    expect(checkpoint).toHaveBeenCalledTimes(2)
  })
})

const net = vi.hoisted(() => ({ apiRequest: vi.fn(), invalidateQueries: vi.fn() }))
vi.mock('@/lib/api-query', async (orig) => ({ ...(await orig<typeof import('@/lib/api-query')>()), apiRequest: net.apiRequest, invalidateQueries: net.invalidateQueries }))

describe('inboxApi', () => {
  beforeEach(() => { net.apiRequest.mockReset(); net.invalidateQueries.mockReset() })

  it('uploads without refreshing, and sends the thumbnail in the same request', async () => {
    net.apiRequest.mockResolvedValue({ status: 'stored' })
    await inboxApi.upload(new Blob(['x']), 'a.jpg', NO_META, new Blob(['t'], { type: 'image/jpeg' }))
    await inboxApi.upload(new Blob(['y']), 'b.jpg', NO_META, null)
    expect(net.invalidateQueries).not.toHaveBeenCalled()
    const form = net.apiRequest.mock.calls[0][1].body as FormData
    expect(form.get('thumb')).toBeInstanceOf(Blob)
    expect(form.get('file')).toBeInstanceOf(Blob)
    expect((net.apiRequest.mock.calls[1][1].body as FormData).has('thumb')).toBe(false)
    expect(form.has('capture_offset_min')).toBe(false)
  })

  it('refreshes the list and the count together, or the count alone', async () => {
    await inboxApi.refresh()
    expect(net.invalidateQueries).toHaveBeenCalledTimes(2)
    net.invalidateQueries.mockClear()
    await inboxApi.refreshCount()
    expect(net.invalidateQueries).toHaveBeenCalledTimes(1)
  })

  it('splits a big selection into requests of at most 500 and merges the answers', async () => {
    const ids = Array.from({ length: 600 }, (_, i) => `id${i}`)
    const sizes = (pick: (body: never) => number) => net.apiRequest.mock.calls.map(([, init]) => pick(init as never))

    net.apiRequest.mockImplementation(async (_p: string, init: { json: { ids: string[] } }) => ({ deleted: init.json.ids.length, busy: [{ id: init.json.ids[0], ok: false }] }))
    await expect(inboxApi.bin(ids)).resolves.toMatchObject({ deleted: 600, busy: [{ id: 'id0' }, { id: 'id500' }] })
    expect(sizes((i: { json: { ids: string[] } }) => i.json.ids.length)).toEqual([500, 100])

    net.apiRequest.mockReset()
    net.apiRequest.mockImplementation(async (_p: string, init: { json: { items: { id: string }[] } }) => ({ results: init.json.items.map((i) => ({ id: i.id, ok: true })) }))
    const items = ids.map((id) => ({ id, title: 't' }))
    expect(await inboxApi.fileToPassage(items)).toHaveLength(600)
    expect(await inboxApi.fileToDocuments('f1', items)).toHaveLength(600)
    expect(sizes((i: { json: { items: unknown[] } }) => i.json.items.length)).toEqual([500, 100, 500, 100])

    net.apiRequest.mockReset()
    net.apiRequest.mockImplementation(async (path: string) => ({ titles: Object.fromEntries(path.split('ids=')[1].split(',').map((id) => [id, `T${id}`])) }))
    expect(Object.keys(await inboxApi.suggestedTitles(ids))).toHaveLength(600)
    expect(net.apiRequest.mock.calls.map(([p]) => (p as string).split('ids=')[1].split(',').length)).toEqual([500, 100])
  })

  it('refreshes the folder tree and tags as well as the listings after filing to Documents', async () => {
    net.apiRequest.mockResolvedValue({ results: [] })
    await inboxApi.fileToDocuments('f1', [{ id: 'a' }])
    expect(net.invalidateQueries).toHaveBeenCalledWith(['/api/document-folders/tree'])
    expect(net.invalidateQueries).toHaveBeenCalledWith(['/api/documents/tags'])
    net.invalidateQueries.mockClear()
    await inboxApi.fileToPassage([{ id: 'a', title: 't' }])
    expect(net.invalidateQueries).toHaveBeenCalledWith(['/api/document-folders/tree'])
  })

  it('asks which original hashes are known', async () => {
    net.apiRequest.mockResolvedValue({ known: ['h1'] })
    await expect(inboxApi.known(['h1', 'h2'])).resolves.toEqual(['h1'])
    expect(net.apiRequest).toHaveBeenCalledWith('/api/inbox/known', { method: 'POST', json: { hashes: ['h1', 'h2'] } })
  })
})

const vessel = vi.hoisted(() => ({ zone: 'Australia/Brisbane' }))
vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselStateSelector: (select: (s: { timeZone: string; timeZoneSource: string }) => unknown) => select({ timeZone: vessel.zone, timeZoneSource: 'fix' }),
}))
const upl = vi.hoisted(() => ({ hashFile: vi.fn(), prepareFile: vi.fn() }))
vi.mock('@/lib/inbox-upload', async (orig) => ({ ...(await orig<typeof import('@/lib/inbox-upload')>()), hashFile: upl.hashFile, prepareFile: upl.prepareFile }))

describe('InboxUploadPage', () => {
  it('refreshes the Inbox once when the batch is done, not after each file', async () => {
    const spies = {
      upload: vi.spyOn(inboxApi, 'upload').mockResolvedValue({ status: 'stored', item: {} as never }),
      known: vi.spyOn(inboxApi, 'known').mockResolvedValue([]),
      refresh: vi.spyOn(inboxApi, 'refresh').mockResolvedValue(),
      refreshCount: vi.spyOn(inboxApi, 'refreshCount').mockResolvedValue(),
    }
    upl.hashFile.mockImplementation(async (f: File) => `h-${f.name}`)
    upl.prepareFile.mockImplementation(async (f: File) => prepared(f.name))
    render(<InboxUploadPage canWrite onBack={vi.fn()} onOpenInbox={vi.fn()} />)
    const input = screen.getByTestId('inbox-file-input') as HTMLInputElement
    fireEvent.change(input, { target: { files: files('a.jpg', 'b.jpg', 'c.jpg') } })
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('3 uploaded'))
    expect(spies.upload).toHaveBeenCalledTimes(3)
    expect(spies.refresh).toHaveBeenCalledTimes(1)
    expect(spies.refreshCount).not.toHaveBeenCalled()
  })
})

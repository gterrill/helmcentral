import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { InboxUploadPage } from '@/components/passages/inbox-upload-page'
import { inboxApi, type InboxUploadMeta } from '@/lib/inbox-api'
import type { Launch } from '@/lib/inbox-launch'
import { createIdbQueueStore } from '@/lib/inbox-queue'
import type { PreparedFile } from '@/lib/inbox-upload'
import { FakeIdbFactory } from './helpers/fake-idb'

// The upload page works from the phone's waiting list (ADR 0280): picked and
// shared files both go on it, and a file leaves only when the box has answered.

const NO_META: InboxUploadMeta = { captureMs: null, bearingDeg: null, phoneLat: null, phoneLon: null, fingerprint: '', sharpness: 0, sourceSha256: '' }
const prepared = (name: string): PreparedFile => ({ blob: new Blob([name]), thumb: null, filename: name, meta: NO_META, warnings: [] })
const file = (name: string) => new File([name], name, { type: 'image/jpeg' })

vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselStateSelector: (select: (s: { timeZone: string; timeZoneSource: string }) => unknown) => select({ timeZone: 'Australia/Brisbane', timeZoneSource: 'fix' }),
}))
const upl = vi.hoisted(() => ({ hashFile: vi.fn(), prepareFile: vi.fn() }))
vi.mock('@/lib/inbox-upload', async (orig) => ({ ...(await orig<typeof import('@/lib/inbox-upload')>()), hashFile: upl.hashFile, prepareFile: upl.prepareFile }))
const launch = vi.hoisted(() => ({ next: { autoStart: false, shareError: null } as Launch }))
vi.mock('@/lib/inbox-launch', () => ({ takeLaunch: () => { const l = launch.next; launch.next = { autoStart: false, shareError: null }; return l }, requestAutoUpload: vi.fn() }))

let idb: FakeIdbFactory
const queue = () => createIdbQueueStore(idb as unknown as IDBFactory)

function mountPage() {
  return render(<InboxUploadPage canWrite onBack={vi.fn()} onOpenInbox={vi.fn()} />)
}

beforeEach(() => {
  idb = new FakeIdbFactory()
  vi.stubGlobal('indexedDB', idb)
  launch.next = { autoStart: false, shareError: null }
  upl.hashFile.mockImplementation(async (f: File) => `h-${f.name}`)
  upl.prepareFile.mockImplementation(async (f: File) => prepared(f.name))
  vi.spyOn(inboxApi, 'known').mockResolvedValue([])
  vi.spyOn(inboxApi, 'refresh').mockResolvedValue()
  vi.spyOn(inboxApi, 'refreshCount').mockResolvedValue()
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('InboxUploadPage with the waiting list', () => {
  it('puts picked files on the list, uploads them, and leaves the list empty', async () => {
    const upload = vi.spyOn(inboxApi, 'upload').mockResolvedValue({ status: 'stored', item: {} as never })
    mountPage()
    fireEvent.change(screen.getByTestId('inbox-file-input'), { target: { files: [file('a.jpg'), file('b.jpg')] } })
    await waitFor(() => expect(screen.getByText('2 uploaded')).toBeTruthy())
    expect(upload).toHaveBeenCalledTimes(2)
    expect(await queue().list()).toEqual([])
  })

  it('keeps a file the box did not take, with its error, and Upload now tries it again', async () => {
    const upload = vi.spyOn(inboxApi, 'upload')
      .mockResolvedValueOnce({ status: 'stored', item: {} as never })
      .mockRejectedValueOnce(new Error('HTTP 503'))
    mountPage()
    fireEvent.change(screen.getByTestId('inbox-file-input'), { target: { files: [file('a.jpg'), file('b.jpg')] } })

    const waiting = await screen.findByRole('region', { name: 'Waiting to upload' })
    await waitFor(() => expect(within(waiting).getByText('1 waiting to upload')).toBeTruthy())
    expect(within(waiting).getByText(/b\.jpg/)).toBeTruthy()
    expect(within(waiting).getByText(/HTTP 503/)).toBeTruthy()
    expect((await queue().list()).map((q) => [q.name, q.error])).toEqual([['b.jpg', 'HTTP 503']])

    upload.mockResolvedValue({ status: 'stored', item: {} as never })
    fireEvent.click(within(waiting).getByRole('button', { name: 'Upload now' }))
    await waitFor(async () => expect(await queue().list()).toEqual([]))
    expect(screen.queryByRole('region', { name: 'Waiting to upload' })).toBeNull()
  })

  it('starts at once when files were just shared', async () => {
    await queue().add([file('shared1.jpg'), file('shared2.jpg'), file('shared3.jpg')])
    launch.next = { autoStart: true, shareError: null }
    const upload = vi.spyOn(inboxApi, 'upload').mockResolvedValue({ status: 'stored', item: {} as never })
    mountPage()
    await waitFor(() => expect(upload).toHaveBeenCalledTimes(3))
    await waitFor(async () => expect(await queue().list()).toEqual([]))
  })

  it('does not start by itself on an ordinary visit, and offers what is waiting', async () => {
    await queue().add([file('old.jpg')])
    const upload = vi.spyOn(inboxApi, 'upload').mockResolvedValue({ status: 'stored', item: {} as never })
    mountPage()
    expect(await screen.findByText('1 waiting to upload')).toBeTruthy()
    expect(upload).not.toHaveBeenCalled()
  })

  it('removes one waiting file, or discards them all after a confirmation', async () => {
    await queue().add([file('a.jpg'), file('b.jpg'), file('c.jpg')])
    mountPage()
    const waiting = await screen.findByRole('region', { name: 'Waiting to upload' })
    fireEvent.click(within(waiting).getByRole('button', { name: 'Remove b.jpg' }))
    await waitFor(async () => expect((await queue().list()).map((q) => q.name)).toEqual(['a.jpg', 'c.jpg']))

    fireEvent.click(within(waiting).getByRole('button', { name: 'Discard all' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Discard' }))
    await waitFor(async () => expect(await queue().list()).toEqual([]))
  })

  it('says which share error brought the operator here, and sends nothing', async () => {
    launch.next = { autoStart: false, shareError: 'not-kept' }
    const upload = vi.spyOn(inboxApi, 'upload')
    mountPage()
    expect(await screen.findByRole('alert')).toHaveTextContent(/could not keep the files you shared/i)
    expect(upload).not.toHaveBeenCalled()
  })

  it('says a share that missed the app was not kept', async () => {
    launch.next = { autoStart: false, shareError: 'no-worker' }
    mountPage()
    expect(await screen.findByRole('alert')).toHaveTextContent(/did not reach the Helmcentral app/i)
  })

  it('sends nothing when the picked files cannot be put on the list, and says why', async () => {
    idb.options.failWrite = { n: 1, error: new Error('QuotaExceededError') }
    const upload = vi.spyOn(inboxApi, 'upload')
    mountPage()
    fireEvent.change(screen.getByTestId('inbox-file-input'), { target: { files: [file('a.jpg')] } })
    expect(await screen.findByRole('alert')).toHaveTextContent(/Nothing was added.*QuotaExceededError/)
    expect(upload).not.toHaveBeenCalled()
  })

  it('says so when the browser has no IndexedDB and will not upload without a list', async () => {
    vi.stubGlobal('indexedDB', undefined)
    const upload = vi.spyOn(inboxApi, 'upload')
    mountPage()
    expect(await screen.findByRole('alert')).toHaveTextContent(/cannot keep a waiting list/i)
    expect((screen.getByRole('button', { name: /Choose photos and files/ }) as HTMLButtonElement).disabled).toBe(true)
    expect(upload).not.toHaveBeenCalled()
  })
})

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { InboxWaitingBanner } from '@/components/inbox-waiting-banner'
import { createIdbQueueStore } from '@/lib/inbox-queue'
import { FakeIdbFactory } from './helpers/fake-idb'

const launch = vi.hoisted(() => ({ requestAutoUpload: vi.fn() }))
vi.mock('@/lib/inbox-launch', () => ({ requestAutoUpload: launch.requestAutoUpload, takeLaunch: vi.fn() }))

let idb: FakeIdbFactory
const queue = () => createIdbQueueStore(idb as unknown as IDBFactory)
const photo = (n: number) => new File([`p${n}`], `p${n}.jpg`, { type: 'image/jpeg' })
const pdf = new File(['%PDF'], 'r.pdf', { type: 'application/pdf' })

beforeEach(() => {
  idb = new FakeIdbFactory()
  vi.stubGlobal('indexedDB', idb)
  launch.requestAutoUpload.mockClear()
})
afterEach(() => vi.unstubAllGlobals())

describe('the waiting banner', () => {
  it('shows nothing when no file is waiting', async () => {
    const { container } = render(<InboxWaitingBanner canWrite onUploadNow={vi.fn()} />)
    await new Promise((r) => setTimeout(r, 20))
    expect(container.textContent).toBe('')
  })

  it('says how many photos are waiting', async () => {
    await queue().add(Array.from({ length: 7 }, (_, i) => photo(i)))
    render(<InboxWaitingBanner canWrite onUploadNow={vi.fn()} />)
    expect(await screen.findByText('7 photos waiting to upload')).toBeTruthy()
  })

  it('says files, not photos, when a PDF is among them, and says photo for one', async () => {
    await queue().add([photo(1), pdf])
    const { unmount } = render(<InboxWaitingBanner canWrite onUploadNow={vi.fn()} />)
    expect(await screen.findByText('2 files waiting to upload')).toBeTruthy()
    unmount()
    await queue().clear()
    await queue().add([photo(1)])
    render(<InboxWaitingBanner canWrite onUploadNow={vi.fn()} />)
    expect(await screen.findByText('1 photo waiting to upload')).toBeTruthy()
  })

  it('Upload now asks the page to start sending, and opens it', async () => {
    await queue().add([photo(1)])
    const onUploadNow = vi.fn()
    render(<InboxWaitingBanner canWrite onUploadNow={onUploadNow} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Upload now' }))
    expect(launch.requestAutoUpload).toHaveBeenCalledTimes(1)
    expect(onUploadNow).toHaveBeenCalledTimes(1)
  })

  it('Discard empties the list after a confirmation, and the banner goes', async () => {
    await queue().add([photo(1), photo(2)])
    render(<InboxWaitingBanner canWrite onUploadNow={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Discard' }))
    expect(await queue().list()).toHaveLength(2)
    fireEvent.click(await screen.findByRole('button', { name: 'Discard files' }))
    await waitFor(async () => expect(await queue().list()).toEqual([]))
    await waitFor(() => expect(screen.queryByText(/waiting to upload/)).toBeNull())
  })

  it('cannot upload for a sign-in that can only look, but still lets the files be discarded', async () => {
    await queue().add([photo(1)])
    render(<InboxWaitingBanner canWrite={false} onUploadNow={vi.fn()} />)
    expect((await screen.findByRole('button', { name: 'Upload now' }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole('button', { name: 'Discard' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('stays out of the way when the browser has no IndexedDB', async () => {
    vi.stubGlobal('indexedDB', undefined)
    const { container } = render(<InboxWaitingBanner canWrite onUploadNow={vi.fn()} />)
    await new Promise((r) => setTimeout(r, 20))
    expect(container.textContent).toBe('')
  })
})

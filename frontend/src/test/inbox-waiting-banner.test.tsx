import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { InboxWaitingBanner } from '@/components/inbox-waiting-banner'
import { resetInboxQueue } from '@/hooks/use-inbox-queue'
import { resetUploadRun, runPicked } from '@/lib/inbox-queue-runner'
import { FakeIdbFactory } from './helpers/fake-idb'
import { seedableQueue } from './helpers/seed-queue'

let idb: FakeIdbFactory
const queue = () => seedableQueue(idb)
const photo = (n: number) => new File([`p${n}`], `p${n}.jpg`, { type: 'image/jpeg' })
const pdf = new File(['%PDF'], 'r.pdf', { type: 'application/pdf' })

beforeEach(() => {
  idb = new FakeIdbFactory()
  resetInboxQueue()
  resetUploadRun()
  vi.stubGlobal('indexedDB', idb)
})
afterEach(() => vi.unstubAllGlobals())

describe('the waiting banner', () => {
  it('shows nothing when no file is waiting', async () => {
    const { container } = render(<InboxWaitingBanner canWrite onUploadNow={() => true} />)
    await new Promise((r) => setTimeout(r, 20))
    expect(container.textContent).toBe('')
  })

  it('says how many photos are waiting', async () => {
    await queue().add(Array.from({ length: 7 }, (_, i) => photo(i)))
    render(<InboxWaitingBanner canWrite onUploadNow={() => true} />)
    expect(await screen.findByText('7 photos waiting to upload')).toBeTruthy()
  })

  it('says files, not photos, when a PDF is among them, and says photo for one', async () => {
    await queue().add([photo(1), pdf])
    const { unmount } = render(<InboxWaitingBanner canWrite onUploadNow={() => true} />)
    expect(await screen.findByText('2 files waiting to upload')).toBeTruthy()
    unmount()
    await queue().clear()
    await queue().add([photo(1)])
    render(<InboxWaitingBanner canWrite onUploadNow={() => true} />)
    expect(await screen.findByText('1 photo waiting to upload')).toBeTruthy()
  })

  it('Upload now only opens the upload page, which waits for a tap', async () => {
    await queue().add([photo(1)])
    const onUploadNow = vi.fn()
    render(<InboxWaitingBanner canWrite onUploadNow={onUploadNow} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Upload now' }))
    expect(onUploadNow).toHaveBeenCalledTimes(1)
  })

  it('cannot discard while a run is going', async () => {
    await queue().add([photo(1)])
    let release!: () => void
    const gate = new Promise<void>((r) => { release = r })
    render(<InboxWaitingBanner canWrite onUploadNow={() => {}} />)
    expect((await screen.findByRole('button', { name: 'Discard' }) as HTMLButtonElement).disabled).toBe(false)
    let run!: Promise<unknown>
    act(() => {
      run = runPicked([photo(9)], { hash: async () => { await gate; return 'h' }, known: async () => [], prepare: async () => { throw new Error('unused') }, upload: async () => { throw new Error('unused') } }).catch(() => {})
    })
    await waitFor(() => expect((screen.getByRole('button', { name: 'Discard' }) as HTMLButtonElement).disabled).toBe(true))
    release()
    await act(async () => { await run })
    await waitFor(() => expect((screen.getByRole('button', { name: 'Discard' }) as HTMLButtonElement).disabled).toBe(false))
  })

  it('shows why a discard failed, and drops that message on a retry and when the list changes', async () => {
    await queue().add([photo(1)])
    render(<InboxWaitingBanner canWrite onUploadNow={() => {}} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Discard' }))
    idb.options.failWrite = { n: 1, error: new Error('QuotaExceededError') }
    fireEvent.click(await screen.findByRole('button', { name: 'Discard files' }))
    expect(await screen.findByText(/QuotaExceededError/)).toBeTruthy()

    // Trying again starts clean.
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }))
    expect(screen.queryByText(/QuotaExceededError/)).toBeNull()
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }))

    // A change to the list makes the old message stale.
    idb.options.failWrite = { n: 1, error: new Error('QuotaExceededError') }
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Discard files' }))
    expect(await screen.findByText(/QuotaExceededError/)).toBeTruthy()
    await act(async () => { await queue().add([photo(2)]); await new Promise((r) => setTimeout(r, 20)) })
    await waitFor(() => expect(screen.queryByText(/QuotaExceededError/)).toBeNull())
    expect(screen.getByText('2 photos waiting to upload')).toBeTruthy()
  })

  it('Discard empties the list after a confirmation, and the banner goes', async () => {
    await queue().add([photo(1), photo(2)])
    render(<InboxWaitingBanner canWrite onUploadNow={() => true} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Discard' }))
    expect(await queue().list()).toHaveLength(2)
    fireEvent.click(await screen.findByRole('button', { name: 'Discard files' }))
    await waitFor(async () => expect(await queue().list()).toEqual([]))
    await waitFor(() => expect(screen.queryByText(/waiting to upload/)).toBeNull())
  })

  it('cannot upload for a sign-in that can only look, but still lets the files be discarded', async () => {
    await queue().add([photo(1)])
    render(<InboxWaitingBanner canWrite={false} onUploadNow={() => true} />)
    expect((await screen.findByRole('button', { name: 'Upload now' }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole('button', { name: 'Discard' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('says so when the waiting list cannot be read, instead of showing nothing', async () => {
    vi.stubGlobal('indexedDB', undefined)
    resetInboxQueue()
    render(<InboxWaitingBanner canWrite onUploadNow={() => {}} />)
    expect(await screen.findByRole('alert')).toHaveTextContent(/cannot keep a waiting list/i)
  })
})

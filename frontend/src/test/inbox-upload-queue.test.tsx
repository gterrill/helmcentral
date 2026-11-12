import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { StrictMode, Suspense } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { InboxUploadPage } from '@/components/passages/inbox-upload-page'
import { inboxApi, type InboxUploadMeta } from '@/lib/inbox-api'
import type { Launch } from '@/lib/inbox-launch'
import { resetInboxQueue } from '@/hooks/use-inbox-queue'
import { resetUploadRun } from '@/lib/inbox-queue-runner'
import type { PreparedFile } from '@/lib/inbox-upload'
import { FakeIdbFactory } from './helpers/fake-idb'
import { seedableQueue } from './helpers/seed-queue'

// The upload page works from the phone's waiting list (ADR 0280): picked and
// shared files both go on it, and a file leaves only when the box has answered.

const NO_META: InboxUploadMeta = { captureMs: null, bearingDeg: null, phoneLat: null, phoneLon: null, fingerprint: '', sharpness: 0, sourceSha256: '' }
const prepared = (name: string): PreparedFile => ({ blob: new Blob([name]), thumb: null, filename: name, meta: NO_META, warnings: [] })
const file = (name: string) => new File([name], name, { type: 'image/jpeg' })

const vessel = vi.hoisted(() => ({ state: { timeZone: 'Australia/Brisbane', timeZoneSource: 'fix' } as { timeZone: string | null; timeZoneSource: string | null } }))
vi.mock('@/hooks/use-vessel-state', () => ({
  useVesselStateSelector: (select: (s: { timeZone: string | null; timeZoneSource: string | null }) => unknown) => select(vessel.state),
}))
const upl = vi.hoisted(() => ({ hashFile: vi.fn(), prepareFile: vi.fn() }))
vi.mock('@/lib/inbox-upload', async (orig) => ({ ...(await orig<typeof import('@/lib/inbox-upload')>()), hashFile: upl.hashFile, prepareFile: upl.prepareFile }))
const launch = vi.hoisted(() => ({ next: { shared: false, shareError: null } as Launch }))
vi.mock('@/lib/inbox-launch', () => ({
  takeLaunch: () => launch.next,
  clearLaunch: () => { launch.next = { shared: false, shareError: null } },
}))
const installed = vi.hoisted(() => ({ value: false }))
vi.mock('@/hooks/use-screen-wake-lock', () => ({ runningFromHomeScreen: () => installed.value }))

let idb: FakeIdbFactory
const queue = () => seedableQueue(idb)

function mountPage() {
  return render(<InboxUploadPage canWrite onBack={vi.fn()} onOpenInbox={vi.fn()} />)
}

beforeEach(() => {
  idb = new FakeIdbFactory()
  resetInboxQueue()
  resetUploadRun()
  vessel.state = { timeZone: 'Australia/Brisbane', timeZoneSource: 'fix' }
  vi.stubGlobal('indexedDB', idb)
  launch.next = { shared: false, shareError: null }
  installed.value = false
  upl.hashFile.mockReset()
  upl.prepareFile.mockReset()
  upl.hashFile.mockImplementation(async (f: File) => `h-${f.name}`)
  upl.prepareFile.mockImplementation(async (f: File) => prepared(f.name))
  vi.spyOn(inboxApi, 'known').mockResolvedValue([])
  vi.spyOn(inboxApi, 'refresh').mockResolvedValue()
  vi.spyOn(inboxApi, 'refreshCount').mockResolvedValue()
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('InboxUploadPage with the waiting list', () => {
  it('uploads picked files straight away, without putting them on the list', async () => {
    const upload = vi.spyOn(inboxApi, 'upload').mockResolvedValue({ status: 'stored', item: {} as never })
    mountPage()
    fireEvent.change(screen.getByTestId('inbox-file-input'), { target: { files: [file('a.jpg'), file('b.jpg')] } })
    await waitFor(() => expect(screen.getByText('2 uploaded')).toBeTruthy())
    expect(upload).toHaveBeenCalledTimes(2)
    expect(idb.rows('helmcentral-inbox-queue', 'meta')).toEqual([])
    expect(idb.rows('helmcentral-inbox-queue', 'blobs')).toEqual([])
    expect(screen.queryByRole('region', { name: 'Waiting to upload' })).toBeNull()
  })

  it('reports a picked file the box did not take, and picking it again sends it', async () => {
    const upload = vi.spyOn(inboxApi, 'upload')
      .mockResolvedValueOnce({ status: 'stored', item: {} as never })
      .mockRejectedValueOnce(new Error('HTTP 503'))
    mountPage()
    fireEvent.change(screen.getByTestId('inbox-file-input'), { target: { files: [file('a.jpg'), file('b.jpg')] } })
    expect(await screen.findByText(/b\.jpg was not sent: HTTP 503/)).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'Waiting to upload' })).toBeNull()

    upload.mockResolvedValue({ status: 'stored', item: {} as never })
    fireEvent.change(screen.getByTestId('inbox-file-input'), { target: { files: [file('b.jpg')] } })
    await waitFor(() => expect(screen.getByText('1 uploaded')).toBeTruthy())
  })

  it('lists what was just shared and sends it only when the operator taps Upload', async () => {
    await queue().add([file('shared1.jpg'), file('shared2.jpg'), file('shared3.jpg')])
    launch.next = { shared: true, shareError: null }
    const upload = vi.spyOn(inboxApi, 'upload').mockResolvedValue({ status: 'stored', item: {} as never })
    mountPage()
    const button = await screen.findByRole('button', { name: 'Upload 3 photos' })
    expect(screen.getByText('shared2.jpg')).toBeTruthy()
    await new Promise((r) => setTimeout(r, 40))
    expect(upload).not.toHaveBeenCalled()
    fireEvent.click(button)
    await waitFor(() => expect(upload).toHaveBeenCalledTimes(3))
    await waitFor(async () => expect(await queue().list()).toEqual([]))
  })

  it('does not start by itself on an ordinary visit, and offers what is waiting', async () => {
    await queue().add([file('old.jpg')])
    const upload = vi.spyOn(inboxApi, 'upload').mockResolvedValue({ status: 'stored', item: {} as never })
    mountPage()
    expect(await screen.findByText('1 waiting to upload')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Upload 1 photo' })).toBeTruthy()
    expect(upload).not.toHaveBeenCalled()
  })

  it('says how many are still waiting when a run ends with more on the list, and offers the button again', async () => {
    await queue().add([file('a.jpg')])
    let release!: () => void
    const gate = new Promise<void>((r) => { release = r })
    const upload = vi.spyOn(inboxApi, 'upload').mockImplementation(async () => { await gate; return { status: 'stored', item: {} as never } })
    mountPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Upload 1 photo' }))
    await waitFor(() => expect(upload).toHaveBeenCalledTimes(1))
    await queue().add([file('late.jpg')])
    release()
    expect(await screen.findByText('1 more waiting')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Upload 1 photo' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('says nothing about a second window sending the files shared meanwhile, only that they wait', async () => {
    await queue().add([file('a.jpg')])
    vi.stubGlobal('navigator', { ...navigator, locks: { request: async (_n: string, _o: unknown, cb: (l: unknown) => unknown) => cb(null) } })
    mountPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Upload 1 photo' }))
    const note = await screen.findByText(/Another window of Helmcentral on this phone is already uploading/)
    expect(note).toHaveTextContent(/wait for the next upload/i)
    expect(note).not.toHaveTextContent(/will go with it/i)
  })

  it('removes one waiting file, or discards them all after a confirmation', async () => {
    await queue().add([file('a.jpg'), file('b.jpg'), file('c.jpg')])
    mountPage()
    const waiting = await screen.findByRole('region', { name: 'Waiting to upload' })
    fireEvent.click(within(waiting).getByRole('button', { name: 'Remove b.jpg' }))
    await waitFor(async () => expect((await queue().list()).map((q) => q.name)).toEqual(['a.jpg', 'c.jpg']))

    fireEvent.click(within(waiting).getByRole('button', { name: 'Discard all' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Discard files' }))
    await waitFor(async () => expect(await queue().list()).toEqual([]))
  })

  it('still says why a share failed when React draws the page twice, as StrictMode does', async () => {
    launch.next = { shared: false, shareError: 'store-failed' }
    render(<StrictMode><InboxUploadPage canWrite onBack={vi.fn()} onOpenInbox={vi.fn()} /></StrictMode>)
    expect(await screen.findByRole('alert')).toHaveTextContent(/could not keep the files you shared/i)
  })

  it('still says why a share failed when React throws a first draw of the page away', async () => {
    launch.next = { shared: false, shareError: 'store-failed' }
    let ready = false
    let wake!: () => void
    const pending = new Promise<void>((r) => { wake = r }).then(() => { ready = true })
    const Holdup = () => {
      if (!ready) throw pending
      return null
    }
    render(
      <StrictMode>
        <Suspense fallback={null}>
          <InboxUploadPage canWrite onBack={vi.fn()} onOpenInbox={vi.fn()} />
          <Holdup />
        </Suspense>
      </StrictMode>,
    )
    await act(async () => { wake(); await pending })
    expect(await screen.findByRole('alert')).toHaveTextContent(/could not keep the files you shared/i)
  })

  it('says which share error brought the operator here, and sends nothing', async () => {
    launch.next = { shared: false, shareError: 'store-failed' }
    const upload = vi.spyOn(inboxApi, 'upload')
    mountPage()
    expect(await screen.findByRole('alert')).toHaveTextContent(/could not keep the files you shared/i)
    expect(upload).not.toHaveBeenCalled()
  })

  it('says a share that held no files Helmcentral can take, which is not a storage failure', async () => {
    launch.next = { shared: false, shareError: 'no-files' }
    mountPage()
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/held no files Helmcentral can take/i)
    expect(alert).not.toHaveTextContent(/could not keep/i)
  })

  it('says a share that missed the app was not kept', async () => {
    launch.next = { shared: false, shareError: 'no-worker' }
    mountPage()
    expect(await screen.findByRole('alert')).toHaveTextContent(/did not reach the Helmcentral app/i)
  })

  it('lets files be picked and uploaded where the browser has no IndexedDB at all', async () => {
    vi.stubGlobal('indexedDB', undefined)
    const upload = vi.spyOn(inboxApi, 'upload').mockResolvedValue({ status: 'stored', item: {} as never })
    mountPage()
    const choose = screen.getByRole('button', { name: /Choose photos and files/ }) as HTMLButtonElement
    expect(choose.disabled).toBe(false)
    fireEvent.change(screen.getByTestId('inbox-file-input'), { target: { files: [file('a.jpg')] } })
    await waitFor(() => expect(screen.getByText('1 uploaded')).toBeTruthy())
    expect(upload).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('says the waiting list cannot be read when the page was opened from a share', async () => {
    vi.stubGlobal('indexedDB', undefined)
    launch.next = { shared: true, shareError: null }
    mountPage()
    expect(await screen.findByRole('alert')).toHaveTextContent(/cannot keep a waiting list/i)
  })

  it('says the waiting list cannot be read in the installed app', async () => {
    vi.stubGlobal('indexedDB', undefined)
    installed.value = true
    mountPage()
    expect(await screen.findByRole('alert')).toHaveTextContent(/cannot keep a waiting list/i)
  })
})

describe('InboxUploadPage time zone gate', () => {
  it('keeps Upload and Choose off, and says why, while the boat time zone is unsettled', async () => {
    vessel.state = { timeZone: null, timeZoneSource: null }
    await queue().add([file('shared.jpg')])
    launch.next = { shared: true, shareError: null }
    const upload = vi.spyOn(inboxApi, 'upload').mockResolvedValue({ status: 'stored', item: {} as never })
    const { rerender } = mountPage()
    const button = await screen.findByRole('button', { name: 'Upload 1 photo' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    expect((screen.getByRole('button', { name: /Choose photos and files/ }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText("Getting the boat's time zone…")).toBeTruthy()

    vessel.state = { timeZone: 'Australia/Brisbane', timeZoneSource: 'fix' }
    rerender(<InboxUploadPage canWrite onBack={vi.fn()} onOpenInbox={vi.fn()} />)
    await waitFor(() => expect((screen.getByRole('button', { name: 'Upload 1 photo' }) as HTMLButtonElement).disabled).toBe(false))
    expect((screen.getByRole('button', { name: /Choose photos and files/ }) as HTMLButtonElement).disabled).toBe(false)
    expect(screen.queryByText("Getting the boat's time zone…")).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Upload 1 photo' }))
    await waitFor(() => expect(upload).toHaveBeenCalledTimes(1))
    expect(upl.prepareFile).toHaveBeenCalledWith(expect.anything(), expect.anything(), { boatTimeZone: 'Australia/Brisbane' })
  })

  it('enables both after about ten seconds anyway', async () => {
    vi.useFakeTimers()
    try {
      vessel.state = { timeZone: null, timeZoneSource: null }
      mountPage()
      const choose = () => screen.getByRole('button', { name: /Choose photos and files/ }) as HTMLButtonElement
      expect(choose().disabled).toBe(true)
      act(() => { vi.advanceTimersByTime(9_000) })
      expect(choose().disabled).toBe(true)
      act(() => { vi.advanceTimersByTime(1_500) })
      expect(choose().disabled).toBe(false)
      expect(screen.queryByText("Getting the boat's time zone…")).toBeNull()
    } finally {
      vi.useRealTimers()
    }
  })

  it('uses the zone as it is when each file is prepared, not as it was when the run began', async () => {
    vessel.state = { timeZone: null, timeZoneSource: 'fix' }
    await queue().add([file('a.jpg')])
    vi.spyOn(inboxApi, 'upload').mockResolvedValue({ status: 'stored', item: {} as never })
    let release!: () => void
    const gate = new Promise<void>((r) => { release = r })
    upl.hashFile.mockImplementation(async (f: File) => { await gate; return `h-${f.name}` })
    const { rerender } = mountPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Upload 1 photo' }))
    vessel.state = { timeZone: 'Pacific/Auckland', timeZoneSource: 'fix' }
    rerender(<InboxUploadPage canWrite onBack={vi.fn()} onOpenInbox={vi.fn()} />)
    release()
    await waitFor(() => expect(upl.prepareFile).toHaveBeenCalled())
    expect(upl.prepareFile).toHaveBeenCalledWith(expect.anything(), expect.anything(), { boatTimeZone: 'Pacific/Auckland' })
  })

  it('sends without a zone once the boat says it has none', async () => {
    vessel.state = { timeZone: 'UTC', timeZoneSource: 'none' }
    await queue().add([file('shared.jpg')])
    const upload = vi.spyOn(inboxApi, 'upload').mockResolvedValue({ status: 'stored', item: {} as never })
    mountPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Upload 1 photo' }))
    await waitFor(() => expect(upload).toHaveBeenCalledTimes(1))
    expect(upl.prepareFile).toHaveBeenCalledWith(expect.anything(), expect.anything(), { boatTimeZone: undefined })
  })
})

describe('InboxUploadPage shares', () => {
  it('shows a run that is going to a page that comes back, and keeps the buttons off until it ends', async () => {
    await queue().add([file('a.jpg'), file('b.jpg')])
    let release!: () => void
    const gate = new Promise<void>((r) => { release = r })
    const upload = vi.spyOn(inboxApi, 'upload').mockImplementation(async () => { await gate; return { status: 'stored', item: {} as never } })
    const first = mountPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Upload 2 photos' }))
    await waitFor(() => expect(upload).toHaveBeenCalledTimes(1))
    first.unmount()

    mountPage()
    const waiting = await screen.findByRole('region', { name: 'Waiting to upload' })
    expect(await screen.findByText(/Sending 1 of 2/)).toBeTruthy()
    expect((within(waiting).getByRole('button', { name: 'Upload 2 photos' }) as HTMLButtonElement).disabled).toBe(true)
    expect((within(waiting).getByRole('button', { name: 'Discard all' }) as HTMLButtonElement).disabled).toBe(true)
    expect((within(waiting).getByRole('button', { name: 'Remove a.jpg' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.queryByText(/Another window/)).toBeNull()

    release()
    await waitFor(async () => expect(await queue().list()).toEqual([]))
    await waitFor(() => expect(screen.getByText('2 uploaded')).toBeTruthy())
  })

  it('says how many went, and that the rest still wait, when the list fails part way', async () => {
    await queue().add([file('a.jpg'), file('b.jpg'), file('c.jpg')])
    vi.spyOn(inboxApi, 'upload').mockResolvedValue({ status: 'stored', item: {} as never })
    idb.options.failWrite = { n: 3, error: new Error('QuotaExceededError') }
    mountPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Upload 3 photos' }))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(/^2 of 3 were stored or already held before this stopped: QuotaExceededError\. .*still waiting/)
    expect(alert).not.toHaveTextContent('Nothing was sent')
  })
})

describe('InboxUploadPage progress', () => {
  it('redraws the progress line about four times a second, however fast files finish', async () => {
    await queue().add(Array.from({ length: 12 }, (_, i) => file(`p${i}.jpg`)))
    vi.spyOn(inboxApi, 'upload').mockResolvedValue({ status: 'stored', item: {} as never })
    const seen = new Set<string>()
    const watch = new MutationObserver(() => {
      for (const el of document.querySelectorAll('[role="status"]')) {
        const m = el.textContent?.match(/(Sending|Checking) \d+ of 12/)
        if (m) seen.add(m[0])
      }
    })
    watch.observe(document.body, { subtree: true, childList: true, characterData: true })
    mountPage()
    fireEvent.click(await screen.findByRole('button', { name: 'Upload 12 photos' }))
    expect(await screen.findByText('12 uploaded')).toBeTruthy()
    watch.disconnect()
    // Twelve files finish in a few milliseconds; unthrottled that is a draw for each.
    expect(seen.size).toBeLessThanOrEqual(3)
  })
})

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { PassageVideoDialog, type PassageVideoSource } from '@/components/passages/passage-video-dialog'
import { PassageVideoError } from '@/lib/passage-video/errors'
import type { PassageVideoInput } from '@/lib/passage-video/setup'

const rendering = vi.hoisted(() => ({
  renderPassageVideo: vi.fn(),
}))
vi.mock('@/lib/passage-video/render', () => rendering)

const saved = vi.hoisted(() => ({ saveBlob: vi.fn() }))
vi.mock('@/lib/save-blob', () => saved)

const source = (): PassageVideoSource => ({
  passages: [], shots: [], signs: [], hull: null, threeD: true, satellite: false, isDarkTheme: false, lengthS: 15,
  summary: { title: 'Fairey Reef to Blue Pearl Bay', dates: 'D', stats: [], start: Date.UTC(2026, 9, 24, 3, 0) / 1000, zone: { o: 10, l: 'UTC+10' } },
})

type Run = { input: PassageVideoInput; options: { signal: AbortSignal; onProgress: (p: { done: number; total: number }) => void }; resolve: (b: Blob) => void; reject: (e: unknown) => void }
let runs: Run[] = []

beforeEach(() => {
  runs = []
  rendering.renderPassageVideo.mockReset()
  rendering.renderPassageVideo.mockImplementation((input: PassageVideoInput, options: Run['options']) => new Promise<Blob>((resolve, reject) => {
    runs.push({ input, options, resolve, reject })
  }))
  saved.saveBlob.mockReset()
})
afterEach(() => { vi.unstubAllGlobals() })

const open = (onClose = vi.fn()) => {
  const view = render(<PassageVideoDialog open onClose={onClose} source={source} />)
  return { onClose, ...view }
}
const start = async () => {
  fireEvent.click(screen.getByRole('button', { name: 'Make video' }))
  await waitFor(() => expect(runs).toHaveLength(1))
  return runs[0]
}

describe('PassageVideoDialog', () => {
  it('offers landscape and portrait, landscape to begin with', () => {
    open()
    expect(screen.getByRole('button', { name: /Landscape/ })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: /Portrait/ })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: /1920 × 1080/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /1080 × 1920/ })).toBeInTheDocument()
  })

  it('says it is made in this browser, takes a few minutes, and is not kept on the boat', () => {
    open()
    expect(screen.getByText(/made in this browser/i)).toBeInTheDocument()
    expect(screen.getByText(/Nothing is stored on the boat/i)).toBeInTheDocument()
  })

  it('makes the video of the replay as it is set, in the shape chosen', async () => {
    open()
    fireEvent.click(screen.getByRole('button', { name: /Portrait/ }))
    const run = await start()
    expect(run.input).toMatchObject({ orientation: 'portrait', lengthS: 15, threeD: true })
    expect(run.input.summary.title).toBe('Fairey Reef to Blue Pearl Bay')
  })

  it('shows a progress bar and the frame it has got to, with Cancel', async () => {
    open()
    const run = await start()
    act(() => run.options.onProgress({ done: 120, total: 520 }))
    const bar = screen.getByRole('progressbar', { name: 'Making the video' })
    expect(bar).toHaveAttribute('aria-valuenow', '120')
    expect(bar).toHaveAttribute('aria-valuemax', '520')
    expect(screen.getByText('Frame 120 of 520')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
  })

  it('stops the work when Cancel is pressed, and goes back to the choice', async () => {
    open()
    const run = await start()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(run.options.signal.aborted).toBe(true)
    expect(screen.getByRole('button', { name: 'Make video' })).toBeInTheDocument()
    // The render answering later, as cancelled, is not a fault.
    await act(async () => { run.reject(new PassageVideoError('cancelled', 'The video was cancelled.')) })
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('does not stop the work for Escape, a click outside or the corner X, only for Cancel', async () => {
    const { onClose } = open()
    const run = await start()
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    fireEvent.pointerDown(document.body)
    fireEvent.click(document.body)
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    await act(async () => {})
    expect(run.options.signal.aborted).toBe(false)
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('progressbar', { name: 'Making the video' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(run.options.signal.aborted).toBe(true)
  })

  it('closes the box, and stops nothing, when it is not making a video', () => {
    const { onClose } = open()
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    expect(onClose).toHaveBeenCalled()
  })

  it('stops the work when the page is left', async () => {
    const { unmount } = open()
    const run = await start()
    unmount()
    expect(run.options.signal.aborted).toBe(true)
  })

  it('offers the file to download, named for the passage and the day', async () => {
    open()
    const run = await start()
    await act(async () => { run.resolve(new Blob(['x'.repeat(2048)], { type: 'video/mp4' })) })
    expect(screen.getByText('The video is ready.')).toBeInTheDocument()
    expect(screen.getByText(/fairey-reef-to-blue-pearl-bay-2026-10-24\.mp4/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Download' }))
    expect(saved.saveBlob).toHaveBeenCalledWith(expect.any(Blob), 'fairey-reef-to-blue-pearl-bay-2026-10-24.mp4')
  })

  it('offers Share only where the device can share a file', async () => {
    open()
    const run = await start()
    await act(async () => { run.resolve(new Blob(['x'], { type: 'video/mp4' })) })
    expect(screen.queryByRole('button', { name: 'Share' })).toBeNull()
  })

  it('shares the file on a device that can', async () => {
    const share = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, canShare: () => true, share })
    open()
    const run = await start()
    await act(async () => { run.resolve(new Blob(['x'], { type: 'video/mp4' })) })
    fireEvent.click(screen.getByRole('button', { name: 'Share' }))
    await waitFor(() => expect(share).toHaveBeenCalled())
    const arg = share.mock.calls[0][0] as { files: File[] }
    expect(arg.files[0].name).toBe('fairey-reef-to-blue-pearl-bay-2026-10-24.mp4')
    expect(arg.files[0].type).toBe('video/mp4')
    // Download is still there.
    expect(screen.getByRole('button', { name: 'Download' })).toBeInTheDocument()
  })

  it('says why it stopped, in the words it was given, and offers to try again', async () => {
    open()
    const run = await start()
    await act(async () => { run.reject(new PassageVideoError('unsupported', "This browser can't make H.264 video.")) })
    expect(screen.getByRole('alert')).toHaveTextContent("This browser can't make H.264 video.")
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(screen.getByRole('button', { name: 'Make video' })).toBeInTheDocument()
  })

  it('says the satellite imagery did not load, when that is what stopped it', async () => {
    open()
    const run = await start()
    const message = "The satellite imagery didn't load, so the boat may be offline (HTTP 502). The video wasn't finished."
    await act(async () => { run.reject(new PassageVideoError('source', message)) })
    expect(screen.getByRole('alert')).toHaveTextContent(message)
  })

  it('does not show a finished video that was cancelled before it came back', async () => {
    open()
    const run = await start()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await act(async () => { run.resolve(new Blob(['x'], { type: 'video/mp4' })) })
    expect(screen.queryByText('The video is ready.')).toBeNull()
  })
})

import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { InboxBannerSlot } from '@/components/inbox-banner-slot'
import { resetInboxQueue } from '@/hooks/use-inbox-queue'
import { FakeIdbFactory } from './helpers/fake-idb'
import { seedableQueue } from './helpers/seed-queue'

// Shares land only in the installed app, so only the installed app loads the
// waiting banner or opens the waiting list's database (ADR 0280).

const installed = vi.hoisted(() => {
  const state = { value: false, loaded: vi.fn(), rendered: vi.fn(), asked: vi.fn() }
  state.asked.mockImplementation(() => state.value)
  return state
})
vi.mock('@/hooks/use-screen-wake-lock', () => ({ runningFromHomeScreen: installed.asked }))
vi.mock('@/components/inbox-waiting-banner', async () => {
  installed.loaded()
  const actual = await vi.importActual<typeof import('@/components/inbox-waiting-banner')>('@/components/inbox-waiting-banner')
  return {
    InboxWaitingBanner: (props: Parameters<typeof actual.InboxWaitingBanner>[0]) => {
      installed.rendered()
      return actual.InboxWaitingBanner(props)
    },
  }
})

let idb: FakeIdbFactory

beforeEach(() => {
  idb = new FakeIdbFactory()
  resetInboxQueue()
  installed.loaded.mockClear()
  vi.stubGlobal('indexedDB', idb)
})

describe('the waiting banner slot', () => {
  it('loads nothing and opens no database in a browser tab or on a kiosk', async () => {
    installed.value = false
    await seedableQueue(idb).add([new File(['x'], 'x.jpg', { type: 'image/jpeg' })])
    idb.opened.length = 0
    const { container } = render(<InboxBannerSlot canWrite onUploadNow={() => true} />)
    await new Promise((r) => setTimeout(r, 30))
    expect(container.textContent).toBe('')
    expect(installed.loaded).not.toHaveBeenCalled()
    expect(idb.opened).toEqual([])
  })

  it('shows the banner in the installed app', async () => {
    installed.value = true
    await seedableQueue(idb).add([new File(['x'], 'x.jpg', { type: 'image/jpeg' })])
    render(<InboxBannerSlot canWrite onUploadNow={() => {}} />)
    expect(await screen.findByText('1 photo waiting to upload')).toBeTruthy()
  })

  it('does not draw again when its parent does, with the same props', async () => {
    installed.value = true
    await seedableQueue(idb).add([new File(['x'], 'x.jpg', { type: 'image/jpeg' })])
    const onUploadNow = () => {}
    const { rerender } = render(<InboxBannerSlot canWrite onUploadNow={onUploadNow} />)
    await screen.findByText('1 photo waiting to upload')
    const drawn = installed.rendered.mock.calls.length
    rerender(<InboxBannerSlot canWrite onUploadNow={onUploadNow} />)
    rerender(<InboxBannerSlot canWrite onUploadNow={onUploadNow} />)
    expect(installed.rendered.mock.calls.length).toBe(drawn)
  })

  it('asks the installed-app question once, not on every draw', async () => {
    installed.value = true
    installed.asked.mockClear()
    const { rerender } = render(<InboxBannerSlot canWrite onUploadNow={() => {}} />)
    rerender(<InboxBannerSlot canWrite={false} onUploadNow={() => {}} />)
    rerender(<InboxBannerSlot canWrite onUploadNow={() => {}} />)
    expect(installed.asked).toHaveBeenCalledTimes(1)
  })
})

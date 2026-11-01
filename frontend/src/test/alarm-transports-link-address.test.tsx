import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AlarmTransportsForm } from '@/components/alarm-transports-form'
import { AlarmTransportsProvider, useAlarmTransportsFormContext } from '@/components/settings/alarm-transports-context'
import { resetQueryStore } from '@/lib/api-query'
import { clearPendingSetupFocus, requestSetupFocus } from '@/lib/setup-status'

// The address for links: where Helmcentral is reached from off the boat, so a
// notification can carry a link to a watch run. Empty by default.

function SaveProbe() {
  const { save, dirty } = useAlarmTransportsFormContext()
  return <button type="button" onClick={() => { void save() }}>{dirty ? 'save-dirty' : 'save-clean'}</button>
}

const posts: unknown[] = []

function stub(linkAddress: string) {
  posts.length = 0
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const ok = (data: unknown) => ({ ok: true, status: 200, json: async () => data })
    if (init?.method === 'POST' && String(url).endsWith('/api/alarm-transports')) {
      posts.push(JSON.parse(String(init.body)))
      return ok({})
    }
    if (String(url).endsWith('/api/alarm-transports')) {
      return ok({
        ntfy: { enabled: false, server: '', topic: '' }, smtp: { enabled: false, host: '', port: 587, username: '', from: '', to: [] },
        webhook: { enabled: false, url: '' }, signalk: { enabled: false }, webpush: { enabled: false },
        watchdog: { stream_silence_seconds: 0, heartbeat_minutes: 0 }, link_address: linkAddress,
      })
    }
    if (String(url).includes('/api/settings/secrets')) return ok({})
    return { ok: false, status: 404, json: async () => ({}) }
  }))
}

afterEach(() => {
  vi.unstubAllGlobals()
  resetQueryStore()
})

describe('the address for links', () => {
  it('is empty until the operator sets it, and saves with the other notification settings', async () => {
    stub('')
    render(<AlarmTransportsProvider><AlarmTransportsForm /><SaveProbe /></AlarmTransportsProvider>)
    const field = await screen.findByLabelText('Address for links')
    expect(field).toHaveValue('')
    expect(screen.getByText(/only used to put a link in notifications about a watch/i)).toBeInTheDocument()
    fireEvent.change(field, { target: { value: 'http://boat.local:9091' } })
    expect(screen.getByRole('button', { name: 'save-dirty' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'save-dirty' }))
    await waitFor(() => expect(posts).toHaveLength(1))
    expect(posts[0]).toMatchObject({ link_address: 'http://boat.local:9091' })
  })

  it('shows the saved address', async () => {
    stub('https://boat.example')
    render(<AlarmTransportsProvider><AlarmTransportsForm /></AlarmTransportsProvider>)
    expect(await screen.findByLabelText('Address for links')).toHaveValue('https://boat.example')
  })
})

describe('the notifications anchor', () => {
  it('scrolls to and focuses the section once drawn when a link asked for it', async () => {
    stub('')
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    requestSetupFocus('notifications')
    render(<AlarmTransportsProvider><AlarmTransportsForm /></AlarmTransportsProvider>)
    await screen.findByLabelText('Address for links')
    await waitFor(() => expect(scroll).toHaveBeenCalledTimes(1))
    expect(scroll.mock.contexts[0]).toHaveAttribute('id', 'notifications')
    expect(document.getElementById('notifications')).toHaveFocus()
  })

  it('acts on a request made after the form is already showing', async () => {
    stub('')
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    render(<AlarmTransportsProvider><AlarmTransportsForm /></AlarmTransportsProvider>)
    await screen.findByLabelText('Address for links')
    act(() => requestSetupFocus('notifications'))
    await waitFor(() => expect(scroll).toHaveBeenCalledTimes(1))
    clearPendingSetupFocus()
  })

  it('does not scroll when nothing asked', async () => {
    stub('')
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    render(<AlarmTransportsProvider><AlarmTransportsForm /></AlarmTransportsProvider>)
    await screen.findByLabelText('Address for links')
    expect(scroll).not.toHaveBeenCalled()
  })
})

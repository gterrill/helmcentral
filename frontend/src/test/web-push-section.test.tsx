import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { WebPushSection } from '@/components/web-push-section'
import type { WebPushSupport } from '@/hooks/use-web-push'

const enableOnThisDevice = vi.fn().mockResolvedValue(undefined)
const disableOnThisDevice = vi.fn().mockResolvedValue(undefined)

function mockWebPush(overrides: Partial<{
  support: WebPushSupport
  subscribed: boolean
  deviceCount: number
  error: string | null
}> = {}) {
  vi.doMock('@/hooks/use-web-push', () => ({
    useWebPush: () => ({
      support: overrides.support ?? { kind: 'ok' },
      permission: 'default',
      subscribed: overrides.subscribed ?? false,
      deviceCount: overrides.deviceCount ?? 0,
      busy: false,
      error: overrides.error ?? null,
      enableOnThisDevice,
      disableOnThisDevice,
    }),
  }))
}

async function renderSection(overrides: Parameters<typeof mockWebPush>[0] = {}) {
  vi.resetModules()
  mockWebPush(overrides)
  const { WebPushSection: Section } = await import('@/components/web-push-section')
  render(<Section enabled onEnabledChange={vi.fn()} />)
}

afterEach(() => {
  vi.clearAllMocks()
  vi.resetModules()
})

describe('WebPushSection', () => {
  it('exports a component', () => {
    expect(WebPushSection).toBeTruthy()
  })

  /**
   * The headline case on a boat. Helmcentral is served over plain HTTP on a LAN
   * address, so the Push API is absent entirely — the operator needs to be told
   * how to get a secure origin, not shown a toggle that silently cannot work.
   */
  it('explains the secure-context requirement and offers no Enable button', async () => {
    await renderSection({ support: { kind: 'insecure-context' } })

    expect(screen.getByText(/tailscale serve/i)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /enable on this device/i })).toBeNull()
  })

  // iOS withholds PushManager until the site is on the Home Screen, so the fix
  // is an install, not a different browser.
  it('gives Add to Home Screen instructions on an uninstalled iPhone', async () => {
    await renderSection({ support: { kind: 'ios-not-installed' } })

    expect(screen.getByText(/add to home screen/i)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /enable on this device/i })).toBeNull()
  })

  /**
   * requestPermission() on a denied site resolves instantly without prompting,
   * so an Enable button here would appear to do nothing at all.
   */
  it('explains a blocked permission and offers no Enable button', async () => {
    await renderSection({ support: { kind: 'blocked' } })

    expect(screen.getByText(/blocked/i)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /enable on this device/i })).toBeNull()
  })

  // A dead end is less useful than a working alternative.
  it('points at the other transports when the browser cannot do push', async () => {
    await renderSection({ support: { kind: 'unsupported' } })

    expect(screen.getByText(/ntfy|email/i)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /enable on this device/i })).toBeNull()
  })

  it('enables this device with the typed label', async () => {
    await renderSection({ support: { kind: 'ok' } })

    const nameInput = screen.getByLabelText(/device name/i)
    fireEvent.change(nameInput, { target: { value: 'Helm tablet' } })
    fireEvent.click(screen.getByRole('button', { name: /enable on this device/i }))

    await waitFor(() => expect(enableOnThisDevice).toHaveBeenCalledWith('Helm tablet'))
  })

  it('offers unsubscribe once this device is registered', async () => {
    await renderSection({ support: { kind: 'ok' }, subscribed: true, deviceCount: 2 })

    expect(screen.queryByRole('button', { name: /enable on this device/i })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /unsubscribe/i }))

    await waitFor(() => expect(disableOnThisDevice).toHaveBeenCalled())
  })

  // "Enabled but nothing registered" is the silently-undeliverable state the
  // backend refuses to pretend is fine, so the UI says so too.
  it('shows the device count so an empty transport is visible', async () => {
    await renderSection({ support: { kind: 'ok' }, deviceCount: 0 })

    expect(screen.getByText(/no devices/i)).toBeTruthy()
  })

  it('surfaces an error from the hook', async () => {
    await renderSection({ support: { kind: 'ok' }, error: 'Notifications were not allowed for this site.' })

    expect(screen.getByText(/not allowed/i)).toBeTruthy()
  })
})

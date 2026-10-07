/**
 * The post-update message has to reach a mounted <Toaster>. sonner drops a
 * toast raised before its Toaster subscribes, and App shows "Checking
 * sign-in…" (no Toaster) on first render after the reload, so UpdatedToast is
 * rendered beside the Toaster rather than called from App's top level. This
 * runs against the real sonner, which the hook's own unit test mocks out.
 */
import { afterEach, describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'

import { Toaster } from '@/components/ui/sonner'
import { UpdatedToast } from '@/components/updated-toast'
import { UPDATED_TO_KEY } from '@/hooks/use-version-reload'

describe('UpdatedToast', () => {
  afterEach(() => sessionStorage.clear())

  it('shows the update message when rendered beside the Toaster', async () => {
    sessionStorage.setItem(UPDATED_TO_KEY, JSON.stringify({ from: 'v0.41.0', to: 'v0.42.0' }))

    render(
      <>
        <Toaster />
        <UpdatedToast />
      </>,
    )

    expect(await screen.findByText('Helmcentral updated to v0.42.0')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Release notes' })).toBeInTheDocument()
  })
})

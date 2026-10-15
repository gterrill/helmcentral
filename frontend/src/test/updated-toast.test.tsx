/**
 * The post-update message has to reach a mounted <Toaster>. sonner drops a
 * toast raised before its Toaster subscribes, so UpdatedToast is rendered
 * beside the Toaster rather than called from App's top level. This runs
 * against the real sonner, which the hook's own unit test mocks out.
 */
import { afterEach, describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'

import { Toaster } from '@/components/ui/sonner'
import { UpdatedToast } from '@/components/updated-toast'
import { SEEN_VERSION_KEY } from '@/lib/seen-version'

describe('UpdatedToast', () => {
  afterEach(() => localStorage.clear())

  it('shows the update message when rendered beside the Toaster', async () => {
    localStorage.setItem(SEEN_VERSION_KEY, 'v0.41.0')

    render(
      <>
        <Toaster />
        <UpdatedToast current={{ version: 'v0.42.0', revision: 'aaa' }} />
      </>,
    )

    expect(await screen.findByText('Helmcentral updated to v0.42.0')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Release notes' })).toBeInTheDocument()
  })
})

/**
 * The sidebar footer is the one place in the UI that answers "what am I
 * actually running?". The version has to come from the running backend
 * (`/api/health`, whose `version`/`revision` are stamped in at build time by
 * the Dockerfile's ldflags) rather than from frontend/package.json, which is
 * not part of the release versioning at all.
 */
import { describe, it, expect, vi, afterEach } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { resetAppVersionProbe } from '@/hooks/use-app-version'
import { SidebarVersion } from '@/components/sidebar-version'
import { GC_TIME_MS } from '@/lib/api-query'
import { jsonResponse } from '@/test/helpers/stub-api'

const stubHealth = (payload: unknown, ok = true) => {
  const fetchMock = vi.fn(async () => jsonResponse(payload, ok ? 200 : 500))
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('SidebarVersion', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows the version the running backend reports', async () => {
    stubHealth({ status: 'ok', version: 'v0.17.0', revision: 'deadbeef' })

    render(<SidebarVersion />)

    expect(await screen.findByText('v0.17.0')).toBeInTheDocument()
  })

  it('reads it from the /api/health endpoint', async () => {
    const fetchMock = stubHealth({ status: 'ok', version: 'v0.17.0', revision: 'deadbeef' })

    render(<SidebarVersion />)
    await screen.findByText('v0.17.0')

    expect(fetchMock).toHaveBeenCalledWith('/api/health', expect.anything())
  })

  it('reads the stamp once per page load, even when the sidebar mounts again after the store has collected', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      const fetchMock = stubHealth({ status: 'ok', version: 'v0.17.0', revision: 'deadbeef' })

      const first = render(<SidebarVersion />)
      await screen.findByText('v0.17.0')
      first.unmount()
      await act(async () => { await vi.advanceTimersByTimeAsync(GC_TIME_MS + 1000) })
      stubHealth({ status: 'ok', version: 'v0.18.0', revision: 'cafef00d' })
      render(<SidebarVersion />)

      expect(await screen.findByText('v0.17.0')).toBeInTheDocument()
      expect(fetchMock).toHaveBeenCalledTimes(1)
    } finally {
      vi.useRealTimers()
    }
  })

  it('tries again on the next mount after a failed read', async () => {
    stubHealth({}, false)
    const first = render(<SidebarVersion />)
    await screen.findByText('version unavailable')
    first.unmount()

    stubHealth({ status: 'ok', version: 'v0.17.0', revision: 'deadbeef' })
    render(<SidebarVersion />)
    expect(await screen.findByText('v0.17.0')).toBeInTheDocument()
  })

  it('carries the build revision for support, without spending sidebar width on it', async () => {
    stubHealth({ status: 'ok', version: 'v0.17.0', revision: 'deadbeef' })

    render(<SidebarVersion />)

    await waitFor(() =>
      expect(screen.getByTestId('sidebar-version')).toHaveAttribute(
        'title',
        'Helmcentral v0.17.0 (deadbeef)',
      ),
    )
  })

  it('renders a dev build as reported rather than dressing it up as a release', async () => {
    stubHealth({ status: 'ok', version: 'dev', revision: 'unknown' })

    render(<SidebarVersion />)

    expect(await screen.findByText('dev')).toBeInTheDocument()
  })

  it('says the version is unavailable rather than inventing one when the probe fails', async () => {
    stubHealth({}, false)

    render(<SidebarVersion />)

    expect(await screen.findByText('version unavailable')).toBeInTheDocument()
  })

  it('links a release version to its release notes, opening in a new tab', async () => {
    stubHealth({ status: 'ok', version: 'v0.42.0', revision: 'deadbeef' })

    render(<SidebarVersion />)

    const link = await screen.findByRole('link', { name: 'Release notes for v0.42.0' })
    expect(link).toHaveAttribute('href', 'https://github.com/gterrill/helmcentral/releases/tag/v0.42.0')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(link).toHaveAttribute('data-testid', 'sidebar-version')
    expect(link).toHaveAttribute('title', 'Helmcentral v0.42.0 (deadbeef)')
    expect(link).toHaveTextContent('v0.42.0')
  })

  it('leaves non-release versions as plain text', async () => {
    stubHealth({ status: 'ok', version: 'v0.42.0-3-gabc', revision: 'deadbeef' })

    render(<SidebarVersion />)

    await screen.findByText('v0.42.0-3-gabc')
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('does not link a dev build or the unavailable state', async () => {
    stubHealth({ status: 'ok', version: 'dev', revision: 'unknown' })
    const { unmount } = render(<SidebarVersion />)
    await screen.findByText('dev')
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
    unmount()

    // The first probe's answer stands for the page load; start a new one.
    resetAppVersionProbe()
    stubHealth({}, false)
    render(<SidebarVersion />)
    await screen.findByText('version unavailable')
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('renders nothing until the probe answers, so no placeholder flashes', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))

    const { container } = render(<SidebarVersion />)

    expect(container).toBeEmptyDOMElement()
  })

  it('says Update available and offers Reload until the page is reloaded', async () => {
    stubHealth({ status: 'ok', version: 'v0.42.0', revision: 'deadbeef' })
    const onReload = vi.fn()

    render(<SidebarVersion updateAvailable onReload={onReload} />)

    const button = await screen.findByRole('button', { name: 'Update available · Reload' })
    fireEvent.click(button)
    expect(onReload).toHaveBeenCalledTimes(1)
    expect(screen.getByTestId('sidebar-version')).toBeInTheDocument()
  })

  it('shows the plain version when no update is available', async () => {
    stubHealth({ status: 'ok', version: 'v0.42.0', revision: 'deadbeef' })
    render(<SidebarVersion updateAvailable={false} onReload={vi.fn()} />)
    await screen.findByText('v0.42.0')
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})

/**
 * The sidebar footer is the one place in the UI that answers "what am I
 * actually running?". The version has to come from the running backend
 * (`/api/health`, whose `version`/`revision` are stamped in at build time by
 * the Dockerfile's ldflags) rather than from frontend/package.json, which is
 * not part of the release versioning at all.
 */
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { SidebarVersion } from '@/components/sidebar-version'

const stubHealth = (payload: unknown, ok = true) => {
  const fetchMock = vi.fn(async () => ({ ok, json: async () => payload }))
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

  it('reads it from the relative /api/health endpoint', async () => {
    const fetchMock = stubHealth({ status: 'ok', version: 'v0.17.0', revision: 'deadbeef' })

    render(<SidebarVersion />)
    await screen.findByText('v0.17.0')

    expect(fetchMock).toHaveBeenCalledWith('/api/health')
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
})

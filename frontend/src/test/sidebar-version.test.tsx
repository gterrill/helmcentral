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

  it('renders nothing until the probe answers, so no placeholder flashes', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))

    const { container } = render(<SidebarVersion />)

    expect(container).toBeEmptyDOMElement()
  })
})

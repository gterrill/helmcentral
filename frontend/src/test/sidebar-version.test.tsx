/**
 * The sidebar footer is the one place in the UI that answers "what am I
 * actually running?". It shows the build the page itself carries (the same
 * source the update toasts compare against), never a second health probe.
 */
import { describe, it, expect, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { SidebarVersion } from '@/components/sidebar-version'

const build = (version: string, revision = 'deadbeef') => ({ version, revision })

describe('SidebarVersion', () => {
  it('shows the version it is given', () => {
    render(<SidebarVersion build={build('v0.17.0')} />)
    expect(screen.getByText('v0.17.0')).toBeInTheDocument()
  })

  it('carries the build revision for support, without spending sidebar width on it', () => {
    render(<SidebarVersion build={build('v0.17.0')} />)
    expect(screen.getByTestId('sidebar-version')).toHaveAttribute('title', 'Helmcentral v0.17.0 (deadbeef)')
  })

  it('renders a dev build as reported rather than dressing it up as a release', () => {
    render(<SidebarVersion build={build('dev', 'unknown')} />)
    expect(screen.getByText('dev')).toBeInTheDocument()
  })

  it('says the version is unavailable rather than inventing one when the read failed', () => {
    render(<SidebarVersion build={null} error="HTTP 500" />)
    expect(screen.getByText('version unavailable')).toBeInTheDocument()
    expect(screen.getByTestId('sidebar-version')).toHaveAttribute('title', 'Helmcentral version unavailable: HTTP 500')
  })

  it('renders nothing until there is a version or an error, so no placeholder flashes', () => {
    const { container } = render(<SidebarVersion build={null} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('links a release version to its release notes, opening in a new tab', () => {
    render(<SidebarVersion build={build('v0.42.0')} />)
    const link = screen.getByRole('link', { name: 'Release notes for v0.42.0' })
    expect(link).toHaveAttribute('href', 'https://github.com/gterrill/helmcentral/releases/tag/v0.42.0')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(link).toHaveAttribute('data-testid', 'sidebar-version')
    expect(link).toHaveTextContent('v0.42.0')
  })

  it('leaves non-release versions, dev and the unavailable state as plain text', () => {
    const { rerender } = render(<SidebarVersion build={build('v0.42.0-3-gabc')} />)
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
    rerender(<SidebarVersion build={build('dev', 'unknown')} />)
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
    rerender(<SidebarVersion build={null} error="x" />)
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('says Update available and offers Reload until the page is reloaded', () => {
    const onReload = vi.fn()
    render(<SidebarVersion build={build('v0.42.0')} updateAvailable onReload={onReload} />)
    fireEvent.click(screen.getByRole('button', { name: 'Update available · Reload' }))
    expect(onReload).toHaveBeenCalledTimes(1)
  })

  it('shows the plain version when no update is available', () => {
    render(<SidebarVersion build={build('v0.42.0')} updateAvailable={false} onReload={vi.fn()} />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})

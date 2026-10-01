import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { FormSection, SettingsLayout } from '@/components/patterns'

describe('SettingsLayout', () => {
  it('renders the page heading, an optional description and its sections', () => {
    render(
      <SettingsLayout title="General" description="Units and defaults.">
        <FormSection title="Units"><p>units body</p></FormSection>
        <FormSection title="Display"><p>display body</p></FormSection>
      </SettingsLayout>,
    )
    expect(screen.getByRole('heading', { level: 1, name: 'General' })).toBeInTheDocument()
    expect(screen.getByText('Units and defaults.')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'Units' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 2, name: 'Display' })).toBeInTheDocument()
    expect(screen.getByText('units body')).toBeInTheDocument()
  })

  it('renders tools as a final section headed Tools, and omits it when absent', () => {
    const { rerender } = render(
      <SettingsLayout title="Logs" tools={<button type="button">Reset</button>}>
        <FormSection title="Body"><p>x</p></FormSection>
      </SettingsLayout>,
    )
    const headings = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)
    expect(headings).toEqual(['Body', 'Tools'])
    expect(screen.getByRole('button', { name: 'Reset' })).toBeInTheDocument()

    rerender(
      <SettingsLayout title="Logs">
        <FormSection title="Body"><p>x</p></FormSection>
      </SettingsLayout>,
    )
    expect(screen.queryByRole('heading', { name: 'Tools' })).not.toBeInTheDocument()
  })

  it('is a single narrow centred column that can shrink', () => {
    const { container } = render(
      <SettingsLayout title="General"><FormSection title="A"><p>x</p></FormSection></SettingsLayout>,
    )
    const root = container.firstElementChild as HTMLElement
    expect(root.className).toContain('mx-auto')
    expect(root.className).toContain('max-w-3xl')
    expect(root.className).toContain('min-w-0')
  })
})

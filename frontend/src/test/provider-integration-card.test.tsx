import { describe, it, expect, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { ProviderIntegrationCard } from '@/components/settings/provider-integration-card'

describe('ProviderIntegrationCard', () => {
  it('renders the already-active card with its switch checked and disabled', () => {
    render(
      <ProviderIntegrationCard
        id="bom"
        name="Bureau of Meteorology"
        description="Australian tide data"
        active
        onActivate={vi.fn()}
        onOpenSettings={vi.fn()}
      />,
    )

    const switchEl = screen.getByRole('switch', { name: /activate bureau of meteorology/i })
    expect(switchEl).toHaveAttribute('aria-disabled', 'true')
    expect(screen.getByText('Active')).toBeTruthy()
  })

  it('calls the activate callback with its id when an inactive card is switched on', () => {
    const onActivate = vi.fn()
    render(
      <ProviderIntegrationCard
        id="stormglass"
        name="Storm Glass"
        description="Paid worldwide tide data"
        active={false}
        onActivate={onActivate}
        onOpenSettings={vi.fn()}
      />,
    )

    const switchEl = screen.getByRole('switch', { name: /activate storm glass/i })
    expect(switchEl).not.toHaveAttribute('aria-disabled')
    fireEvent.click(switchEl)

    expect(onActivate).toHaveBeenCalledWith('stormglass')
  })

  it('calls onOpenSettings with its id when the Settings button is clicked, regardless of active state', () => {
    const onOpenSettings = vi.fn()
    render(
      <ProviderIntegrationCard
        id="stormglass"
        name="Storm Glass"
        description="Paid worldwide tide data"
        active={false}
        onActivate={vi.fn()}
        onOpenSettings={onOpenSettings}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /settings/i }))
    expect(onOpenSettings).toHaveBeenCalledWith('stormglass')
  })
})

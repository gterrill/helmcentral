import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

import { MarineHeader } from '@/components/marine-header'

// DESIGN.md's zero-state rule: a placeholder must not be styled as if it
// were real data. Before this fix, 'VESSEL NAME NOT SET' rendered in the
// same text-primary hero display type as an actual vessel name.

const mockUseVesselIdentity = vi.fn()

vi.mock('@/hooks/use-vessel-identity', () => ({
  useVesselIdentity: () => mockUseVesselIdentity(),
}))

describe('MarineHeader', () => {
  it('renders a real vessel name in the display treatment', () => {
    mockUseVesselIdentity.mockReturnValue({
      vesselStatus: 'At Anchor',
      boatName: 'M/V Pikorua',
      boatModel: 'Riviera 445',
    })

    render(<MarineHeader />)

    const name = screen.getByText('M/V Pikorua')
    expect(name).toHaveClass('text-primary')
  })

  it('renders an unset vessel name as a muted placeholder, not the hero display type', () => {
    mockUseVesselIdentity.mockReturnValue({
      vesselStatus: 'At Anchor',
      boatName: null,
      boatModel: 'Riviera 445',
    })

    render(<MarineHeader />)

    const name = screen.getByText('VESSEL NAME NOT SET')
    expect(name).not.toHaveClass('text-primary')
    expect(name).toHaveClass('text-muted-foreground')
  })

  it('renders an unset model as a distinguishable placeholder within the status line', () => {
    mockUseVesselIdentity.mockReturnValue({
      vesselStatus: 'At Anchor',
      boatName: 'M/V Pikorua',
      boatModel: null,
    })

    render(<MarineHeader />)

    const model = screen.getByText('MODEL NOT SET')
    expect(model).toHaveClass('italic')
  })
})

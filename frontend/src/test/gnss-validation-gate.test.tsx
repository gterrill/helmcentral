import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { useAnchorWatch } from '@/hooks/use-anchor-watch'

function Probe({ gnssCritical }: { gnssCritical: boolean }) {
  const watch = useAnchorWatch(-25.2939, 152.9103, 'anchored', 1, gnssCritical)
  return (
    <div>
      <div data-testid="anchor-state">{watch.anchorState}</div>
      <div data-testid="gnss-critical">{String(watch.gnssCritical)}</div>
    </div>
  )
}

describe('GNSS validation gate', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        active: true,
        lat: -25.2939,
        lon: 152.9103,
        radius_meters: 20,
      }),
    }))
  })

  it('surfaces a corrupt GPS fix as a diagnostic flag without forcing the drag alarm', async () => {
    render(<Probe gnssCritical />)

    await waitFor(() => {
      expect(screen.getByTestId('gnss-critical')).toHaveTextContent('true')
    })

    // Vessel is within radius of the anchor point, so the alarm itself
    // should stay 'set' - a corrupt GPS fix is surfaced separately rather
    // than being conflated with "the boat has dragged."
    expect(screen.getByTestId('anchor-state')).toHaveTextContent('set')
  })
})
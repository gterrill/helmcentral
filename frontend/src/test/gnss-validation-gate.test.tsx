import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { useAnchorWatch } from '@/hooks/use-anchor-watch'

function Probe({ positionCritical }: { positionCritical: boolean }) {
  const watch = useAnchorWatch(-25.2939, 152.9103, 'anchored', 1, positionCritical)
  return <div data-testid="anchor-state">{watch.anchorState}</div>
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

  it('forces anchor watch into critical mode when the GPS fix is corrupt', async () => {
    render(<Probe positionCritical />)

    await waitFor(() => {
      expect(screen.getByTestId('anchor-state')).toHaveTextContent('critical')
    })
  })
})
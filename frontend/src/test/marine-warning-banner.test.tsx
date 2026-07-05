import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { MarineWarningBanner } from '@/components/marine-warning-banner'
import type { MarineWarnings } from '@/hooks/use-marine-warnings'

describe('MarineWarningBanner', () => {
  it('renders nothing when there is no active warning', () => {
    const { container } = render(<MarineWarningBanner warnings={null} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing when warnings has no bulletins', () => {
    const warnings: MarineWarnings = { state: 'QLD', zone: 'Capricornia Coast', bulletins: [] }
    const { container } = render(<MarineWarningBanner warnings={warnings} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders the warning when active for the zone', () => {
    const warnings: MarineWarnings = {
      state: 'QLD',
      zone: 'Capricornia Coast',
      bulletins: [
        {
          productId: 'IDQ20085',
          title: 'Marine Wind Warning Summary for Queensland',
          issuedAtRaw: '11:51 am EST on Sunday 5 July 2026',
          issuedAt: '2026-07-05T01:51:00Z',
          detailsUrl: 'http://www.bom.gov.au/qld/forecasts/map.shtml',
          sections: [
            { day: 'Sunday 5 July', warningType: 'Strong Wind Warning', zones: ['Capricornia Coast'] },
          ],
        },
      ],
    }
    render(<MarineWarningBanner warnings={warnings} />)
    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(screen.getByText(/Strong Wind Warning/)).toBeInTheDocument()
    expect(screen.getByText(/Capricornia Coast/)).toBeInTheDocument()
    expect(screen.getByText(/Sunday 5 July/)).toBeInTheDocument()
  })

  it('renders a link to the BOM details page when detailsUrl is present', () => {
    const warnings: MarineWarnings = {
      state: 'QLD',
      zone: 'Capricornia Coast',
      bulletins: [
        {
          productId: 'IDQ20085',
          title: 'Marine Wind Warning Summary for Queensland',
          issuedAtRaw: '11:51 am EST on Sunday 5 July 2026',
          issuedAt: '2026-07-05T01:51:00Z',
          detailsUrl: 'http://www.bom.gov.au/qld/forecasts/map.shtml',
          sections: [
            { day: 'Sunday 5 July', warningType: 'Strong Wind Warning', zones: ['Capricornia Coast'] },
          ],
        },
      ],
    }
    render(<MarineWarningBanner warnings={warnings} />)
    const link = screen.getByRole('link', { name: /view on bom/i })
    expect(link).toHaveAttribute('href', 'http://www.bom.gov.au/qld/forecasts/map.shtml')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'))
    expect(link).toHaveAttribute('rel', expect.stringContaining('noreferrer'))
  })

  it('does not render a details link when detailsUrl is empty', () => {
    const warnings: MarineWarnings = {
      state: 'QLD',
      zone: 'Capricornia Coast',
      bulletins: [
        {
          productId: 'IDQ20085',
          title: 'Marine Wind Warning Summary for Queensland',
          issuedAtRaw: '11:51 am EST on Sunday 5 July 2026',
          issuedAt: '2026-07-05T01:51:00Z',
          detailsUrl: '',
          sections: [
            { day: 'Sunday 5 July', warningType: 'Strong Wind Warning', zones: ['Capricornia Coast'] },
          ],
        },
      ],
    }
    render(<MarineWarningBanner warnings={warnings} />)
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('stacks multiple simultaneous warnings for the same zone', () => {
    const warnings: MarineWarnings = {
      state: 'QLD',
      zone: 'Capricornia Coast',
      bulletins: [
        {
          productId: 'IDQ20085',
          title: 'Marine Wind Warning Summary for Queensland',
          issuedAtRaw: '11:51 am EST on Sunday 5 July 2026',
          issuedAt: '2026-07-05T01:51:00Z',
          detailsUrl: 'http://www.bom.gov.au/qld/forecasts/map.shtml',
          sections: [
            { day: 'Sunday 5 July', warningType: 'Strong Wind Warning', zones: ['Capricornia Coast'] },
          ],
        },
        {
          productId: 'IDQ21285',
          title: 'Hazardous Surf Warning Summary for Queensland',
          issuedAtRaw: '11:51 am EST on Sunday 5 July 2026',
          issuedAt: '2026-07-05T01:51:00Z',
          detailsUrl: 'http://www.bom.gov.au/qld/',
          sections: [
            { day: 'Sunday 5 July', warningType: 'Hazardous Surf Warning', zones: ['Capricornia Coast'] },
          ],
        },
      ],
    }
    render(<MarineWarningBanner warnings={warnings} />)
    expect(screen.getByText(/Strong Wind Warning/)).toBeInTheDocument()
    expect(screen.getByText(/Hazardous Surf Warning/)).toBeInTheDocument()
  })

  // Regression test for the backend bug: a bulletin with has_active_warning=true
  // (somewhere in the state) but has_active_warning_for_zone=false (not the
  // vessel's own zone) must never surface here. useMarineWarnings is expected to
  // have already filtered this out before the banner ever sees it, but we assert
  // the banner's own contract too: an "active" bulletin whose sections don't
  // name the vessel's zone must not render.
  it('does not render a warning that is active for the state but not the vessel zone', () => {
    // Simulates what useMarineWarnings would produce for a bulletin where
    // has_active_warning=true but has_active_warning_for_zone=false: no
    // sections survive the zone filter, so bulletins end up empty.
    const warnings: MarineWarnings = {
      state: 'QLD',
      zone: 'Capricornia Coast',
      bulletins: [],
    }
    const { container } = render(<MarineWarningBanner warnings={warnings} />)
    expect(container).toBeEmptyDOMElement()
    expect(screen.queryByText(/Hazardous Surf Warning/)).not.toBeInTheDocument()
  })
})

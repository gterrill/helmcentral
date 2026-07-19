import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { WindWarningNotice } from '@/components/wind-warning-notice'
import type { ForecastWarnings } from '@/hooks/use-forecast-warnings'

describe('WindWarningNotice', () => {
  it('renders nothing when there is no active warning', () => {
    const { container } = render(<WindWarningNotice warnings={null} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing when warnings has no bulletins', () => {
    const warnings: ForecastWarnings = { provider: 'bom', region: 'Capricornia Coast', bulletins: [] }
    const { container } = render(<WindWarningNotice warnings={warnings} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing when only a surf warning is active', () => {
    const warnings: ForecastWarnings = {
      provider: 'bom',
      region: 'Capricornia Coast',
      bulletins: [
        {
          id: 'IDQ21285',
          title: 'Hazardous Surf Warning Summary for Queensland',
          issuedAt: '2026-07-05T01:51:00Z',
          detailsUrl: 'http://www.bom.gov.au/qld/',
          category: 'surf',
          sections: [{ day: 'Sunday 5 July', warningType: 'Hazardous Surf Warning' }],
        },
      ],
    }
    const { container } = render(<WindWarningNotice warnings={warnings} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders the notice with a working link when a wind warning is active', () => {
    const warnings: ForecastWarnings = {
      provider: 'bom',
      region: 'Capricornia Coast',
      bulletins: [
        {
          id: 'IDQ20085',
          title: 'Marine Wind Warning Summary for Queensland',
          issuedAt: '2026-07-05T01:51:00Z',
          detailsUrl: 'http://www.bom.gov.au/qld/forecasts/map.shtml',
          category: 'wind',
          sections: [{ day: 'Sunday 5 July', warningType: 'Strong Wind Warning' }],
        },
      ],
    }
    render(<WindWarningNotice warnings={warnings} />)

    expect(screen.getByText(/Wind warning in effect/)).toBeInTheDocument()
    const link = screen.getByRole('link', { name: /view details/i })
    expect(link).toHaveAttribute('href', 'http://www.bom.gov.au/qld/forecasts/map.shtml')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'))
    expect(link).toHaveAttribute('rel', expect.stringContaining('noreferrer'))
  })

  it('renders the notice even when only a surf warning has no sections but a wind warning does', () => {
    const warnings: ForecastWarnings = {
      provider: 'bom',
      region: 'Capricornia Coast',
      bulletins: [
        {
          id: 'IDQ20085',
          title: 'Marine Wind Warning Summary for Queensland',
          issuedAt: '2026-07-05T01:51:00Z',
          detailsUrl: '',
          category: 'wind',
          sections: [{ day: 'Sunday 5 July', warningType: 'Strong Wind Warning' }],
        },
      ],
    }
    render(<WindWarningNotice warnings={warnings} />)
    expect(screen.getByText(/Wind warning in effect/)).toBeInTheDocument()
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })
})

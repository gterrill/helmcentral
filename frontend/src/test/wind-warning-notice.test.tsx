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

  // It used to return a bare fragment so it could sit inline inside the
  // forecast summary paragraph. It is the highest-stakes element on the page,
  // so it now carries its own block wrapper and can be found and styled as a
  // unit rather than being indistinguishable from the prose around it.
  it('renders as its own block element, not a bare run of text', () => {
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
    const { container } = render(<WindWarningNotice warnings={warnings} />)

    expect(container.childNodes).toHaveLength(1)
    const notice = container.firstChild as HTMLElement
    expect(notice.nodeType).toBe(Node.ELEMENT_NODE)
    expect(notice).toHaveAttribute('data-testid', 'forecast-wind-warning')
    expect(notice).toHaveTextContent('Wind warning in effect.')
    expect(notice.textContent?.startsWith(' ')).toBe(false)
  })
})

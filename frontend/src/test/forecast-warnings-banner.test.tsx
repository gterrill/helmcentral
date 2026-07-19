import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import { ForecastWarningsBanner } from '@/components/forecast-warnings-banner'
import type { ForecastWarnings } from '@/hooks/use-forecast-warnings'

const windWarning: ForecastWarnings = {
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

const surfWarning: ForecastWarnings = {
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

describe('ForecastWarningsBanner', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('renders nothing when there is no active warning', () => {
    const { container } = render(<ForecastWarningsBanner warnings={null} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing when warnings has no bulletins', () => {
    const warnings: ForecastWarnings = { provider: 'bom', region: 'Capricornia Coast', bulletins: [] }
    const { container } = render(<ForecastWarningsBanner warnings={warnings} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders the warning with the region in the header', () => {
    render(<ForecastWarningsBanner warnings={windWarning} />)
    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(screen.getByText(/Strong Wind Warning/)).toBeInTheDocument()
    expect(screen.getByText(/Capricornia Coast/)).toBeInTheDocument()
    expect(screen.getByText(/Sunday 5 July/)).toBeInTheDocument()
  })

  it('renders a generic "View details" link when detailsUrl is present', () => {
    render(<ForecastWarningsBanner warnings={windWarning} />)
    const link = screen.getByRole('link', { name: /view details/i })
    expect(link).toHaveAttribute('href', 'http://www.bom.gov.au/qld/forecasts/map.shtml')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'))
    expect(link).toHaveAttribute('rel', expect.stringContaining('noreferrer'))
  })

  it('does not render a details link when detailsUrl is empty', () => {
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
    render(<ForecastWarningsBanner warnings={warnings} />)
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('stacks multiple simultaneous warnings', () => {
    const warnings: ForecastWarnings = {
      provider: 'bom',
      region: 'Capricornia Coast',
      bulletins: [...windWarning.bulletins, ...surfWarning.bulletins],
    }
    render(<ForecastWarningsBanner warnings={warnings} />)
    expect(screen.getByText(/Strong Wind Warning/)).toBeInTheDocument()
    expect(screen.getByText(/Hazardous Surf Warning/)).toBeInTheDocument()
  })

  it('renders nothing when bulletins is an empty array (backend already filtered everything out)', () => {
    const warnings: ForecastWarnings = {
      provider: 'bom',
      region: 'Capricornia Coast',
      bulletins: [],
    }
    const { container } = render(<ForecastWarningsBanner warnings={warnings} />)
    expect(container).toBeEmptyDOMElement()
    expect(screen.queryByText(/Hazardous Surf Warning/)).not.toBeInTheDocument()
  })

  it('renders a dismiss button when there is an active warning', () => {
    render(<ForecastWarningsBanner warnings={windWarning} />)
    expect(screen.getByRole('button', { name: /dismiss/i })).toBeInTheDocument()
  })

  it('hides the banner after clicking dismiss', () => {
    render(<ForecastWarningsBanner warnings={windWarning} />)
    expect(screen.getByRole('alert')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /dismiss/i }))

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('still shows a different (new) warning after an earlier warning was dismissed', () => {
    const { rerender } = render(<ForecastWarningsBanner warnings={windWarning} />)

    fireEvent.click(screen.getByRole('button', { name: /dismiss/i }))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    // A different warning (different id/day/type combination) comes in -
    // dismissing warning A must not suppress unrelated warning B.
    rerender(<ForecastWarningsBanner warnings={surfWarning} />)
    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(screen.getByText(/Hazardous Surf Warning/)).toBeInTheDocument()
  })

  it('persists dismissal across remounts for the same warning signature', () => {
    const { unmount } = render(<ForecastWarningsBanner warnings={windWarning} />)

    fireEvent.click(screen.getByRole('button', { name: /dismiss/i }))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    unmount()

    // Simulate a page reload: remount with the exact same still-active warning.
    render(<ForecastWarningsBanner warnings={windWarning} />)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})

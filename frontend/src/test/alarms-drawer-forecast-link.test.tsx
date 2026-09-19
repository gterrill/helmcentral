import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AlarmsDrawer } from '@/components/alarms-drawer'
import type { ActiveAlarm } from '@/hooks/use-alarms'
import type { ForecastWarnings } from '@/hooks/use-forecast-warnings'
import { FORECAST_WIND_WARNING_PATH } from '@/lib/alarm-display'

// useAlarmLog is the one hook the drawer still owns itself; rules now come
// in as props (see alarms-drawer-actions.test.tsx for the pattern), so this
// file passes empty rules directly instead of mocking useAlarmRules.
vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
  useAlarmLog: () => ({ entries: [], refresh: vi.fn().mockResolvedValue(undefined) }),
}))

function makeAlarm(overrides: Partial<ActiveAlarm> = {}): ActiveAlarm {
  return {
    rule_id: 'helmcentral:forecast-gale',
    label: 'Forecast gale or storm warning',
    path: FORECAST_WIND_WARNING_PATH,
    phase: 'active',
    state: 'alarm',
    op: 'above',
    unit: '',
    value: 2,
    message: 'Forecast gale or storm warning',
    silenced: false,
    can_silence: false,
    can_acknowledge: true,
    ...overrides,
  }
}

function makeForecastWarnings(detailsUrl: string): ForecastWarnings {
  return {
    provider: 'bom',
    region: 'Capricornia Coast',
    bulletins: [
      {
        id: 'IDQ20085',
        title: 'Marine Wind Warning Summary for Queensland',
        issuedAt: null,
        detailsUrl,
        category: 'wind',
        sections: [{ day: 'Sunday 5 July', warningType: 'Gale Warning' }],
      },
    ],
  }
}

function renderDrawer(alarms: ActiveAlarm[], forecastWarnings: ForecastWarnings | null) {
  return render(
    <AlarmsDrawer
      alarms={alarms}
      onAcknowledge={vi.fn().mockResolvedValue(undefined)}
      onSilence={vi.fn().mockResolvedValue(undefined)}
      rules={[]}
      loading={false}
      error={null}
      createRule={vi.fn()}
      updateRule={vi.fn()}
      deleteRule={vi.fn()}
      collisionTuningUrl={null}
      forecastWarnings={forecastWarnings}
    />,
  )
}

// The bulletin link is resolved client-side by joining the alarm's path to
// the forecast-warnings payload App already holds (ADR 0114), rather than
// carried on the alarm itself.
describe('AlarmsDrawer forecast bulletin link', () => {
  it('links a forecast alarm card to the active bulletin and drops the Forecast-page clause', () => {
    renderDrawer([makeAlarm()], makeForecastWarnings('https://www.bom.gov.au/qld/warnings/'))

    expect(screen.getByText('Gale warning in force.')).toBeInTheDocument()
    expect(screen.queryByText(/Details on the Forecast page/)).toBeNull()

    const link = screen.getByRole('link', { name: 'View details →' })
    expect(link).toHaveAttribute('href', 'https://www.bom.gov.au/qld/warnings/')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('keeps the Forecast-page sentence and adds no link when there is no matching active bulletin', () => {
    renderDrawer([makeAlarm()], null)

    expect(screen.getByText('Gale warning in force. Details on the Forecast page.')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /View details/ })).toBeNull()
  })

  it('does not add a bulletin link to a non-forecast alarm card', () => {
    renderDrawer(
      [
        {
          rule_id: 'helmcentral:anchor-drag',
          label: 'Anchor dragging',
          path: 'notifications.navigation.anchor',
          phase: 'active',
          state: 'alarm',
          value: 62,
          message: 'Anchor dragging: 62m from where it was set',
          silenced: false,
          can_silence: false,
          can_acknowledge: true,
        },
      ],
      makeForecastWarnings('https://www.bom.gov.au/qld/warnings/'),
    )

    expect(screen.queryByRole('link', { name: /View details/ })).toBeNull()
  })
})

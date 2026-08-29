import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { RadarTargetsTile } from '@/components/radar-targets-tile'
import type { RadarInfo, RadarTarget } from '@/hooks/use-radar-targets'

const oneRadar: RadarInfo[] = [{ id: 'fur6424A', name: 'DRS4D-NXT 6424', transmitting: true }]

const dualRangeRadars: RadarInfo[] = [
  { id: 'fur6424A', name: 'DRS4D-NXT 6424', transmitting: true },
  { id: 'fur6424B', name: 'DRS4D-NXT 6424 B', transmitting: true },
]

// Built from the field shapes captured in backend/testdata/mayara/targets-fur6424A.json
// (converted to the SSE wire contract), so the fixture matches reality.
const trackingTarget: RadarTarget = {
  id: 'fur6424A:100000016',
  radar_id: 'fur6424A',
  target_id: 100000016,
  status: 'tracking',
  bearing_rad: 0.768,
  range_m: 418,
  lat: -20.3446,
  lon: 148.952,
  position_derived: false,
  course_rad: 5.286,
  sog_knots: 13.1,
  cpa_m: 422.9,
  tcpa_seconds: 7.19,
  is_dangerous: false,
  acquisition: 'auto',
  source_zone: 1,
  age_seconds: 3,
  last_seen_at: '2026-08-28T02:00:12.326Z',
}

const dangerousTarget: RadarTarget = {
  ...trackingTarget,
  id: 'fur6424A:100000029',
  target_id: 100000029,
  cpa_m: 3.7,
  tcpa_seconds: 111.4,
  is_dangerous: true,
}

describe('RadarTargetsTile', () => {
  it('renders -- when the integration is disabled', () => {
    render(<RadarTargetsTile targets={[]} radars={[]} source="disabled" loading={false} distanceUnits="metric" />)
    expect(screen.getByText('--')).toBeInTheDocument()
  })

  it('renders RADAR OFFLINE in the alert treatment when mayara is unreachable', () => {
    render(<RadarTargetsTile targets={[]} radars={oneRadar} source="mayara-unreachable" loading={false} distanceUnits="metric" />)
    const offline = screen.getByText('RADAR OFFLINE')
    expect(offline).toBeInTheDocument()
    expect(offline.className).toMatch(/text-red-/)
  })

  it('renders -- for a healthy radar with zero targets, never a fabricated row', () => {
    render(<RadarTargetsTile targets={[]} radars={oneRadar} source="mayara" loading={false} distanceUnits="metric" />)
    expect(screen.getByText('--')).toBeInTheDocument()
    expect(screen.queryByText(/RDR/)).not.toBeInTheDocument()
  })

  it('gives a dangerous target the alert treatment', () => {
    const { container } = render(
      <RadarTargetsTile targets={[dangerousTarget]} radars={oneRadar} source="mayara" loading={false} distanceUnits="metric" />,
    )
    const alertNode = container.querySelector('.text-red-500, .text-red-600')
    expect(alertNode).not.toBeNull()
  })

  it('does not give a normal target the alert treatment', () => {
    const { container } = render(
      <RadarTargetsTile targets={[trackingTarget]} radars={oneRadar} source="mayara" loading={false} distanceUnits="metric" />,
    )
    expect(container.querySelector('.text-red-500, .text-red-600')).toBeNull()
  })

  it('does not render one header line per radar when a dual-range radar is present', () => {
    render(<RadarTargetsTile targets={[]} radars={dualRangeRadars} source="mayara" loading={false} distanceUnits="metric" />)
    expect(screen.getAllByText(/DRS4D-NXT 6424/)).toHaveLength(1)
    expect(screen.queryByText('DRS4D-NXT 6424 B')).not.toBeInTheDocument()
  })

  it('names the radar in the header when healthy', () => {
    render(<RadarTargetsTile targets={[]} radars={oneRadar} source="mayara" loading={false} distanceUnits="metric" />)
    expect(screen.getByText(/DRS4D-NXT 6424/)).toBeInTheDocument()
  })

  it('renders a target row with range, bearing, CPA and TCPA under an RDR tag', () => {
    render(<RadarTargetsTile targets={[trackingTarget]} radars={oneRadar} source="mayara" loading={false} distanceUnits="metric" />)
    expect(screen.getByText(/RDR/)).toBeInTheDocument()
    expect(screen.getByText(/418 m/)).toBeInTheDocument()
  })
})

// A radar in standby serves no targets, and an empty list on its own reads as
// clear water. Measured 2026-08-28: mayara kept serving 50 extrapolated
// contacts half an hour into standby, which the backend now gates out
// (observedTargets, radar_source.go). The tile has to say why it is empty.
describe('radar power state', () => {
  it('says STANDBY when the radar is not transmitting', () => {
    render(
      <RadarTargetsTile
        targets={[]}
        radars={[{ id: 'fur6424A', name: 'DRS4D-NXT 6424', transmitting: false }]}
        source="mayara"
        loading={false}
        distanceUnits="metric"
      />,
    )

    expect(screen.getByText(/standby/i)).toBeInTheDocument()
  })

  it('names the radar normally while it is transmitting', () => {
    render(
      <RadarTargetsTile
        targets={[]}
        radars={[{ id: 'fur6424A', name: 'DRS4D-NXT 6424', transmitting: true }]}
        source="mayara"
        loading={false}
        distanceUnits="metric"
      />,
    )

    expect(screen.getByText('DRS4D-NXT 6424')).toBeInTheDocument()
    expect(screen.queryByText(/standby/i)).toBeNull()
  })
})

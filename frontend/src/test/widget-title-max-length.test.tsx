/**
 * The three widget titles the backend caps at gaugeGroupTitleMaxLen.
 *
 * dashboard_pages.go rejects a cluster, indicator strip or gauge group whose
 * title runs past 48 characters. Nothing stopped the operator typing a longer
 * one, so the only signal was a save that failed after the fact. These assert
 * the input refuses the 49th character in the first place, the same way the
 * lamp label field already does with LAMP_LABEL_MAX_LENGTH.
 */

import { render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { EngineClusterConfigDialog } from '@/components/engine-cluster-config-dialog'
import { GaugeGroupConfigDialog } from '@/components/gauge-group-config-dialog'
import { LampStripConfigDialog } from '@/components/lamp-strip-config-dialog'
import { GAUGE_GROUP_TITLE_MAX_LENGTH } from '@/lib/dashboard-widgets'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

vi.mock('@/hooks/use-signalk-paths', () => ({ useSignalKPaths: () => ({ paths: [], loading: false }) }))
vi.mock('@/hooks/use-engine-profiles', () => ({
  useEngineProfiles: () => ({ profiles: [], problems: [], loading: false }),
}))

const cluster: DashboardLayoutItem = {
  id: 'cluster:abcd1234', x: 0, y: 0, w: 6, h: 8,
  cluster: {
    title: 'Port',
    ring: { path: 'propulsion.port.revolutions', label: 'RPM', display: 'radial', quantity: 'frequency', unit: 'rpm' },
    centre: { path: 'propulsion.port.runTime', label: 'Hours', display: 'numeric', quantity: 'duration', unit: 'h' },
    corners: [],
  },
}

const lamps: DashboardLayoutItem = {
  id: 'lamps:abcd1234', x: 0, y: 0, w: 6, h: 4,
  lamps: { title: 'Status', lamps: [{ path: 'electrical.alarm', label: 'Alarm' }], showCheck: true },
}

const gaugeGroup: DashboardLayoutItem = {
  id: 'gauge-group:abcd1234', x: 0, y: 0, w: 6, h: 8,
  gaugeGroup: {
    title: 'Port',
    gauges: [{ path: 'propulsion.port.revolutions', label: 'RPM', display: 'numeric', quantity: 'frequency', unit: 'rpm' }],
  },
}

describe('widget title length cap', () => {
  test('the cap matches the one the backend enforces', () => {
    expect(GAUGE_GROUP_TITLE_MAX_LENGTH).toBe(48)
  })

  test.each([
    ['engine cluster', <EngineClusterConfigDialog key="c" widget={cluster} onCancel={vi.fn()} onSave={vi.fn()} />],
    ['indicator strip', <LampStripConfigDialog key="l" widget={lamps} onCancel={vi.fn()} onSave={vi.fn()} />],
    ['gauge group', <GaugeGroupConfigDialog key="g" widget={gaugeGroup} onCancel={vi.fn()} onSave={vi.fn()} />],
  ])('%s caps its title at the backend limit', (_name, dialog) => {
    render(dialog)
    expect(screen.getByLabelText('Title')).toHaveAttribute('maxLength', String(GAUGE_GROUP_TITLE_MAX_LENGTH))
  })
})

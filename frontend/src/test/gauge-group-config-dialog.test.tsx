import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { GaugeGroupConfigDialog } from '@/components/gauge-group-config-dialog'
import type { DashboardLayoutItem, GaugeGroupWidgetConfig } from '@/lib/dashboard-widgets'
import type { EngineProfile } from '@/lib/engine-profiles'
import type { EquipmentTileContext } from '@/lib/equipment-tiles'

const oilGauge = { path_suffix: 'oilPressure', label: 'Oil Press', display: 'radial', quantity: 'pressure', unit: 'psi', min: 0, max: 100 } as const
const yanmar: EngineProfile = {
  id: 'yanmar-4jh', name: 'Yanmar 4JH', kind: 'engine',
  gauges: [
    { ...oilGauge, zones: [{ direction: 'below', threshold: 15, state: 'alarm' }] },
    { path_suffix: 'revolutions', label: 'RPM', hero: true, display: 'radial', quantity: 'frequency', unit: 'rpm', min: 0, max: 3000 },
  ],
}
let ctx: EquipmentTileContext = { items: [], profiles: [], reporting: null }
vi.mock('@/hooks/use-equipment-tile-context', () => ({ useEquipmentTileContext: () => ctx }))

const portEngine: GaugeGroupWidgetConfig = {
  title: 'Port',
  gauges: [
    { path: 'propulsion.port.revolutions', label: 'RPM', display: 'numeric', quantity: 'frequency', unit: 'rpm' },
    { path: 'propulsion.port.oilPressure', label: 'Oil Press', display: 'numeric', quantity: 'pressure', unit: 'psi' },
    { path: 'environment.depth.belowTransducer', label: 'Depth', display: 'numeric', quantity: 'length', unit: 'ft' },
  ],
}

function widget(config: GaugeGroupWidgetConfig): DashboardLayoutItem {
  return { id: 'gauge-group:m1x8abcd', x: 0, y: 0, w: 6, h: 8, gaugeGroup: config }
}

function pathInputs(): HTMLInputElement[] {
  return screen.getAllByLabelText('SignalK path') as HTMLInputElement[]
}

beforeEach(() => {
  ctx = { items: [], profiles: [], reporting: null }
  vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
    if (String(url).includes('/api/equipment-profiles') || String(url).includes('/api/engine-profiles')) {
      return Promise.resolve({
        ok: true,
        json: async () => ({
          profiles: [{
            id: 'alt',
            name: 'Alternator',
            kind: 'alternator',
            gauges: [
              { path_suffix: 'power', label: 'Output', hero: true, display: 'numeric', quantity: 'power', unit: 'W' },
              { path_suffix: 'temperature', label: 'Temp', display: 'numeric', quantity: 'temperature', unit: 'C' },
            ],
          }],
          problems: [],
        }),
      })
    }
    return Promise.resolve({ ok: true, json: async () => ({ paths: [{ path: 'propulsion.port.revolutions' }] }) })
  }))
})

describe('GaugeGroupConfigDialog', () => {
  test('lists one row per member', () => {
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={vi.fn()} />)
    expect(pathInputs()).toHaveLength(3)
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Port')
  })

  test('adds and removes members', () => {
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: /add gauge/i }))
    expect(pathInputs()).toHaveLength(4)

    fireEvent.click(screen.getAllByRole('button', { name: /remove gauge/i })[3])
    expect(pathInputs()).toHaveLength(3)
  })

  test('will not save a group with a blank path, which the backend rejects', () => {
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={vi.fn()} />)
    const save = screen.getByRole('button', { name: 'Save' })
    expect(save).not.toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: /add gauge/i }))
    expect(save).toBeDisabled()
  })

  test('will not save an untitled group', () => {
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={vi.fn()} />)
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: '  ' } })
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  test('duplicates the current config and closes without any replace controls', () => {
    const onDuplicate = vi.fn()
    render(
      <GaugeGroupConfigDialog
        widget={widget(portEngine)}
        onCancel={vi.fn()}
        onSave={vi.fn()}
        onDuplicate={onDuplicate}
      />,
    )

    expect(screen.queryByLabelText('Replace')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('With')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /duplicate this tile/i }))

    expect(onDuplicate).toHaveBeenCalledOnce()
    expect(onDuplicate.mock.calls[0][0]).toMatchObject({
      title: 'Port',
      gauges: expect.arrayContaining([
        expect.objectContaining({ path: 'propulsion.port.revolutions' }),
      ]),
    })
  })

  test('re-seeds per instance, so editing one group never shows another', () => {
    const other: GaugeGroupWidgetConfig = { title: 'Starboard', gauges: [portEngine.gauges[0]] }
    const { rerender } = render(
      <GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={vi.fn()} />,
    )
    rerender(
      <GaugeGroupConfigDialog
        widget={{ ...widget(other), id: 'gauge-group:zzzz9999' }}
        onCancel={vi.fn()}
        onSave={vi.fn()}
      />,
    )
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Starboard')
    expect(pathInputs()).toHaveLength(1)
  })

  test('saves the selected hero gauge index', () => {
    const onSave = vi.fn()
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={onSave} />)

    fireEvent.click(screen.getByRole('button', { name: /set gauge 1 as hero/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    const saved = onSave.mock.calls[0][0] as GaugeGroupWidgetConfig
    expect(saved.hero).toBe(0)
  })

  test('moves the hero index with its gauge when reordered', () => {
    const onSave = vi.fn()
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={onSave} />)

    fireEvent.click(screen.getByRole('button', { name: /set gauge 1 as hero/i }))
    fireEvent.click(screen.getByRole('button', { name: /move gauge 1 down/i }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    const saved = onSave.mock.calls[0][0] as GaugeGroupWidgetConfig
    expect(saved.hero).toBe(1)
  })

  test('clears the hero index when that gauge is removed', () => {
    const onSave = vi.fn()
    render(<GaugeGroupConfigDialog widget={widget(portEngine)} onCancel={vi.fn()} onSave={onSave} />)

    fireEvent.click(screen.getByRole('button', { name: /set gauge 2 as hero/i }))
    fireEvent.click(screen.getAllByRole('button', { name: /remove gauge/i })[1])
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    const saved = onSave.mock.calls[0][0] as GaugeGroupWidgetConfig
    expect(saved.hero).toBeUndefined()
  })

  describe('a group that shows equipment (ADR 0185)', () => {
    beforeEach(() => {
      ctx = {
        items: [
          { id: 'eng-port', name: 'Port engine', signalk_source: 'propulsion.port', source_kind: 'engine', profile_id: 'yanmar-4jh' },
          { id: 'eng-star', name: 'Starboard engine', signalk_source: 'propulsion.starboard', source_kind: 'engine', profile_id: 'yanmar-4jh' },
        ],
        profiles: [yanmar],
        reporting: null,
      }
    })

    const equipmentGroup: GaugeGroupWidgetConfig = {
      equipmentKind: 'engine', equipmentId: 'eng-port', title: '',
      gauges: [{ path: '', suffix: 'oilPressure', label: 'Oil Press', display: 'radial', quantity: 'pressure', unit: 'psi', min: 0, max: 100 }],
    }

    test('fills the gauges and the hero from the item profile', () => {
      const onSave = vi.fn()
      render(<GaugeGroupConfigDialog widget={widget(equipmentGroup)} onCancel={vi.fn()} onSave={onSave} />)
      fireEvent.click(screen.getByRole('button', { name: /fill gauges from profile/i }))
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))

      const saved = onSave.mock.calls[0][0] as GaugeGroupWidgetConfig
      expect(saved.gauges.map((g) => g.suffix)).toEqual(['oilPressure', 'revolutions'])
      expect(saved.gauges.every((g) => g.path === '' && g.zones === undefined)).toBe(true)
      expect(saved.hero).toBe(1)
      expect(saved.equipmentId).toBe('eng-port')
    })

    test('a title is optional, because the item name is the default', () => {
      render(<GaugeGroupConfigDialog widget={widget(equipmentGroup)} onCancel={vi.fn()} onSave={vi.fn()} />)
      expect((screen.getByLabelText('Title') as HTMLInputElement).placeholder).toBe('Port engine')
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    })

    test('will not save with no equipment picked', () => {
      render(<GaugeGroupConfigDialog widget={widget({ ...equipmentGroup, equipmentId: '' })} onCancel={vi.fn()} onSave={vi.fn()} />)
      expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    })

    test('retargets to the other engine by picking the other item', () => {
      const onSave = vi.fn()
      render(<GaugeGroupConfigDialog widget={widget(equipmentGroup)} onCancel={vi.fn()} onSave={onSave} />)
      fireEvent.change(screen.getByLabelText('Equipment'), { target: { value: 'eng-star' } })
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
      expect(onSave.mock.calls[0][0]).toMatchObject({ equipmentId: 'eng-star', gauges: [{ suffix: 'oilPressure' }] })
    })

    test('shows the profile zones and says what editing them touches', () => {
      render(<GaugeGroupConfigDialog widget={widget(equipmentGroup)} onCancel={vi.fn()} onSave={vi.fn()} />)
      expect((screen.getByLabelText('Zone 1 threshold') as HTMLInputElement).value).toBe('15')
      expect(screen.getByTestId('zone-notice')).toHaveTextContent(
        'Applies to every tile and alarm using Yanmar 4JH (Port engine, Starboard engine)',
      )
    })

    test('editing a zone saves the profile first, then the tile', async () => {
      const calls: string[] = []
      vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string, init?: RequestInit) => {
        calls.push(`${init?.method ?? 'GET'} ${url}`)
        return Promise.resolve({ ok: true, json: async () => ({ profile: yanmar }) })
      }))
      const onSave = vi.fn(() => { calls.push('tile saved') })
      render(<GaugeGroupConfigDialog widget={widget(equipmentGroup)} onCancel={vi.fn()} onSave={onSave} />)

      fireEvent.change(screen.getByLabelText('Zone 1 threshold'), { target: { value: '25' } })
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))

      await vi.waitFor(() => expect(onSave).toHaveBeenCalled())
      const put = calls.findIndex((c) => c.startsWith('PUT') && c.includes('/api/equipment-profiles/yanmar-4jh'))
      expect(put).toBeGreaterThanOrEqual(0)
      expect(put).toBeLessThan(calls.indexOf('tile saved'))
      const body = JSON.parse(((vi.mocked(fetch).mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'PUT')![1]) as RequestInit).body as string)
      expect(body.gauges[0].zones[0]).toMatchObject({ direction: 'below', threshold: 25, state: 'alarm' })
    })

    test('a failed profile save keeps the dialog open and says why', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 400, json: async () => ({ error: 'zone 1 is wrong' }) }))
      const onSave = vi.fn()
      render(<GaugeGroupConfigDialog widget={widget(equipmentGroup)} onCancel={vi.fn()} onSave={onSave} />)
      fireEvent.change(screen.getByLabelText('Zone 1 threshold'), { target: { value: '25' } })
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('zone 1 is wrong')
      expect(onSave).not.toHaveBeenCalled()
    })
  })
})

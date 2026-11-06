import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactElement } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { EmbedConfigDialog } from '@/components/embed-config-dialog'
import { GaugeConfigDialog } from '@/components/gauge-config-dialog'
import { GaugeGroupConfigDialog } from '@/components/gauge-group-config-dialog'
import { LampStripConfigDialog } from '@/components/lamp-strip-config-dialog'
import { PoiMapConfigDialog } from '@/components/poi-map-config-dialog'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

// Every tile dialog's Save goes busy while the write is out (the pattern the
// engine cluster dialog set), and a failed write is shown in the dialog.

vi.mock('@/hooks/use-signalk-paths', () => ({
  useSignalKPaths: () => ({ paths: [], loading: false, error: null }),
}))
vi.mock('@/hooks/use-equipment-tile-context', () => ({
  useEquipmentTileContext: () => ({ items: [], profiles: [], reporting: null }),
}))

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ profiles: [] }) }))
})

const gaugeConfig = { path: 'environment.depth.belowTransducer', label: 'Depth', display: 'numeric', quantity: 'length', unit: 'ft' } as const
const gaugeWidget: DashboardLayoutItem = { id: 'gauge:m1x8abcd', x: 0, y: 0, w: 3, h: 6, gauge: gaugeConfig }
const groupWidget: DashboardLayoutItem = { id: 'gauge-group:m1x8abcd', x: 0, y: 0, w: 4, h: 7, gaugeGroup: { title: 'Depths', gauges: [gaugeConfig] } }
const lampsWidget = { id: 'lamps:m1x8abcd', lamps: { title: 'Status', lamps: [{ path: 'electrical.generator.state', label: 'GEN' }], showCheck: true } }
const embedWidget: DashboardLayoutItem = { id: 'embed:m1x8abcd', x: 0, y: 0, w: 6, h: 8, embed: { title: 'Windrose', url: 'http://boat.local:3000/d-solo/abc?panelId=2' } }
const poiWidget: DashboardLayoutItem = {
  id: 'poi-map:m1x8abcd', x: 0, y: 0, w: 12, h: 7,
  poiMap: { title: 'Nearby', rangeNm: 5, categories: ['anchorage', 'marina'], layout: 'split', showAis: true, showTrail: false },
}

// Each dialog's onSave takes its own arguments; the test only needs it to be
// called once and to hand back the promise it is given.
type Save = () => void | Promise<void>
const dialogs: Array<[string, (onSave: Save) => ReactElement]> = [
  ['Gauge', (onSave) => <GaugeConfigDialog widget={gaugeWidget} onCancel={vi.fn()} onSave={onSave} />],
  ['Gauge group', (onSave) => <GaugeGroupConfigDialog widget={groupWidget} onCancel={vi.fn()} onSave={onSave} />],
  ['Lamp strip', (onSave) => <LampStripConfigDialog widget={lampsWidget} onCancel={vi.fn()} onSave={onSave} />],
  ['Embed', (onSave) => <EmbedConfigDialog widget={embedWidget} open onOpenChange={vi.fn()} onSave={onSave} />],
  ['Nearby map', (onSave) => <PoiMapConfigDialog widget={poiWidget} open onOpenChange={vi.fn()} onSave={onSave} />],
]

const deferred = () => {
  let resolve!: () => void
  let reject!: (err: Error) => void
  const promise = new Promise<void>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
const saveButton = () => screen.getByRole('button', { name: /^sav(e|ing)/i }) as HTMLButtonElement

describe.each(dialogs)('%s dialog Save', (_name, dialog) => {
  test('is disabled and reads Saving… while the write is pending, then frees', async () => {
    const write = deferred()
    const onSave = vi.fn(() => write.promise)
    render(dialog(onSave))
    fireEvent.click(saveButton())
    expect(onSave).toHaveBeenCalledTimes(1)
    await vi.waitFor(() => expect(saveButton().disabled).toBe(true))
    expect(saveButton().textContent).toBe('Saving…')
    fireEvent.click(saveButton())
    expect(onSave).toHaveBeenCalledTimes(1)
    write.resolve()
    await vi.waitFor(() => expect(saveButton().disabled).toBe(false))
    expect(saveButton().textContent).toBe('Save')
  })

  test('shows a rejected write, stays open and can be retried', async () => {
    const retry = deferred()
    const onSave = vi.fn<Save>()
      .mockRejectedValueOnce(new Error('refused'))
      .mockReturnValueOnce(retry.promise)
    render(dialog(onSave))
    fireEvent.click(saveButton())
    expect((await screen.findByRole('alert')).textContent).toContain('refused')
    expect(saveButton().disabled).toBe(false)
    fireEvent.click(saveButton())
    await vi.waitFor(() => expect(saveButton().disabled).toBe(true))
    expect(screen.queryByText('refused')).toBeNull()
    retry.resolve()
    await vi.waitFor(() => expect(saveButton().disabled).toBe(false))
    expect(onSave).toHaveBeenCalledTimes(2)
  })

  test('shows a write that throws at once', async () => {
    render(dialog(() => { throw new Error('wired wrong') }))
    fireEvent.click(saveButton())
    expect((await screen.findByRole('alert')).textContent).toContain('wired wrong')
    expect(saveButton().disabled).toBe(false)
  })
})

describe('Embed and Nearby map dialogs leave closing to the save', () => {
  test.each([
    ['Embed', (onOpenChange: () => void) => <EmbedConfigDialog widget={embedWidget} open onOpenChange={onOpenChange} onSave={async () => {}} />],
    ['Nearby map', (onOpenChange: () => void) => <PoiMapConfigDialog widget={poiWidget} open onOpenChange={onOpenChange} onSave={async () => {}} />],
  ])('%s does not close itself, so a failed write keeps it open', async (_name, make) => {
    const onOpenChange = vi.fn()
    render(make(onOpenChange))
    fireEvent.click(saveButton())
    await vi.waitFor(() => expect(saveButton().disabled).toBe(false))
    expect(onOpenChange).not.toHaveBeenCalled()
  })
})

import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactElement } from 'react'
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { EmbedConfigDialog } from '@/components/embed-config-dialog'
import { GaugeConfigDialog } from '@/components/gauge-config-dialog'
import { GaugeGroupConfigDialog } from '@/components/gauge-group-config-dialog'
import { LampStripConfigDialog } from '@/components/lamp-strip-config-dialog'
import { PoiMapConfigDialog } from '@/components/poi-map-config-dialog'
import { useTileDraft } from '@/hooks/use-tile-draft'
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

describe('a write the page refuses reaches the dialog', () => {
  // The real shape: useTileDraft's saveTile, over an updatePage that answers
  // null after toasting. Nothing rejects in the dialog's own code.
  test('Gauge dialog shows the failure, stays open and saves on retry', async () => {
    const updatePage = vi.fn<() => Promise<object | null>>().mockResolvedValueOnce(null).mockResolvedValue({})
    function Harness() {
      const tiles = useTileDraft({
        pageRef: { current: { id: 'p1' } },
        widgetsRef: { current: [] },
        updatePage,
      })
      return (
        <>
          <button onClick={() => tiles.addTile('gauge')}>add</button>
          <GaugeConfigDialog
            widget={tiles.draftOfKind('gauge')}
            onCancel={tiles.cancelTile}
            onSave={(config) => tiles.saveTile('gauge', config)}
          />
        </>
      )
    }
    render(<Harness />)
    fireEvent.click(screen.getByText('add'))
    fireEvent.change(await screen.findByLabelText('SignalK path'), { target: { value: 'environment.depth.belowTransducer' } })
    fireEvent.click(saveButton())
    expect((await screen.findByRole('alert')).textContent).toContain('Could not save the tile')
    expect(saveButton().disabled).toBe(false)
    fireEvent.click(saveButton())
    await vi.waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(updatePage).toHaveBeenCalledTimes(2)
  })
})

describe('other buttons wait for the save', () => {
  test('Duplicate on a gauge group is disabled while Save is out', async () => {
    const write = deferred()
    render(<GaugeGroupConfigDialog widget={groupWidget} onCancel={vi.fn()} onSave={() => write.promise} onDuplicate={vi.fn()} />)
    fireEvent.click(saveButton())
    await vi.waitFor(() => expect(saveButton().disabled).toBe(true))
    expect((screen.getByRole('button', { name: 'Duplicate this tile' }) as HTMLButtonElement).disabled).toBe(true)
    write.resolve()
    await vi.waitFor(() => expect((screen.getByRole('button', { name: 'Duplicate this tile' }) as HTMLButtonElement).disabled).toBe(false))
  })

  test('Duplicate shows a refused copy and does not cancel', async () => {
    const onCancel = vi.fn()
    render(<GaugeGroupConfigDialog widget={groupWidget} onCancel={onCancel} onSave={vi.fn()} onDuplicate={() => Promise.reject(new Error('refused'))} />)
    fireEvent.click(screen.getByRole('button', { name: 'Duplicate this tile' }))
    expect((await screen.findByRole('alert')).textContent).toContain('refused')
    expect(onCancel).not.toHaveBeenCalled()
  })

  test('Remove ribbon is disabled while Save is out, and while it removes', async () => {
    const write = deferred()
    const removal = deferred()
    const onRemove = vi.fn(() => removal.promise)
    render(<LampStripConfigDialog widget={lampsWidget} onCancel={vi.fn()} onSave={() => write.promise} onRemove={onRemove} />)
    const remove = () => screen.getByRole('button', { name: 'Remove ribbon' }) as HTMLButtonElement
    fireEvent.click(saveButton())
    await vi.waitFor(() => expect(remove().disabled).toBe(true))
    write.resolve()
    await vi.waitFor(() => expect(remove().disabled).toBe(false))
    fireEvent.click(remove())
    await vi.waitFor(() => expect(saveButton().disabled).toBe(true))
    expect(remove().disabled).toBe(true)
    removal.resolve()
    await vi.waitFor(() => expect(remove().disabled).toBe(false))
  })
})

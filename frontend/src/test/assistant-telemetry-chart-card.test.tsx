import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import { AssistantCard } from '@/components/assistant-card'
import { AssistantTelemetryChartCard } from '@/components/assistant-telemetry-chart-card'
import { resetQueryStore } from '@/lib/api-query'
import { decodeMateCards } from '@/lib/mate-cards'

import { seriesBody, telemetryChartCard, telemetryChartPayload } from './helpers/telemetry-chart-fixture'

const fetchMock = vi.fn()
let reply: { ok: boolean; status: number; body: unknown }

beforeEach(() => {
  resetQueryStore()
  reply = { ok: true, status: 200, body: seriesBody() }
  fetchMock.mockReset()
  fetchMock.mockImplementation(() => Promise.resolve({ ok: reply.ok, status: reply.status, json: async () => reply.body }))
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  vi.unstubAllGlobals()
})

function lastParams(): URLSearchParams {
  const url = String(fetchMock.mock.calls.at(-1)![0])
  expect(url).toContain('/api/telemetry/series?')
  return new URL(url, 'http://x').searchParams
}

describe('decoder: telemetry_chart', () => {
  const decode = (over: Record<string, unknown>) => decodeMateCards([{ kind: 'telemetry_chart', payload: telemetryChartPayload(over) }])[0]
  const reason = (c: ReturnType<typeof decode>) => (c.kind === 'error' ? c.reason : '')

  it('accepts the contract example', () => {
    const c = decode({})
    expect(c).toMatchObject({ kind: 'telemetry_chart', title: 'Alternator current against house bank SOC', view: 'time', xy: null })
    expect(c.kind === 'telemetry_chart' && c.series[0]).toMatchObject({ source: 'venus.c0619ab9d0ee', axis: 'left', unit: 'A' })
    expect(c.kind === 'telemetry_chart' && c.series[1].source).toBeNull()
  })

  it('accepts an xy view with two valid indices and compare ranges', () => {
    const c = decode({
      view: 'xy',
      xy: { x: 1, y: 0 },
      compare: [{ label: 'Before', from: '2026-10-01T00:00:00+10:00', to: '2026-10-05T00:00:00+10:00' }],
    })
    expect(c).toMatchObject({ kind: 'telemetry_chart', view: 'xy', xy: { x: 1, y: 0 } })
  })

  it.each([
    ['xy view without xy', { view: 'xy', xy: null }, 'xy'],
    ['xy index out of range', { view: 'xy', xy: { x: 0, y: 2 } }, 'xy.y'],
    ['xy indices equal', { view: 'xy', xy: { x: 1, y: 1 } }, 'xy'],
    ['time view carrying xy', { view: 'time', xy: { x: 0, y: 1 } }, 'xy'],
    ['unknown view', { view: 'polar' }, 'view'],
    ['missing si_unit', { series: [{ path: 'a', source: null, label: 'A', unit: 'A', axis: 'left' }] }, 'series[0].si_unit'],
    ['si_unit not a string', { series: [{ path: 'a', source: null, label: 'A', unit: 'A', si_unit: 3, axis: 'left' }] }, 'series[0].si_unit'],
    ['bad axis', { series: [{ path: 'a', source: null, label: 'A', unit: 'A', si_unit: 'A', axis: 'middle' }] }, 'series[0].axis'],
    ['no series', { series: [] }, 'series'],
    ['five series', { series: Array.from({ length: 5 }, () => ({ path: 'a', source: null, label: 'A', unit: 'A', si_unit: 'A', axis: 'left' })) }, 'series'],
    ['source not string or null', { series: [{ path: 'a', source: 3, label: 'A', unit: 'A', si_unit: 'A', axis: 'left' }] }, 'series[0].source'],
    ['bad range time', { range: { from: 'soon', to: '2026-10-19T00:00:00+10:00' } }, 'range.from'],
    ['bad timezone', { timezone: 'Mars/Olympus' }, 'timezone'],
    ['four compare ranges', { compare: Array.from({ length: 4 }, () => ({ label: 'x', from: '2026-10-01T00:00:00Z', to: '2026-10-02T00:00:00Z' })) }, 'compare'],
    ['compare time unparseable', { compare: [{ label: 'x', from: 'nope', to: '2026-10-02T00:00:00Z' }] }, 'compare[0].from'],
  ])('rejects %s, naming the field', (_n, over, field) => {
    const c = decode(over)
    expect(c.kind).toBe('error')
    expect(reason(c)).toContain(field)
    expect(c.kind === 'error' && c.rawKind).toBe('telemetry_chart')
  })

  it('keeps the other cards when one telemetry_chart is bad', () => {
    const cards = decodeMateCards([
      { kind: 'telemetry_chart', payload: telemetryChartPayload({ view: 'xy' }) },
      { kind: 'telemetry_chart', payload: telemetryChartPayload() },
    ])
    expect(cards.map((c) => c.kind)).toEqual(['error', 'telemetry_chart'])
  })
})

describe('AssistantTelemetryChartCard', () => {
  it('shows the Chart label, title and range, and asks for aligned path/source/unit', async () => {
    render(<AssistantTelemetryChartCard card={telemetryChartCard()} />)
    expect(screen.getByText('Chart')).toBeInTheDocument()
    expect(screen.getByText('Alternator current against house bank SOC')).toBeInTheDocument()
    expect(screen.getByText('12–19 Oct')).toBeInTheDocument()
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    const p = lastParams()
    expect(p.getAll('path')).toEqual(['electrical.alternators.12.current', 'electrical.batteries.512.capacity.stateOfCharge'])
    expect(p.getAll('source')).toEqual(['venus.c0619ab9d0ee', ''])
    expect(p.getAll('unit')).toEqual(['A', '%'])
    expect(p.getAll('si_unit')).toEqual(['A', '%'])
    expect(Date.parse(p.get('from')!)).toBe(Date.parse('2026-10-12T00:00:00+10:00'))
    expect(Date.parse(p.get('to')!)).toBe(Date.parse('2026-10-19T00:00:00+10:00'))
  })

  it('labels the chart for assistive tech with the title and range', async () => {
    render(<AssistantTelemetryChartCard card={telemetryChartCard()} />)
    expect(await screen.findByRole('img', { name: /Alternator current against house bank SOC.*12–19 Oct/ })).toBeInTheDocument()
  })

  it('shows a skeleton while loading', () => {
    fetchMock.mockImplementation(() => new Promise(() => {}))
    render(<AssistantTelemetryChartCard card={telemetryChartCard()} />)
    expect(screen.getByTestId('telemetry-chart-skeleton')).toBeInTheDocument()
  })

  it('refetches with the new from/to when a preset is chosen', async () => {
    render(<AssistantTelemetryChartCard card={telemetryChartCard()} />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: '24 h' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const to = Date.parse('2026-10-19T00:00:00+10:00')
    let p = lastParams()
    expect(Date.parse(p.get('to')!)).toBe(to)
    expect(Date.parse(p.get('from')!)).toBe(to - 24 * 3_600_000)
    expect(p.getAll('unit')).toEqual(['A', '%'])
    expect(p.getAll('source')).toEqual(['venus.c0619ab9d0ee', ''])
    fireEvent.click(screen.getByRole('button', { name: '30 d' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
    p = lastParams()
    expect(Date.parse(p.get('from')!)).toBe(to - 30 * 24 * 3_600_000)
  })

  it('refetches when custom dates are entered, read in the payload timezone', async () => {
    render(<AssistantTelemetryChartCard card={telemetryChartCard()} />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    fireEvent.change(screen.getByLabelText('From'), { target: { value: '2026-10-14' } })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    expect(Date.parse(lastParams().get('from')!)).toBe(Date.parse('2026-10-14T00:00:00+10:00'))
    fireEvent.change(screen.getByLabelText('To'), { target: { value: '2026-10-16' } })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
    expect(Date.parse(lastParams().get('to')!)).toBe(Date.parse('2026-10-17T00:00:00+10:00'))
  })

  it('treats the To date as inclusive: the same From and To day is that one whole day', async () => {
    render(<AssistantTelemetryChartCard card={telemetryChartCard()} />)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    fireEvent.change(screen.getByLabelText('From'), { target: { value: '2026-10-14' } })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    fireEvent.change(screen.getByLabelText('To'), { target: { value: '2026-10-14' } })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))
    const p = lastParams()
    expect(Date.parse(p.get('to')!) - Date.parse(p.get('from')!)).toBe(24 * 3_600_000)
    expect(screen.getByLabelText('To')).toHaveValue('2026-10-14')
    expect(screen.getByLabelText('From')).toHaveValue('2026-10-14')
    expect(screen.getByText('14 Oct')).toBeInTheDocument()
  })

  it('clamps the end of a To date of today to now', async () => {
    const now = Date.parse('2026-10-17T12:00:00+10:00')
    const spy = vi.spyOn(Date, 'now').mockReturnValue(now)
    try {
      render(<AssistantTelemetryChartCard card={telemetryChartCard()} />)
      await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
      fireEvent.change(screen.getByLabelText('To'), { target: { value: '2026-10-17' } })
      await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
      expect(Date.parse(lastParams().get('to')!)).toBe(now)
      expect(screen.getByLabelText('To')).toHaveValue('2026-10-17')
    } finally {
      spy.mockRestore()
    }
  })

  it('shows labelled chips per compare range, no range control, and fetches each range', async () => {
    const card = telemetryChartCard({
      compare: [
        { label: 'Before regulator change', from: '2026-10-01T00:00:00+10:00', to: '2026-10-05T00:00:00+10:00' },
        { label: 'After', from: '2026-10-12T00:00:00+10:00', to: '2026-10-16T00:00:00+10:00' },
      ],
    })
    render(<AssistantTelemetryChartCard card={card} />)
    expect(screen.getByText('Before regulator change')).toBeInTheDocument()
    expect(screen.getAllByText('After').length).toBeGreaterThan(0)
    expect(screen.queryByRole('button', { name: '7 d' })).toBeNull()
    expect(screen.queryByLabelText('From')).toBeNull()
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const froms = fetchMock.mock.calls.map((c) => Date.parse(new URL(String(c[0]), 'http://x').searchParams.get('from')!)).sort()
    expect(froms).toEqual([Date.parse('2026-10-01T00:00:00+10:00'), Date.parse('2026-10-12T00:00:00+10:00')])
  })

  it('shows the backend message on a 503, styled as an error, with no chart', async () => {
    reply = { ok: false, status: 503, body: { error: 'InfluxDB is not configured' } }
    render(<AssistantTelemetryChartCard card={telemetryChartCard()} />)
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('InfluxDB is not configured')
    expect(screen.queryByRole('img', { name: /Alternator/ })).toBeNull()
  })

  it('shows a 409 naming the sources verbatim', async () => {
    const msg = 'electrical.batteries.512.voltage is logged by 2 sources: venus.a, n2k.b; pick one'
    reply = { ok: false, status: 409, body: { error: msg } }
    render(<AssistantTelemetryChartCard card={telemetryChartCard()} />)
    expect(await screen.findByRole('alert')).toHaveTextContent(msg)
  })

  it('omits the parentheses when a series has no unit', async () => {
    const card = telemetryChartCard({
      series: [
        { path: 'a.b', source: null, label: 'Engine state', unit: '', si_unit: '', axis: 'left' },
        { path: 'c.d', source: null, label: 'House bank SOC', unit: '%', si_unit: '', axis: 'right' },
      ],
    })
    const { container } = render(<AssistantTelemetryChartCard card={card} />)
    await screen.findByRole('img')
    expect(screen.getByText('Engine state')).toBeInTheDocument()
    expect(container.textContent).not.toContain('()')
    expect(screen.getByText('House bank SOC (%)')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'X–Y' }))
    await waitFor(() => expect(screen.getByText(/buckets paired/)).toBeInTheDocument())
    expect(container.textContent).not.toContain('()')
  })

  it('names a series with nothing recorded and still draws the chart', async () => {
    reply = { ok: true, status: 200, body: seriesBody({ socFound: false }) }
    render(<AssistantTelemetryChartCard card={telemetryChartCard()} />)
    expect(await screen.findByText('No House bank SOC recorded in this range')).toBeInTheDocument()
    expect(screen.queryByText(/No Starboard alternator current recorded/)).toBeNull()
    expect(screen.getByRole('img', { name: /Alternator/ })).toBeInTheDocument()
  })

  it('counts paired buckets in the X–Y view', async () => {
    // current has buckets 0-4, soc has 0-3 and 5: four pair of six distinct buckets.
    render(<AssistantTelemetryChartCard card={telemetryChartCard({ view: 'xy', xy: { x: 1, y: 0 } })} />)
    expect(await screen.findByText('4 of 6 buckets paired')).toBeInTheDocument()
  })

  it('toggles between Time and X–Y', async () => {
    render(<AssistantTelemetryChartCard card={telemetryChartCard()} />)
    await screen.findByRole('img', { name: /Alternator/ })
    expect(screen.queryByText(/buckets paired/)).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'X–Y' }))
    // A time-only payload plots series 0 on x and series 1 on y.
    expect(await screen.findByText('4 of 6 buckets paired')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Time' }))
    expect(screen.queryByText(/buckets paired/)).toBeNull()
  })

  it('has no view toggle for a single series', async () => {
    const one = [telemetryChartPayload().series as unknown[]][0].slice(0, 1)
    render(<AssistantTelemetryChartCard card={telemetryChartCard({ series: one })} />)
    await screen.findByRole('img', { name: /Alternator/ })
    expect(screen.queryByRole('button', { name: 'X–Y' })).toBeNull()
  })

  it('pairs per compare range and sums the counts', async () => {
    const card = telemetryChartCard({
      view: 'xy',
      xy: { x: 1, y: 0 },
      compare: [
        { label: 'Before', from: '2026-10-01T00:00:00+10:00', to: '2026-10-05T00:00:00+10:00' },
        { label: 'After', from: '2026-10-12T00:00:00+10:00', to: '2026-10-16T00:00:00+10:00' },
      ],
    })
    render(<AssistantTelemetryChartCard card={card} />)
    expect(await screen.findByText('8 of 12 buckets paired')).toBeInTheDocument()
  })
})

describe('AssistantCard: telemetry_chart', () => {
  it('draws no chart and asks for no data for an error entry', () => {
    render(<AssistantCard card={{ kind: 'error', reason: 'message card 0: xy: expected indices', rawKind: 'telemetry_chart' }} />)
    const err = screen.getByTestId('assistant-card-error')
    expect(within(err).getByText(/xy: expected indices/)).toBeInTheDocument()
    expect(screen.queryByTestId('assistant-telemetry-chart-card')).toBeNull()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('renders the chart card for a telemetry_chart', async () => {
    render(<AssistantCard card={telemetryChartCard()} />)
    expect(await screen.findByTestId('assistant-telemetry-chart-card')).toBeInTheDocument()
  })
})

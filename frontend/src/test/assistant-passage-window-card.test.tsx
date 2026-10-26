import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useState } from 'react'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import { AssistantPassageWindowCard } from '@/components/assistant-passage-window-card'
import { decodeMateCards, type PassageDecision, type PassageWindowCard } from '@/lib/mate-cards'

import { stubApi } from './helpers/stub-api'
import { passageWindowCard, passageWindowPayload } from './helpers/passage-window-fixture'

const toastError = vi.hoisted(() => vi.fn())
vi.mock('sonner', () => ({ toast: { error: toastError } }))

describe('AssistantPassageWindowCard', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-19T10:00:00+10:00'))
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows the header and the forecast time', () => {
    render(<AssistantPassageWindowCard card={passageWindowCard()} />)
    expect(screen.getByText('Passage window')).toBeInTheDocument()
    expect(screen.getByText(/Forecast as of Mon 09:00/)).toBeInTheDocument()
  })

  it('shows state words, leg headers with stays and limiting text', () => {
    render(<AssistantPassageWindowCard card={passageWindowCard()} />)
    const table = screen.getByRole('table')
    expect(within(table).getAllByText('NO-GO').length).toBeGreaterThan(0)
    expect(within(table).getAllByText('GO').length).toBeGreaterThan(0)
    expect(within(table).getAllByText('MARGINAL').length).toBeGreaterThan(0)
    expect(within(table).getAllByText('NO DATA').length).toBeGreaterThan(0)
    expect(within(table).getAllByText('Townsville → Cape Upstart')[0]).toBeInTheDocument()
    expect(within(table).getByText('2 nights')).toBeInTheDocument()
    expect(within(table).queryByText(/1 night/)).toBeNull()
    expect(within(table).getByText('Wind SE 27 kn ahead · Cape Bowling Green → Cape Upstart')).toBeInTheDocument()
    expect(within(table).getByText('Fri 23 Oct')).toBeInTheDocument()
  })

  it('says 1 night for a one-night stay', () => {
    const card = passageWindowCard()
    card.legs[0] = { ...card.legs[0], stayNightsAfter: 1 }
    render(<AssistantPassageWindowCard card={card} />)
    expect(screen.getByText('1 night')).toBeInTheDocument()
  })

  it('puts depart and arrive local times in the cell title', () => {
    render(<AssistantPassageWindowCard card={passageWindowCard()} />)
    const titled = screen.getAllByTitle(/06:00 → 14:30/)
    expect(titled[0].getAttribute('title')).toContain('Wind SE 27 kn ahead')
  })

  it('shows depart and arrive times visibly in each cell', () => {
    render(<AssistantPassageWindowCard card={passageWindowCard()} />)
    expect(screen.getAllByText('06:00 → 14:30').length).toBe(2)
  })

  it('prefixes the departure weekday on a later leg that sails on another day than its row', () => {
    render(<AssistantPassageWindowCard card={passageWindowCard()} />)
    // Row 23 Oct, leg 1 sails on 25 Oct (Sun); row 24 Oct, leg 1 on 26 Oct (Mon).
    expect(screen.getAllByText('Sun 06:00 → 12:00').length).toBe(1)
    expect(screen.getAllByText('Mon 06:00 → 12:00').length).toBe(1)
    expect(screen.getByTitle('Sun 06:00 → 12:00')).toBeInTheDocument()
    // The first leg departs on its row's date and stays bare.
    expect(screen.queryByText(/Fri 06:00 → 14:30/)).toBeNull()
  })

  it('puts the departure weekday before an overnight arrival suffix on a later leg', () => {
    const card = passageWindowCard()
    card.rows[0].cells[1].depart = '2026-10-25T20:00:00+10:00'
    card.rows[0].cells[1].arrive = '2026-10-26T02:10:00+10:00'
    render(<AssistantPassageWindowCard card={card} />)
    expect(screen.getByText('Sun 20:00 → 02:10 Mon')).toBeInTheDocument()
  })

  it('adds the weekday when arrival is on a later local date', () => {
    const card = passageWindowCard()
    card.rows[0].cells[0].depart = '2026-10-23T20:00:00+10:00'
    card.rows[0].cells[0].arrive = '2026-10-24T02:10:00+10:00'
    render(<AssistantPassageWindowCard card={card} />)
    expect(screen.getByText('20:00 → 02:10 Sat')).toBeInTheDocument()
  })

  it('names the stay after the leg in the per-cell leg label', () => {
    render(<AssistantPassageWindowCard card={passageWindowCard()} />)
    expect(screen.getAllByText('Townsville → Cape Upstart · 2 nights').length).toBe(2)
    expect(screen.getAllByText('Cape Upstart → Bowen').length).toBeGreaterThan(0)
  })

  it('shows the note for a no-data cell', () => {
    render(<AssistantPassageWindowCard card={passageWindowCard()} />)
    expect(screen.getByText('No forecast this far ahead')).toBeInTheDocument()
  })

  it('summarises what was not checked as one line', () => {
    render(<AssistantPassageWindowCard card={passageWindowCard()} />)
    expect(screen.getByText('Not checked: wave height, wind against tide')).toBeInTheDocument()
  })

  it('words a not-checked wave direction', () => {
    const card: PassageWindowCard = passageWindowCard()
    for (const r of card.rows) for (const c of r.cells) c.notChecked = ['wave_direction', 'gust']
    render(<AssistantPassageWindowCard card={card} />)
    expect(screen.getByText('Not checked: gusts, wave direction')).toBeInTheDocument()
  })

  it('omits the not-checked line when everything was checked', () => {
    const card: PassageWindowCard = passageWindowCard()
    for (const r of card.rows) for (const c of r.cells) c.notChecked = []
    render(<AssistantPassageWindowCard card={card} />)
    expect(screen.queryByText(/Not checked/)).toBeNull()
  })

  it('flags a forecast older than 6 h and not a fresh one', () => {
    const { unmount } = render(<AssistantPassageWindowCard card={passageWindowCard()} />)
    expect(screen.queryByText('Forecast may have changed')).toBeNull()
    unmount()
    vi.setSystemTime(new Date('2026-10-19T16:30:00+10:00'))
    render(<AssistantPassageWindowCard card={passageWindowCard()} />)
    expect(screen.getByText('Forecast may have changed')).toBeInTheDocument()
  })

  it('decodes and renders the contract payload end to end', () => {
    const [card] = decodeMateCards([{ kind: 'passage_window', payload: passageWindowPayload() }])
    expect(card.kind).toBe('passage_window')
  })
})

async function pick(query: { name: string }) {
  const option = await screen.findByRole('option', query)
  fireEvent.pointerDown(option)
  fireEvent.pointerUp(option)
  fireEvent.click(option)
}

describe('AssistantPassageWindowCard decision', () => {
  const view = (over: Partial<PassageDecision> = {}): PassageDecision => ({
    id: 5,
    state: '',
    rowDate: null,
    inferred: false,
    passageId: null,
    ...over,
  })
  const withDecision = (decision?: PassageDecision): PassageWindowCard => ({ ...passageWindowCard(), cardId: 12, decision })

  function Harness({ initial, canWrite = true }: { initial: PassageWindowCard; canWrite?: boolean }) {
    const [card, setCard] = useState(initial)
    return <AssistantPassageWindowCard card={card} canWrite={canWrite} onDecisionChange={(cardId, d) => {
          if (cardId === 12) setCard({ ...card, decision: d })
        }} />
  }

  beforeEach(() => {
    toastError.mockClear()
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-19T10:00:00+10:00'))
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('shows no control and no label without a decision', () => {
    render(<Harness initial={withDecision(undefined)} />)
    expect(screen.queryByRole('combobox')).toBeNull()
    expect(screen.queryByText(/what you did/i)).toBeNull()
  })

  it('offers one Went option per row, then Stayed, Just looking and Not set', async () => {
    render(<Harness initial={withDecision(view())} />)
    expect(screen.getByRole('combobox')).toHaveTextContent('Not set')
    fireEvent.click(screen.getByRole('combobox'))
    const options = await screen.findAllByRole('option')
    expect(options.map((o) => o.textContent)).toEqual(['Went · Fri 23 Oct', 'Went · Sat 24 Oct', 'Stayed', 'Just looking', 'Not set'])
  })

  it('appends (inferred) to the trigger only when inferred', () => {
    const { unmount } = render(<Harness initial={withDecision(view({ state: 'went', rowDate: '2026-10-24', inferred: true }))} />)
    expect(screen.getByRole('combobox')).toHaveTextContent('Went · Sat 24 Oct (inferred)')
    unmount()
    render(<Harness initial={withDecision(view({ state: 'stayed' }))} />)
    expect(screen.getByRole('combobox')).toHaveTextContent(/^Stayed$/)
  })

  it('sends the PUT, then shows the answer the server gave', async () => {
    const mock = stubApi({
      'PUT /api/passage-decisions/5': () => ({ decision: { id: 5, state: 'went', row_date: '2026-10-24', inferred: false, passage_id: null } }),
    })
    render(<Harness initial={withDecision(view())} />)
    fireEvent.click(screen.getByRole('combobox'))
    await pick({ name: 'Went · Sat 24 Oct' })
    await waitFor(() => expect(screen.getByRole('combobox')).toHaveTextContent('Went · Sat 24 Oct'))
    const put = mock.mock.calls.find(([, init]) => init?.method === 'PUT')
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({ state: 'went', row_date: '2026-10-24' })
  })

  it('sends only the state for Stayed and Not set', async () => {
    const mock = stubApi({
      'PUT /api/passage-decisions/5': () => ({ decision: { id: 5, state: 'stayed', row_date: null, inferred: false, passage_id: null } }),
    })
    render(<Harness initial={withDecision(view())} />)
    fireEvent.click(screen.getByRole('combobox'))
    await pick({ name: 'Stayed' })
    await waitFor(() => expect(screen.getByRole('combobox')).toHaveTextContent('Stayed'))
    expect(JSON.parse(String(mock.mock.calls.find(([, i]) => i?.method === 'PUT')?.[1]?.body))).toEqual({ state: 'stayed' })
  })

  it('toasts the server message and leaves the choice alone when the PUT fails', async () => {
    stubApi({ 'PUT /api/passage-decisions/5': () => new Response(JSON.stringify({ error: 'not a row of this card' }), { status: 400 }) })
    render(<Harness initial={withDecision(view())} />)
    fireEvent.click(screen.getByRole('combobox'))
    await pick({ name: 'Just looking' })
    await waitFor(() => expect(toastError).toHaveBeenCalled())
    expect(JSON.stringify(toastError.mock.calls[0])).toContain('not a row of this card')
    expect(screen.getByRole('combobox')).toHaveTextContent('Not set')
  })

  it('reports the card id, not the decision id, with the answer', async () => {
    stubApi({
      'PUT /api/passage-decisions/5': () => ({ decision: { id: 5, state: 'stayed', row_date: null, inferred: false, passage_id: null } }),
    })
    const onDecisionChange = vi.fn()
    render(<AssistantPassageWindowCard card={withDecision(view())} canWrite onDecisionChange={onDecisionChange} />)
    fireEvent.click(screen.getByRole('combobox'))
    await pick({ name: 'Stayed' })
    await waitFor(() => expect(onDecisionChange).toHaveBeenCalled())
    expect(onDecisionChange.mock.calls[0][0]).toBe(12)
    expect(onDecisionChange.mock.calls[0][1]).toMatchObject({ id: 5, state: 'stayed' })
  })

  it('says the saved answer could not be read, and asks for a reload, when the answer is malformed', async () => {
    stubApi({ 'PUT /api/passage-decisions/5': () => ({ decision: { id: 5, state: 'sailed' } }) })
    const onUnreadable = vi.fn()
    render(<AssistantPassageWindowCard card={withDecision(view())} canWrite onDecisionChange={vi.fn()} onDecisionUnreadable={onUnreadable} />)
    fireEvent.click(screen.getByRole('combobox'))
    await pick({ name: 'Stayed' })
    await waitFor(() => expect(toastError).toHaveBeenCalled())
    expect(JSON.stringify(toastError.mock.calls[0])).toMatch(/saved/i)
    expect(JSON.stringify(toastError.mock.calls[0])).not.toContain('Could not save')
    expect(onUnreadable).toHaveBeenCalled()
  })

  it('read-only shows the same text with no control', () => {
    render(<Harness canWrite={false} initial={withDecision(view({ state: 'went', rowDate: '2026-10-23', inferred: true }))} />)
    expect(screen.queryByRole('combobox')).toBeNull()
    expect(screen.getByText('Went · Fri 23 Oct (inferred)')).toBeInTheDocument()
  })

  it('marks the chosen row header WENT and no other', () => {
    render(<Harness initial={withDecision(view({ state: 'went', rowDate: '2026-10-24' }))} />)
    const rows = screen.getAllByRole('row').filter((r) => r.querySelector('[role="rowheader"]'))
    expect(within(rows[0]).queryByText('WENT')).toBeNull()
    expect(within(rows[1]).getByText('WENT')).toBeInTheDocument()
  })

  it('has no WENT chip when stayed', () => {
    render(<Harness initial={withDecision(view({ state: 'stayed' }))} />)
    expect(screen.queryByText('WENT')).toBeNull()
  })
})

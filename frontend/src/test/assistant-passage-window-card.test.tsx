import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'

import { AssistantPassageWindowCard } from '@/components/assistant-passage-window-card'
import { decodeMateCards, type PassageWindowCard } from '@/lib/mate-cards'

import { passageWindowCard, passageWindowPayload } from './helpers/passage-window-fixture'

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
    expect(screen.getAllByText('06:00 → 12:00').length).toBe(2)
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

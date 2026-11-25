import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { MateActivity } from '@/components/mate-activity'
import { resetQueryStore } from '@/lib/api-query'
import { applyMateEvent, emptyTimeline, type MateTimeline } from '@/lib/mate-activity'
import type { MateEvent } from '@/lib/mate-events'
import type { RunApi } from '@/lib/mate-run'

const foot = { prompt_tokens: 900, completion_tokens: 40, cached_tokens: 600, cost_usd: 0.004 }
const budget = { max_rounds: 30, max_stalled_rounds: 2, tool_phase_ms: 600000, final_reserve_ms: 120000 }

function timeline(events: Array<MateEvent | { type: 'stopped' }>): MateTimeline {
  return events.reduce(applyMateEvent, emptyTimeline())
}

const round = (n: number, at_ms = 0, final = false): MateEvent => ({ type: 'round', round: n, at_ms, final })
const reasoning = (text: string, n = 0): MateEvent => ({ type: 'reasoning', round: n, text })
const lookup = (seq: number, text: string, n = 0): MateEvent => ({ type: 'lookup', round: n, seq, at_ms: 10, text, subject: text.replace(/^(Looking up|Fetching|Reading) /, '').replace(/…$/, ''), skipped: '' })
const lookupDone = (seq: number, reason = '', outcome: 'done' | 'failed' | 'cut' | 'stopped' = reason === '' ? 'done' : 'failed'): MateEvent => ({
  type: 'lookup_done', round: 0, seq, at_ms: 20, outcome, reason,
})
const answerStart = (at_ms = 12000): MateEvent => ({ type: 'answer_start', round: 0, at_ms })
const end = (n = 0, kind: 'text' | 'private' | 'none' = 'text'): MateEvent => ({
  type: 'round_end', round: n, at_ms: 100, reasoning: kind, reasoning_bytes: 0, ...foot,
})

const working = () => timeline([round(0), reasoning('Need the tide first.'), lookup(0, 'Looking up the tide')])

function renderLive(t: MateTimeline, startedLocal: number | null = null, props: Partial<Parameters<typeof MateActivity>[0]> = {}) {
  return render(<MateActivity live={{ timeline: t, startedLocal }} {...props} />)
}

const traceApi: RunApi = {
  id: 'run-1', conversation_id: 'c1', after_seq: 1, answer_message_id: 'm1', model: 'x', started_at: '2026-11-25T09:00:00Z',
  duration_ms: 20000, stop_reason: 'answered', error: '', round_count: 1, budget, thought_ms: 12000, lookups: 1, reasoning_kind: 'text',
  rounds: [{
    index: 0, kind: 'lookups', start_offset_ms: 0, latency_ms: 4100, prompt_tokens: 900, completion_tokens: 40, cached_tokens: 600, cost_usd: 0.004,
    forced_final: false, progress: true, error: '', check_trigger: '', check_draft: '', check_triggers: [],
    reasoning: 'Need the tide.', reasoning_kind: 'text', reasoning_bytes: 14, chatter: 'Checking.', content_start_ms: null,
    calls: [{ seq: 0, name: 'get_tides', args: '{"lat":-16.9}', elapsed_ms: 90, result_bytes: 300, error: '', cache: '', text: 'Looking up the tide…' }],
  }],
}

function stubFetch(body: unknown, ok = true) {
  const fetchMock = vi.fn().mockResolvedValue({ ok, status: ok ? 200 : 404, json: async () => body })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  resetQueryStore()
})

describe('MateActivity while Mate works', () => {
  it('is open, with a scroll box that holds about eight lines of reasoning and can be reached by keyboard', () => {
    renderLive(working())
    const box = screen.getByLabelText('What Mate is doing')
    expect(box).toHaveAttribute('tabindex', '0')
    expect(box.className).toContain('max-h-[min(12lh,40vh)]')
    expect(box.className).not.toContain('max-h-[4lh]')
    expect(box.className).toContain('overflow-y-auto')
    expect(box).toHaveTextContent('Need the tide first.')
    expect(box).toHaveTextContent('the tide…')
    const toggle = screen.getByRole('button', { name: /working/i })
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(toggle).toHaveAttribute('aria-controls', box.id)
  })

  it('does not make streaming text or the seconds a live region, and announces steps in a hidden status', () => {
    renderLive(working())
    const box = screen.getByLabelText('What Mate is doing')
    expect(box.closest('[aria-live]')).toBeNull()
    expect(box.querySelector('[aria-live],[role="status"],[role="log"]')).toBeNull()
    const status = screen.getByRole('status')
    expect(status).toHaveTextContent('Looking up the tide')
    expect(status.className).toContain('sr-only')
    expect(box).not.toContainElement(status)
  })

  it('shows reasoning as body text: small, relaxed, muted, keeping its line breaks, with no markdown', () => {
    renderLive(timeline([round(0), reasoning('some **bold** words\nsecond line')]))
    const text = screen.getByText(/some/)
    expect(text.tagName).toBe('P')
    expect(text.className).toContain('text-sm')
    expect(text.className).toContain('leading-relaxed')
    expect(text.className).toContain('whitespace-pre-wrap')
    expect(text.className).toContain('text-muted-foreground')
    expect(text.textContent).toBe('some **bold** words\nsecond line')
    expect(text.querySelector('strong')).toBeNull()
  })

  it('sets a **title** line as a heading in the model\'s own words', () => {
    renderLive(timeline([round(0), reasoning('**Advancing the Timeline**\nNeed the tide.')]))
    const heading = screen.getByText('Advancing the Timeline')
    expect(heading.className).toContain('text-sm')
    expect(heading.className).toContain('font-medium')
    expect(heading.className).toContain('text-foreground')
    expect(heading.className).not.toContain('uppercase')
    expect(screen.getByText('Need the tide.').className).toContain('text-muted-foreground')
  })

  it('collapses lookups that came one after another into one line of subjects', () => {
    renderLive(timeline([
      round(0), lookup(0, 'Looking up Cairns…'), lookup(1, 'Fetching tides near Cairns…'), lookupDone(0), lookupDone(1),
    ]))
    const box = screen.getByLabelText('What Mate is doing')
    const burst = within(box).getByRole('button', { name: /Cairns · tides near Cairns/ })
    expect(burst).toHaveTextContent('Cairns · tides near Cairns')
    expect(burst).not.toHaveTextContent('…')
    expect(burst.className).toContain('text-xs')
    expect(burst.className).toContain('text-muted-foreground')
    expect(burst.querySelector('svg')).not.toBeNull()
    expect(within(box).getAllByRole('button')).toHaveLength(1)
  })

  it('opens the burst to one row per lookup, and closes it again', () => {
    renderLive(timeline([
      round(0), lookup(0, 'Looking up Cairns…'), lookup(1, 'Fetching tides near Cairns…'), lookupDone(0), lookupDone(1),
    ]))
    const box = screen.getByLabelText('What Mate is doing')
    const burst = within(box).getByRole('button', { name: /Cairns · tides near Cairns/ })
    expect(burst).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(burst)
    expect(burst).toHaveAttribute('aria-expanded', 'true')
    const list = document.getElementById(burst.getAttribute('aria-controls') ?? '')
    expect(list).not.toBeNull()
    expect(within(list as HTMLElement).getAllByRole('button')).toHaveLength(2)
    expect(list).toHaveTextContent('Looking up Cairns')
    expect(list).not.toHaveTextContent('…')
    fireEvent.click(burst)
    expect(burst).toHaveAttribute('aria-expanded', 'false')
  })

  it('shows a running lookup with its ellipsis and a finished one without, collapsed and open', () => {
    renderLive(timeline([round(0), lookup(0, 'Looking up Cairns…'), lookup(1, 'Looking up Hope Vale…'), lookupDone(0)]))
    const burst = screen.getByRole('button', { name: /Cairns · Hope Vale…/ })
    fireEvent.click(burst)
    const list = document.getElementById(burst.getAttribute('aria-controls') ?? '') as HTMLElement
    expect(list).toHaveTextContent('Looking up Cairns')
    expect(list).toHaveTextContent('Looking up Hope Vale…')
    expect(list).not.toHaveTextContent('Cairns…')
  })

  it('says how many failed in muted words with an icon, and never in red', () => {
    renderLive(timeline([
      round(0), lookup(0, 'Looking up the tide…'), lookup(1, 'Looking up the wind…'), lookup(2, 'Looking up the swell…'), lookup(3, 'Looking up depths…'),
      lookupDone(0, 'station offline'), lookupDone(1, '', 'cut'), lookupDone(2, '', 'stopped'), lookupDone(3),
    ]))
    const box = screen.getByLabelText('What Mate is doing')
    const burst = within(box).getByRole('button', { name: /the tide/ })
    expect(burst).toHaveTextContent('1 failed')
    expect(burst).toHaveTextContent('1 cut off')
    expect(burst).toHaveTextContent('1 stopped')
    expect(burst.querySelectorAll('svg').length).toBeGreaterThanOrEqual(2)
    expect(burst).not.toHaveTextContent('station offline')
    fireEvent.click(burst)
    expect(box).toHaveTextContent('Looking up the tide · failed')
    expect(box).toHaveTextContent('cut off at the time limit')
    expect(box.innerHTML).not.toMatch(/destructive|text-red|text-amber/)
  })

  it('says a skipped lookup was skipped', () => {
    renderLive(timeline([
      round(0),
      { type: 'lookup', round: 0, seq: 0, at_ms: 1, text: 'Looking up the tide', subject: 'the tide', skipped: 'withheld' },
      { type: 'lookup', round: 0, seq: 1, at_ms: 1, text: 'Looking up the wind', subject: 'the wind', skipped: 'round_full' },
    ]))
    expect(screen.getByRole('button', { name: /2 skipped/ })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /2 skipped/ }))
    expect(screen.getAllByText(/skipped, /i)).toHaveLength(2)
  })

  it('splits bursts where reasoning or chatter comes between', () => {
    renderLive(timeline([
      round(0), lookup(0, 'Looking up A…'), { type: 'chatter', round: 0, text: 'Now B.' }, lookup(1, 'Looking up B…'),
    ]))
    const box = screen.getByLabelText('What Mate is doing')
    expect(within(box).getAllByRole('button')).toHaveLength(2)
  })

  describe('Thinking… with the seconds counting', () => {
    beforeEach(() => {
      vi.useFakeTimers()
      vi.setSystemTime(new Date('2026-11-25T10:00:10Z'))
    })

    it('counts from the run clock, not from when the page showed it, and ticks', () => {
      const started = Date.now() - 4000
      renderLive(timeline([round(0, 1000)]), started)
      expect(screen.getByText('Thinking… 3 s')).toBeInTheDocument()
      act(() => {
        vi.advanceTimersByTime(2000)
      })
      expect(screen.getByText('Thinking… 5 s')).toBeInTheDocument()
    })

    it('does not show it while a lookup is running', () => {
      renderLive(working(), Date.now() - 4000)
      expect(screen.queryByText(/Thinking…/)).not.toBeInTheDocument()
    })

    it('shows it without a count until the stream has given the clock', () => {
      renderLive(timeline([round(0)]), null)
      expect(screen.getByText('Thinking…')).toBeInTheDocument()
    })

    it('stops ticking once it is folded', () => {
      const t = timeline([round(0), answerStart()])
      renderLive(t, Date.now() - 4000)
      expect(screen.queryByText(/Thinking…/)).not.toBeInTheDocument()
    })
  })

  describe('a model that keeps its reasoning private', () => {
    it('notes it once, however many rounds', () => {
      const t = timeline([round(0), end(0, 'private'), round(1, 200), end(1, 'private')])
      renderLive(t)
      expect(screen.getAllByText('This model keeps its reasoning private.')).toHaveLength(1)
    })

    it('says nothing about a round that showed no reasoning', () => {
      renderLive(timeline([round(0), lookup(0, 'Looking up the tide'), lookupDone(0), end(0, 'none')]))
      expect(screen.queryByText(/reasoning/)).not.toBeInTheDocument()
    })
  })

  it('says reasoning was cut when the model wrote more than the stream carried', () => {
    renderLive(timeline([round(0), reasoning('short'), { ...end(0), reasoning_bytes: 90000 }]))
    expect(screen.getByText(/The rest of this reasoning was cut/)).toBeInTheDocument()
  })

  it('shows a draft sent back with the draft readable', () => {
    renderLive(timeline([
      round(0), answerStart(), { type: 'delta', text: 'Whites Bay is sheltered.' },
      { type: 'draft_sent_back', round: 0, at_ms: 30, triggers: ['anchorage shelter'], draft: 'Whites Bay is sheltered from the north.' },
    ]))
    const box = screen.getByLabelText('What Mate is doing')
    expect(within(box).getByText('Draft sent back: shelter at the anchorage')).toBeInTheDocument()
    expect(within(box).getByText('Whites Bay is sheltered from the north.')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Draft sent back: shelter at the anchorage')
  })

  it('moves a withdrawn answer into chatter, so nothing vanishes', () => {
    renderLive(timeline([
      round(0), answerStart(), { type: 'delta', text: 'Let me check the tide.' },
      { type: 'answer_withdrawn', round: 0, at_ms: 40, reason: 'lookups' },
    ]))
    expect(screen.getByLabelText('What Mate is doing')).toHaveTextContent('Let me check the tide.')
  })
})

describe('MateActivity folding', () => {
  it('folds on answer_start into one line saying how long Mate thought, what it looked up and what it cost', () => {
    const t = timeline([round(0), lookup(0, 'Looking up the tide'), lookupDone(0), end(0), round(1, 200), answerStart(12000)])
    renderLive(t)
    const toggle = screen.getByRole('button', { name: 'Thought 12 s · 1 lookup · <1¢' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByLabelText('What Mate is doing')).not.toBeInTheDocument()
  })

  it('folds by itself when the answer starts, and unfolds again when the draft is sent back', () => {
    const { rerender } = renderLive(working())
    expect(screen.getByLabelText('What Mate is doing')).toBeInTheDocument()

    const answering = applyMateEvent(working(), answerStart(9000))
    rerender(<MateActivity live={{ timeline: answering, startedLocal: null }} />)
    expect(screen.queryByLabelText('What Mate is doing')).not.toBeInTheDocument()

    const sentBack = applyMateEvent(answering, { type: 'draft_sent_back', round: 0, at_ms: 9500, triggers: ['relative wind'], draft: 'Old draft' })
    rerender(<MateActivity live={{ timeline: sentBack, startedLocal: null }} />)
    expect(screen.getByLabelText('What Mate is doing')).toBeInTheDocument()

    const second = applyMateEvent(sentBack, answerStart(15000))
    rerender(<MateActivity live={{ timeline: second, startedLocal: null }} />)
    expect(screen.queryByLabelText('What Mate is doing')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Thought 15 s/ })).toBeInTheDocument()
  })

  it('leaves it open when the operator opened or closed it themselves this run', () => {
    const { rerender } = renderLive(working())
    fireEvent.click(screen.getByRole('button', { name: /working/i }))
    expect(screen.queryByLabelText('What Mate is doing')).not.toBeInTheDocument()

    rerender(<MateActivity live={{ timeline: applyMateEvent(working(), answerStart()), startedLocal: null }} />)
    expect(screen.queryByLabelText('What Mate is doing')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /Thought/ }))
    expect(screen.getByLabelText('What Mate is doing')).toBeInTheDocument()
    rerender(<MateActivity live={{ timeline: applyMateEvent(applyMateEvent(working(), answerStart()), { type: 'message', message: { id: 'm' } as never, conversation: { id: 'c' } as never }), startedLocal: null }} />)
    expect(screen.getByLabelText('What Mate is doing')).toBeInTheDocument()
  })

  it('opens when the run fails or is stopped, whatever the operator chose before', () => {
    for (const ending of [{ type: 'error', error: 'upstream 502', run_id: 'run-1' } as MateEvent, { type: 'stopped' } as const]) {
      const { rerender, unmount } = renderLive(working())
      fireEvent.click(screen.getByRole('button', { name: /working/i }))
      expect(screen.queryByLabelText('What Mate is doing')).not.toBeInTheDocument()

      rerender(<MateActivity live={{ timeline: applyMateEvent(working(), ending), startedLocal: null }} />)
      expect(screen.getByLabelText('What Mate is doing')).toBeInTheDocument()
      unmount()
    }
  })

  it('opens the folded line to the full timeline with the foot, at its full height', () => {
    const t = timeline([
      round(0), reasoning('Need the tide.'), lookup(0, 'Looking up the tide'), lookupDone(0), end(0),
      round(1, 200), answerStart(), end(1), { type: 'message', message: { id: 'm' } as never, conversation: { id: 'c' } as never, run: { id: 'r', thought_ms: 12000, lookups: 1 } },
    ])
    renderLive(t)
    fireEvent.click(screen.getByRole('button', { name: /Thought 12 s/ }))
    const box = screen.getByLabelText('What Mate is doing')
    expect(box.className).not.toContain('max-h-[4lh]')
    expect(box).toHaveTextContent('Need the tide.')
    expect(box).toHaveTextContent('900 in, 40 out, 600 cached')
    expect(screen.getByRole('button', { name: /Thought 12 s/ })).toHaveAttribute('aria-expanded', 'true')
  })

  it('leaves the cost out of a round that cost nothing', () => {
    const free: MateEvent = { type: 'round_end', round: 0, at_ms: 100, reasoning: 'text', reasoning_bytes: 0, prompt_tokens: 10, completion_tokens: 5, cached_tokens: 0, cost_usd: 0 }
    const t = timeline([
      round(0), reasoning('Need the tide.'), free, round(1, 200), answerStart(),
      { type: 'message', message: { id: 'm' } as never, conversation: { id: 'c' } as never, run: { id: 'r', thought_ms: 12000, lookups: 0 } },
    ])
    renderLive(t)
    fireEvent.click(screen.getByRole('button', { name: /Thought 12 s/ }))
    const box = screen.getByLabelText('What Mate is doing')
    expect(box).toHaveTextContent('10 in, 5 out')
    expect(box).not.toHaveTextContent('<1¢')
  })

  it('animates the fold only when the operator allows motion', () => {
    renderLive(working())
    expect(screen.getByLabelText('What Mate is doing').className).toContain('motion-reduce:animate-none')
    expect(screen.getByRole('button', { name: /working/i }).querySelector('svg')?.getAttribute('class')).toContain('motion-reduce:transition-none')
  })
})

describe('MateActivity when a run does not finish', () => {
  it('stays open when it failed, with the error as the last step', () => {
    const t = timeline([round(0), reasoning('Working it out'), { type: 'error', error: 'upstream 502' }])
    renderLive(t)
    const box = screen.getByLabelText('What Mate is doing')
    expect(box.className).not.toContain('max-h-[4lh]')
    const steps = within(box).getAllByRole('listitem')
    expect(steps[steps.length - 1]).toHaveTextContent('upstream 502')
    expect(screen.getByRole('button', { name: /^Stopped before answering/ })).toHaveAttribute('aria-expanded', 'true')
  })

  it('keeps its hidden status mounted, so the failure and the stop are announced', () => {
    const { rerender } = renderLive(working())
    const status = screen.getByRole('status')
    rerender(<MateActivity live={{ timeline: timeline([round(0), { type: 'error', error: 'upstream 502' }]), startedLocal: null }} />)
    expect(screen.getByRole('status')).toBe(status)
    expect(status).toHaveTextContent('upstream 502')
    rerender(<MateActivity live={{ timeline: timeline([round(0), lookup(0, 'Looking up the tide'), { type: 'stopped' }]), startedLocal: null }} />)
    expect(screen.getByRole('status')).toBe(status)
    expect(status).toHaveTextContent('Stopped')
  })

  it('stays open when stopped, ending in Stopped', () => {
    const t = timeline([round(0), lookup(0, 'Looking up the tide'), { type: 'stopped' }])
    renderLive(t)
    const steps = within(screen.getByLabelText('What Mate is doing')).getAllByRole('listitem')
    expect(steps[steps.length - 1]).toHaveTextContent('Stopped')
  })
})

describe('MateActivity scroll box', () => {
  function setGeometry(el: HTMLElement, geometry: { scrollHeight: number; clientHeight: number; scrollTop: number }) {
    Object.defineProperty(el, 'scrollHeight', { configurable: true, get: () => geometry.scrollHeight })
    Object.defineProperty(el, 'clientHeight', { configurable: true, get: () => geometry.clientHeight })
    el.scrollTop = geometry.scrollTop
  }

  it('stays at the latest step as steps arrive', () => {
    const { rerender } = renderLive(working())
    const box = screen.getByLabelText('What Mate is doing')
    setGeometry(box, { scrollHeight: 400, clientHeight: 100, scrollTop: 300 })

    rerender(<MateActivity live={{ timeline: applyMateEvent(working(), reasoning(' And more.')), startedLocal: null }} />)
    expect(box.scrollTop).toBe(400)
  })

  it('stays where the operator scrolled to once they scroll up', () => {
    const { rerender } = renderLive(working())
    const box = screen.getByLabelText('What Mate is doing')
    setGeometry(box, { scrollHeight: 400, clientHeight: 100, scrollTop: 50 })
    fireEvent.scroll(box)

    rerender(<MateActivity live={{ timeline: applyMateEvent(working(), reasoning(' And more.')), startedLocal: null }} />)
    expect(box.scrollTop).toBe(50)

    // Scrolling back to the end picks it up again.
    box.scrollTop = 300
    fireEvent.scroll(box)
    rerender(<MateActivity live={{ timeline: applyMateEvent(applyMateEvent(working(), reasoning(' And more.')), reasoning(' Again.')), startedLocal: null }} />)
    expect(box.scrollTop).toBe(400)
  })
})

describe('MateActivity at phone width', () => {
  it('lets every row shrink and truncates a lookup until it is tapped, with no reliance on a title', () => {
    renderLive(timeline([round(0), lookup(0, 'Looking up the tide at a place with a very long name indeed, Hinchinbrook Channel')]))
    const burst = screen.getByRole('button', { name: /the tide at a place/ })
    expect(burst.className).toContain('min-w-0')
    expect(burst.className).toContain('break-words')
    fireEvent.click(burst)
    const row = screen.getByRole('button', { name: /Looking up the tide at a place/ })
    expect(row.className).toContain('min-w-0')
    expect(row.querySelector('.truncate')).not.toBeNull()
    expect(row).not.toHaveAttribute('title')
    expect(row).toHaveAttribute('aria-expanded', 'false')

    fireEvent.click(row)
    expect(row).toHaveAttribute('aria-expanded', 'true')
    expect(row.querySelector('.truncate')).toBeNull()
    expect(row.querySelector('.break-words')).not.toBeNull()
  })

  it('says a lookup failed on its line and keeps the reason for the tap', () => {
    renderLive(timeline([round(0), lookup(0, 'Looking up the marina…'), { type: 'lookup_done', round: 0, seq: 0, at_ms: 20, outcome: 'failed', reason: 'no marina charted within 500 m' }]))
    expect(screen.getByRole('button', { name: /the marina/ })).toHaveTextContent('1 failed')
    fireEvent.click(screen.getByRole('button', { name: /the marina/ }))
    const row = screen.getByRole('button', { name: /Looking up the marina/ })
    expect(row).toHaveTextContent('Looking up the marina · failed')
    expect(row).not.toHaveTextContent('no marina charted')
    fireEvent.click(row)
    expect(row).toHaveTextContent('no marina charted within 500 m')
    expect(row).not.toHaveTextContent('marina_details')
  })

  it('keeps the root and the box inside the column', () => {
    const { container } = renderLive(working())
    expect((container.firstElementChild as HTMLElement).className).toContain('min-w-0')
    expect(screen.getByLabelText('What Mate is doing').className).toContain('min-w-0')
  })
})

describe('MateActivity for an answer already delivered', () => {
  it('shows the folded line from the stored run and reads nothing until it is opened', () => {
    const fetchMock = stubFetch(traceApi)
    render(<MateActivity run={{ id: 'run-1', thoughtMs: 12000, lookups: 4 }} costUsd={0.03} />)
    expect(screen.getByRole('button', { name: 'Thought 12 s · 4 lookups · 3¢' })).toHaveAttribute('aria-expanded', 'false')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('reads the trace on the first open, shows the steps and the foot, and reads it once', async () => {
    const fetchMock = stubFetch(traceApi)
    render(<MateActivity run={{ id: 'run-1', thoughtMs: 12000, lookups: 1 }} costUsd={0.004} />)
    const toggle = screen.getByRole('button', { name: /Thought 12 s/ })
    fireEvent.click(toggle)
    const box = await screen.findByLabelText('What Mate is doing')
    await waitFor(() => expect(box).toHaveTextContent('Need the tide.'))
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/api/assistant/runs/run-1'), expect.anything())
    expect(box).toHaveTextContent('Checking.')
    expect(box).toHaveTextContent('the tide')
    expect(box).not.toHaveTextContent('Looking up the tide…')
    expect(box).not.toHaveTextContent('get_tides')
    expect(box).toHaveTextContent('900 in, 40 out, 600 cached')
    expect(box).toHaveTextContent('Mate answered when it was ready.')
    expect(box).toHaveTextContent('Up to 30 rounds and 10 min of lookups')

    fireEvent.click(toggle)
    fireEvent.click(toggle)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('shows an old run with no time in its line', () => {
    render(<MateActivity run={{ id: 'run-1', thoughtMs: null, lookups: 2 }} costUsd={0.05} />)
    expect(screen.getByRole('button', { name: '2 lookups · 5¢' })).toBeInTheDocument()
  })

  it('says why the trace could not be read instead of showing nothing', async () => {
    stubFetch({ error: 'run not found' }, false)
    render(<MateActivity run={{ id: 'run-1', thoughtMs: 1000, lookups: 0 }} />)
    fireEvent.click(screen.getByRole('button', { name: /Thought 1 s/ }))
    expect(await screen.findByRole('alert')).toHaveTextContent('run not found')
  })

  it('says Mate showed no reasoning only when no round of the run showed any', async () => {
    stubFetch({ ...traceApi, rounds: [{ ...traceApi.rounds![0], reasoning: '', reasoning_kind: 'none', reasoning_bytes: 0 }] })
    render(<MateActivity run={{ id: 'run-1', thoughtMs: 1000, lookups: 1 }} />)
    fireEvent.click(screen.getByRole('button', { name: /Thought 1 s/ }))
    expect(await screen.findByText(/Mate didn't show any reasoning/)).toBeInTheDocument()
  })

  it('does not say it for an old run that never recorded reasoning', async () => {
    stubFetch({ ...traceApi, rounds: [{ ...traceApi.rounds![0], reasoning: '', reasoning_kind: '', reasoning_bytes: 0 }] })
    render(<MateActivity run={{ id: 'run-1', thoughtMs: null, lookups: 1 }} />)
    fireEvent.click(screen.getByRole('button', { name: /1 lookup/ }))
    await screen.findByLabelText('What Mate is doing')
    expect(screen.queryByText(/didn't show any reasoning/)).not.toBeInTheDocument()
  })

  it('opens at once and reads the trace for a run that failed', async () => {
    const fetchMock = stubFetch({
      ...traceApi, answer_message_id: null, stop_reason: 'time_ceiling', error: 'context deadline exceeded',
      rounds: [{ ...traceApi.rounds![0], kind: 'failed', error: 'context deadline exceeded' }],
    })
    render(<MateActivity run={{ id: 'run-1', thoughtMs: null, lookups: 1 }} startOpen />)
    const box = await screen.findByLabelText('What Mate is doing')
    expect(fetchMock).toHaveBeenCalled()
    await waitFor(() => {
      const steps = within(box).getAllByRole('listitem')
      expect(steps[steps.length - 1]).toHaveTextContent('context deadline exceeded')
    })
  })

  describe('a failed run with a headline above it', () => {
    const headline = { text: 'Mate stopped before answering: it ran out of time after 1 round.', tone: 'error' as const }
    const failedTrace = (error: string) => ({
      ...traceApi, answer_message_id: null, stop_reason: 'time_ceiling', error,
      rounds: [{ ...traceApi.rounds![0], kind: 'failed', error: '' }],
    })

    it('says the sentence once when the error step would repeat it', async () => {
      stubFetch(failedTrace(''))
      render(<MateActivity run={{ id: 'run-1', thoughtMs: null, lookups: 1 }} startOpen headline={headline} />)
      const box = await screen.findByLabelText('What Mate is doing')
      await waitFor(() => expect(within(box).getByText(headline.text)).toBeInTheDocument())
      expect(screen.getAllByText(headline.text)).toHaveLength(1)
      expect(within(box).getAllByRole('listitem').at(-1)).toHaveTextContent(headline.text)
    })

    it('keeps the headline above and the cause as the last step, without repeating the headline in the foot', async () => {
      stubFetch(failedTrace('read openrouter stream: context deadline exceeded'))
      render(<MateActivity run={{ id: 'run-1', thoughtMs: null, lookups: 1 }} startOpen headline={headline} />)
      const box = await screen.findByLabelText('What Mate is doing')
      await waitFor(() => expect(box).toHaveTextContent('read openrouter stream: context deadline exceeded'))
      expect(screen.getAllByText(headline.text)).toHaveLength(1)
      expect(box).not.toHaveTextContent(headline.text)
    })

    it('shows the headline while the trace is still being read', () => {
      vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
      render(<MateActivity run={{ id: 'run-1', thoughtMs: null, lookups: 1 }} startOpen headline={headline} />)
      expect(screen.getByText(headline.text)).toBeInTheDocument()
    })
  })

  it('shows a draft sent back from a saved run, with the reason in the operator\'s words', async () => {
    stubFetch({
      ...traceApi,
      rounds: [{ ...traceApi.rounds![0], kind: 'correction', check_draft: 'A rejected draft.', check_trigger: 'relative wind', check_triggers: ['relative wind'] }],
    })
    render(<MateActivity run={{ id: 'run-1', thoughtMs: 12000, lookups: 1 }} />)
    fireEvent.click(screen.getByRole('button', { name: /Thought 12 s/ }))
    expect(await screen.findByText('Draft sent back: wind angle wrong for the course')).toBeInTheDocument()
    expect(screen.getByText('A rejected draft.')).toBeInTheDocument()
  })

  it('shows the live steps when it has them and does not read the trace', () => {
    const fetchMock = stubFetch(traceApi)
    const t = timeline([
      round(0), reasoning('Live reasoning.'), end(0), round(1, 100), answerStart(5000),
      { type: 'message', message: { id: 'm' } as never, conversation: { id: 'c' } as never, run: { id: 'run-1', thought_ms: 5000, lookups: 0 } },
    ])
    render(<MateActivity live={{ timeline: t, startedLocal: null }} run={{ id: 'run-1', thoughtMs: 5000, lookups: 0 }} />)
    fireEvent.click(screen.getByRole('button', { name: /Thought 5 s/ }))
    expect(screen.getByLabelText('What Mate is doing')).toHaveTextContent('Live reasoning.')
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

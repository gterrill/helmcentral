import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import { AssistantRunDetails } from '@/components/assistant-run-details'
import { resetQueryStore } from '@/lib/api-query'
import type { RunApi } from '@/lib/mate-run'

const budget = { max_rounds: 30, max_stalled_rounds: 2, tool_phase_ms: 600000, final_reserve_ms: 120000 }

const traceApi: RunApi = {
  id: 'run-1',
  conversation_id: 'c1',
  after_seq: 1,
  answer_message_id: 'm1',
  model: 'anthropic/claude-sonnet-4.5',
  started_at: '2026-10-31T09:00:00Z',
  duration_ms: 133000,
  stop_reason: 'stalled',
  error: '',
  round_count: 2,
  budget,
  rounds: [
    {
      index: 0, kind: 'lookups', start_offset_ms: 0, latency_ms: 4100, prompt_tokens: 900, completion_tokens: 40, cached_tokens: 600, cost_usd: 0.004,
      forced_final: false, progress: true, error: '',
      calls: [
        { seq: 0, name: 'find_places', args: '{"query":"Cairns"}', elapsed_ms: 2300, result_bytes: 812, error: '', cache: 'miss' },
        { seq: 1, name: 'find_places', args: '{"query":"Dunk Island"}', elapsed_ms: 3, result_bytes: 400, error: '', cache: 'hit' },
        { seq: 2, name: 'get_tides', args: '{"lat":1}', elapsed_ms: 90, result_bytes: 0, error: 'tide station offline', cache: '' },
      ],
    },
    {
      index: 1, kind: 'lookups', start_offset_ms: 72000, latency_ms: 9000, prompt_tokens: 1200, completion_tokens: 30, cached_tokens: 0, cost_usd: 0.006,
      forced_final: false, progress: false, error: '', calls: [{ seq: 0, name: 'get_tides', args: '{"lat":1}', elapsed_ms: 10, result_bytes: 0, error: 'withheld', cache: '' }],
    },
    {
      index: 2, kind: 'final', start_offset_ms: 90000, latency_ms: 30000, prompt_tokens: 1300, completion_tokens: 500, cached_tokens: 0, cost_usd: 0.02,
      forced_final: true, progress: false, error: '', calls: [],
    },
  ],
}

afterEach(() => {
  vi.unstubAllGlobals()
  resetQueryStore()
})

function stubFetch(response: { ok: boolean; status?: number; body: unknown }) {
  const fetchMock = vi.fn().mockResolvedValue({
    ok: response.ok,
    status: response.status ?? (response.ok ? 200 : 500),
    json: async () => response.body,
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('AssistantRunDetails', () => {
  it('reads nothing until it is opened, then shows the rounds, timings and lookups', async () => {
    const fetchMock = stubFetch({ ok: true, body: traceApi })
    render(<AssistantRunDetails runId="run-1" />)
    expect(fetchMock).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Run details' }))

    const rounds = await screen.findAllByTestId('run-round')
    expect(fetchMock).toHaveBeenCalledWith(expect.stringContaining('/api/assistant/runs/run-1'), expect.anything())
    expect(rounds).toHaveLength(3)
    expect(screen.getByText(/The last 2 rounds found nothing new, so Mate was asked to answer\. 2 rounds of lookups in/)).toBeInTheDocument()
    expect(screen.getByText(/Up to 30 rounds and 10 min of lookups/)).toBeInTheDocument()

    const first = within(rounds[0])
    expect(first.getByText('Round 1')).toBeInTheDocument()
    expect(first.getByText('4.1 s for the reply')).toBeInTheDocument()
    expect(first.getByText(/900 in, 40 out, 600 cached/)).toBeInTheDocument()
    expect(first.getByText('progress')).toBeInTheDocument()
    expect(first.getByText(/2\.3 s · 812 B · looked up/)).toBeInTheDocument()
    expect(first.getByText(/3 ms · 400 B · from memory/)).toBeInTheDocument()
    expect(first.getByText('tide station offline')).toBeInTheDocument()

    expect(within(rounds[1]).getByText('nothing new')).toBeInTheDocument()
    expect(within(rounds[1]).getByText('+1:12')).toBeInTheDocument()
    expect(within(rounds[2]).getByText('final answer')).toBeInTheDocument()
  })

  it('says when a round failed and why', async () => {
    stubFetch({
      ok: true,
      body: {
        ...traceApi,
        answer_message_id: null,
        stop_reason: 'time_ceiling',
        rounds: [{ ...traceApi.rounds![2], kind: 'failed', error: 'context deadline exceeded' }],
      },
    })
    render(<AssistantRunDetails runId="run-1" />)
    fireEvent.click(screen.getByRole('button', { name: 'Run details' }))

    const round = await screen.findByTestId('run-round')
    expect(within(round).getByText('failed')).toBeInTheDocument()
    expect(within(round).getAllByText('context deadline exceeded').length).toBeGreaterThan(0)
    expect(screen.getByText(/Mate stopped before answering: it ran out of time after 2 rounds\./)).toBeInTheDocument()
  })

  it('labels a round by what happened in it', async () => {
    const base = traceApi.rounds![2]
    stubFetch({
      ok: true,
      body: {
        ...traceApi,
        answer_message_id: null,
        rounds: [
          { ...base, index: 0, kind: 'cut', error: 'context deadline exceeded' },
          { ...base, index: 1, kind: 'correction', forced_final: false },
          { ...base, index: 2, kind: 'answer', forced_final: false },
        ],
      },
    })
    render(<AssistantRunDetails runId="run-1" />)
    fireEvent.click(screen.getByRole('button', { name: 'Run details' }))

    const rounds = await screen.findAllByTestId('run-round')
    expect(within(rounds[0]).getByText('cut off by time')).toBeInTheDocument()
    expect(within(rounds[1]).getByText('sent back for correction')).toBeInTheDocument()
    expect(within(rounds[2]).getByText('answer')).toBeInTheDocument()
    expect(screen.getByText(/It ran for/)).toHaveTextContent('It ran for 2 min 13 s.')
  })

  it('reports a failure to read the trace instead of showing nothing', async () => {
    stubFetch({ ok: false, status: 404, body: { error: 'run not found' } })
    render(<AssistantRunDetails runId="run-1" />)
    fireEvent.click(screen.getByRole('button', { name: 'Run details' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('run not found')
  })

  it('closes again without reading twice', async () => {
    const fetchMock = stubFetch({ ok: true, body: traceApi })
    render(<AssistantRunDetails runId="run-1" />)
    const button = screen.getByRole('button', { name: 'Run details' })
    fireEvent.click(button)
    await screen.findAllByTestId('run-round')
    fireEvent.click(button)
    expect(screen.queryByTestId('run-round')).not.toBeInTheDocument()
    fireEvent.click(button)
    await waitFor(() => expect(screen.getAllByTestId('run-round')).toHaveLength(3))
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})

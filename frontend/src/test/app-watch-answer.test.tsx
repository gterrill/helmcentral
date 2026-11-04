/**
 * A watch report's question has an Answer button. Through the real App, the
 * real Watches page and the real Mate sheet: it opens a new chat tied to the
 * run with the text in the composer, focused, and sends nothing until the
 * skipper does. Only the data hooks around the shell are stubbed, as in
 * app-mate-voice.test.tsx.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'

import { App } from '../App'
import { landAnchorWatch } from './helpers/anchor-watch-poll'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

vi.mock('@/hooks/use-auth', () => ({
  refreshAuthState: vi.fn().mockResolvedValue({ mode: 'none', user: null }),
  useAuth: () => ({
    mode: 'signalk' as const,
    user: { username: 'skipper' },
    role: 'readwrite',
    loading: false,
    error: null,
    login: vi.fn(),
    logout: vi.fn(),
  }),
}))

vi.mock('@/hooks/use-app-config', () => ({
  useAppConfig: () => ({
    ui: { distanceUnits: 'metric', autoCloseAnchorWatchOnEngine: true },
    anchor: {
      bowRollerHeightM: 0, chainSizeMm: 10, chainOnboardM: 50,
      hullType: 'power_cat', scopeMethod: 'ratio', windageAreaM2: 10,
      gpsFromBowM: 0, loaM: 0,
    },
    assistant: { voiceInput: false, readAloud: false, wakeWord: false },
    loaded: true,
  }),
  publishAppConfigSettings: vi.fn(),
}))

vi.mock('@/hooks/use-place-name', () => ({ usePlaceName: () => null }))
vi.mock('@/hooks/use-tide-today', () => ({ useTideToday: () => ({ tide: null }) }))
vi.mock('@/hooks/use-weather-today', () => ({ useWeatherToday: () => ({ weather: null }) }))
vi.mock('@/hooks/use-weather-forecast', () => ({
  useForecastSunTimes: () => ({ sunriseTime: null, sunsetTime: null }),
  useForecastSun: () => ({ sunriseTime: null, sunsetTime: null, moonPhase: null }),
  useWeatherForecast: () => ({ forecast: [], loading: false, error: null, provider: null, refetch: vi.fn() }),
}))
vi.mock('@/hooks/use-wave-forecast', () => ({
  useWaveForecast: () => ({ days: [], seaTemperatureF: null, provider: null, loading: false, error: null, refetch: vi.fn() }),
}))
vi.mock('@/hooks/use-czone-switches', () => ({
  useCZoneSwitches: () => ({ switches: [], loading: false, pending: {}, toggleSwitch: vi.fn() }),
}))
vi.mock('@/hooks/use-depth-trend', () => ({ useDepthTrend: () => ({ points: [], since: 'window' }) }))
vi.mock('@/hooks/use-route-activation', () => {
  const result = { status: { state: 'inactive', destination: null } }
  return { useRouteActivation: () => result }
})
vi.mock('@/hooks/use-forecast-warnings', () => ({
  useHasActiveWindBulletin: () => false,
  useForecastWarnings: () => ({ activeWarning: null }),
  findActiveWindBulletin: () => null,
  forecastWarningDetailsUrl: () => null,
}))
vi.mock('@/hooks/use-server-trails', async () => (await import('./trail-mocks')).trailHooksMock)
vi.mock('@/hooks/use-dark-mode', () => ({ useDarkMode: () => [false, vi.fn()] }))
vi.mock('@/hooks/use-alarms', () => ({
  useAlarmListComparedOn: () => [],
  sameShownAlarm: () => true,
  sameLampAlarm: () => true,
  samePillAlarm: () => true,
  useKnownAlarmList: () => [],
  useWorstAlarmState: () => 'normal',
  useAisCollisionAlarms: () => new Map(),
  useAnchorDragAlarm: () => null,
  acknowledgeAlarm: vi.fn(),
  silenceAlarm: vi.fn(),
  collisionAlarmStatesByVessel: () => new Map(),
}))
vi.mock('@/hooks/use-version-reload', () => ({
  useVersionReload: () => ({ current: null, available: null, chunkFailure: null, readError: null, reportChunkFailure: () => {}, clearChunkFailure: () => {}, retryHeldReload: () => {} }),
}))

installFakeTelemetryStream({ seeds: { 'vessel-state': {} } })
beforeEach(() => { landAnchorWatch() })

const QUESTION = 'Did you run the watermaker?'
const DRAFT = `About "${QUESTION}": `

const watch = {
  id: 'w1', name: 'Morning report', kind: 'scheduled', schedule: '0 6 * * *', sections: ['power'],
  thresholds: { alarms_raised: 1, instrument_issues: 1, tank_change_points: 10, pressure_fall_hpa: 3 },
  look_out_for: '', enabled: true, notify: [], send: 'findings', updated_at: '2026-10-29T00:00:00Z',
  schedule_sentence: 'Every day at 6.00 AM', last_run: null, has_finding: false,
}
const watchRun = {
  id: 'run-9', watch_id: 'w1', started_at: '2026-10-28T17:00:00Z', finished_at: '2026-10-28T17:00:30Z',
  boat_time: 'Thu 29 Oct 06:00 UTC+13', timezone: 'UTC+13', outcome: 'ran', report_kind: 'sections',
  report: { kind: 'sections', generated_at: '', boat_time: '', since: 'Wed 28 Oct 22:00 UTC+13', sections: [{ name: 'power', title: 'Power', lines: ['House bank 84%'] }] },
  findings: [], review: 'Quiet night. Nothing to add.', review_opening: 'Quiet night.', review_rest: 'Nothing to add.',
  review_problem: null, cost_usd: 0, cost_known: true, notified: [], questions: [QUESTION],
}

let posts: Array<{ url: string; body: Record<string, unknown> }>

function stubFetch() {
  posts = []
  let created = 0
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const method = (init?.method ?? 'GET').toUpperCase()
    const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : {}
    const ok = (data: unknown, status = 200) => ({ ok: true, status, json: async () => data })
    if (method === 'POST') posts.push({ url, body })
    if (url.endsWith('/api/health')) return ok({ status: 'ok', version: 'v0.20.0', revision: 'deadbeef' })
    if (url.endsWith('/api/assistant/status')) return ok({ enabled: true, configured: true, model: 'm' })
    if (url.endsWith('/api/assistant/conversations') && method === 'GET') return ok({ conversations: [] })
    if (url.endsWith('/api/assistant/conversations') && method === 'POST') {
      created += 1
      return ok({ id: `new-${created}`, title: 'New conversation', created_at: '', updated_at: '' }, 201)
    }
    if (/\/api\/assistant\/conversations\/[^/]+\/messages$/.test(url) && method === 'POST') {
      return { ok: false, status: 503, json: async () => ({ error: 'Mate is not set up in this test.' }) }
    }
    if (/\/api\/assistant\/conversations\/[^/]+\/(run|watch)$/.test(url) && method === 'GET') return { ok: true, status: 204, json: async () => ({}) }
    const conversation = url.match(/\/api\/assistant\/conversations\/([^/]+)$/)
    if (conversation && method === 'GET') {
      return ok({ conversation: { id: conversation[1], title: 'x', created_at: '', updated_at: '' }, messages: [] })
    }
    if (url.includes('/schedule-preview')) return ok({ valid: true, sentence: 'Every day at 6.00 AM', boat_time_known: true, next: [] })
    if (url.endsWith('/api/watches/channels')) return ok({ offered: [], enabled: [] })
    if (/\/api\/watches\/w1(\?|$)/.test(url) && method === 'GET') return ok({ watch, runs: [watchRun], has_older: false })
    if (/\/api\/watches(\?|$)/.test(url) && method === 'GET') return ok({ watches: [watch], max: 20, cost_24h: { usd: 0, unknown_runs: 0 }, storage_bytes: 0 })
    return { ok: false, json: async () => ({}) }
  }))
}

describe('Answer on a watch report question, through the app', () => {
  beforeEach(() => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    window.history.replaceState({}, '', '/watches/w1')
    stubFetch()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    window.history.replaceState({}, '', '/')
  })

  it('puts the draft in the focused composer of the Mate sheet without sending, then sends it tied to the run', async () => {
    render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: `Answer: ${QUESTION}` }, { timeout: 5000 }))

    const composer = await screen.findByPlaceholderText('Ask Mate') as HTMLTextAreaElement
    await waitFor(() => expect(composer.value).toBe(DRAFT))
    await waitFor(() => expect(composer).toHaveFocus())
    expect(posts.filter((p) => p.url.endsWith('/messages'))).toHaveLength(0)

    fireEvent.change(composer, { target: { value: `${DRAFT}No, it was off.` } })
    fireEvent.keyDown(composer, { key: 'Enter' })
    await waitFor(() => expect(posts.filter((p) => p.url.endsWith('/messages'))).toHaveLength(1))
    expect(posts.find((p) => p.url.endsWith('/messages'))!.body).toMatchObject({
      content: `${DRAFT}No, it was off.`,
      screen: expect.objectContaining({ watch_run_id: 'run-9', watch_name: 'Morning report' }),
    })
    expect(posts.filter((p) => p.url.endsWith('/api/assistant/conversations')).map((p) => p.body)).toEqual([{ watch_run_id: 'run-9' }])
  })

  it('does not refill the composer when Mate is opened plainly after the draft was left unsent', async () => {
    render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: `Answer: ${QUESTION}` }, { timeout: 5000 }))
    const composer = await screen.findByPlaceholderText('Ask Mate') as HTMLTextAreaElement
    await waitFor(() => expect(composer.value).toBe(DRAFT))

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(screen.queryByPlaceholderText('Ask Mate')).not.toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: 'Ask Mate' }))
    const reopened = await screen.findByPlaceholderText('Ask Mate') as HTMLTextAreaElement
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 50)) })
    expect(reopened.value).toBe('')
  })

  it('ties only the first message of the draft chat to the run', async () => {
    render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: `Answer: ${QUESTION}` }, { timeout: 5000 }))
    const composer = await screen.findByPlaceholderText('Ask Mate') as HTMLTextAreaElement
    await waitFor(() => expect(composer.value).toBe(DRAFT))
    const messagePosts = () => posts.filter((p) => p.url.endsWith('/messages'))

    fireEvent.change(composer, { target: { value: `${DRAFT}No.` } })
    fireEvent.keyDown(composer, { key: 'Enter' })
    await waitFor(() => expect(messagePosts()).toHaveLength(1))
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 50)) })

    fireEvent.change(composer, { target: { value: 'And one more thing.' } })
    fireEvent.keyDown(composer, { key: 'Enter' })
    await waitFor(() => expect(messagePosts()).toHaveLength(2))
    expect(messagePosts()[0].body.screen).toMatchObject({ watch_run_id: 'run-9' })
    expect(messagePosts()[1].body.screen).not.toHaveProperty('watch_run_id')
  })
})

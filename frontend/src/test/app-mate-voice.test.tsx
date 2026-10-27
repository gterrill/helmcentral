/**
 * ADR 0093 voice phase, "App-wide voice": the header mic button and its
 * Alt+M shortcut, mounted once in App.tsx via hooks/use-mate-voice.ts so
 * push-to-talk works on every panel and dashboard page, not just the Mate
 * panel/sheet. Mirrors App.smoke.test.tsx's minimal mock scaffold (most
 * hooks left real, backed by a blanket "not found" fetch stub) plus a
 * FakeSpeechRecognition (same shape as use-speech-input.test.ts's) and a
 * couple of mutable fixtures so voiceInput/canWrite can vary per test.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { landAnchorWatch } from './helpers/anchor-watch-poll'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { App } from '../App'

interface FakeResultAlternative { transcript: string }
interface FakeResult extends Array<FakeResultAlternative> { isFinal: boolean }

class FakeSpeechRecognition {
  lang = ''
  continuous = false
  interimResults = false
  onresult: ((event: { resultIndex: number; results: ArrayLike<FakeResult> }) => void) | null = null
  onerror: ((event: { error: string }) => void) | null = null
  onend: (() => void) | null = null
  aborted = false

  constructor() {
    instances.push(this)
  }

  start() {}
  stop() {}
  abort() { this.aborted = true }

  emitResult(transcript: string, isFinal: boolean) {
    const result: FakeResult = Object.assign([{ transcript }], { isFinal })
    this.onresult?.({ resultIndex: 0, results: [result] })
  }
}

let instances: FakeSpeechRecognition[] = []

function currentRecognition(): FakeSpeechRecognition {
  return instances[instances.length - 1]
}

// Mutable so individual tests can flip voiceInput/canWrite without a
// separate vi.mock per scenario - reset to the common baseline (voice on,
// writable) in beforeEach.
const mockAssistantVoice = { voiceInput: true, readAloud: false, wakeWord: false }
const mockAuth = { canWrite: true }

vi.mock('@/hooks/use-auth', () => ({
  refreshAuthState: vi.fn().mockResolvedValue({ mode: 'none', user: null }),
  useAuth: () => ({
    mode: 'signalk' as const,
    user: { username: 'skipper' },
    role: mockAuth.canWrite ? 'readwrite' : 'read',
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
    assistant: { ...mockAssistantVoice },
    loaded: true,
  }),
  publishAppConfigSettings: vi.fn(),
}))





vi.mock('@/hooks/use-place-name', () => ({ usePlaceName: () => null }))


vi.mock('@/hooks/use-tide-today', () => ({
  useTideToday: () => ({
    tide: {
      current_tide_height_ft: -1, tide_direction: 'n/a',
      high_tide_time: new Date(0).toISOString(), high_tide_height_ft: -1,
      low_tide_time: new Date(0).toISOString(), low_tide_height_ft: -1,
    },
  }),
}))

vi.mock('@/hooks/use-weather-today', () => ({
  useWeatherToday: () => ({
    weather: {
      temperature_f: -1, condition: 'n/a',
      wind_speed_kts: -1, wind_direction: 'n/a', wind_gust_kts: -1, precipitation_pct: -1,
      provider: '', cached: false, updated_at: '', ttl_seconds: 0,
    },
  }),
}))

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
  // One object, so a re-render does not look like a new route status.
  const result = { status: { state: 'inactive', destination: null } }
  return { useRouteActivation: () => result }
})

vi.mock('@/hooks/use-forecast-warnings', () => ({
  useHasActiveWindBulletin: () => false,
  useForecastWarnings: () => ({ activeWarning: null }),
  findActiveWindBulletin: () => null,
  forecastWarningDetailsUrl: () => null,
}))

vi.mock('@/hooks/use-server-trails', () => ({
  useServerTrails: () => ({ getSelfTrail: vi.fn(), getAisTrails: vi.fn() }),
}))

vi.mock('@/hooks/use-dark-mode', () => ({
  useDarkMode: () => [false, vi.fn()],
}))

// [P1, impeccable critique 2026-09-12] a live alarm has to stay legible over
// the Mate sheet's own scrim - see the stacking test below. One live alarm
// is enough to drive the banner, which reads it through useAlarmListComparedOn; the
// mock lists every hook the shell and its surfaces import from this module.
const mockAlarm = {
  rule_id: 'helmcentral:test-alarm',
  label: 'Test alarm',
  path: 'notifications.test',
  phase: 'active' as const,
  state: 'alarm' as const,
  value: 1,
  message: 'Test alarm firing',
  silenced: false,
  can_silence: false,
  can_acknowledge: true,
}

vi.mock('@/hooks/use-alarms', () => ({
  // The shell holds the store; the surfaces read it through these (ADR 0215).
  useAlarmListComparedOn: () => [mockAlarm],
  sameShownAlarm: () => true,
  sameLampAlarm: () => true,
  samePillAlarm: () => true,
  useKnownAlarmList: () => [mockAlarm],
  useWorstAlarmState: () => 'alarm',
  useAisCollisionAlarms: () => new Map(),
  useAnchorDragAlarm: () => null,
  acknowledgeAlarm: vi.fn(),
  silenceAlarm: vi.fn(),
  collisionAlarmStatesByVessel: () => new Map(),
}))

// Every question App's Mate sheet sends, as the request body.
let sentQuestions: Array<Record<string, unknown>> = []

function stubFetch() {
  const conversations: Array<{ id: string; title: string; created_at: string; updated_at: string }> = []
  let counter = 0
  sentQuestions = []

  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    if (typeof url !== 'string') return { ok: false, json: async () => ({}) }
    const method = (init?.method ?? 'GET').toUpperCase()

    if (url.endsWith('/api/health')) {
      return { ok: true, json: async () => ({ status: 'ok', version: 'v0.20.0', revision: 'deadbeef' }) }
    }
    if (url.endsWith('/api/assistant/status')) {
      return { ok: true, json: async () => ({ enabled: false, configured: false, model: '' }) }
    }
    if (url.endsWith('/api/assistant/conversations') && method === 'GET') {
      return { ok: true, json: async () => ({ conversations }) }
    }
    if (url.endsWith('/api/assistant/conversations') && method === 'POST') {
      counter += 1
      const id = `new-${counter}`
      const now = new Date().toISOString()
      const conversation = { id, title: 'New conversation', created_at: now, updated_at: now }
      conversations.unshift(conversation)
      return { ok: true, status: 201, json: async () => conversation }
    }
    if (/\/api\/assistant\/conversations\/[^/]+\/messages$/.test(url) && method === 'POST') {
      sentQuestions.push(JSON.parse(init?.body as string) as Record<string, unknown>)
      return { ok: false, status: 503, json: async () => ({ error: 'Mate is not set up in this test.' }) }
    }
    const conversationMatch = url.match(/\/api\/assistant\/conversations\/([^/]+)$/)
    if (conversationMatch && method === 'GET') {
      const id = decodeURIComponent(conversationMatch[1])
      const conversation = conversations.find((c) => c.id === id) ?? { id, title: 'Untitled', created_at: '', updated_at: '' }
      return { ok: true, json: async () => ({ conversation, messages: [] }) }
    }
    // ADR 0105: AssistantThread's rejoin effect GETs .../run for whatever
    // conversation becomes active - 204 (no run in flight) is the ordinary
    // answer, matching the real backend, since nothing in this file's
    // voice-input/mic-button scenarios ever leaves a run actually going.
    if (/\/api\/assistant\/conversations\/[^/]+\/run$/.test(url) && method === 'GET') {
      return { ok: true, status: 204, json: async () => ({}) }
    }
    if (/\/api\/assistant\/conversations\/[^/]+\/run\/cancel$/.test(url) && method === 'POST') {
      return { ok: true, status: 204, json: async () => ({}) }
    }
    // Everything else (dashboard pages, routes, tanks, ...) reports "not
    // found" rather than being individually stubbed - the hooks behind
    // them tolerate that (App.smoke.test.tsx pins this), and nothing this
    // file asserts on depends on their data.
    return { ok: false, json: async () => ({}) }
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

// Captures the latest options App hands the update-reload hook, so a test can
// ask the same hasUnsavedWork() the real hook would consult before reloading.
const versionReload = vi.hoisted(() => ({ latest: null as null | { hasUnsavedWork?: () => boolean } }))
vi.mock('@/hooks/use-version-reload', () => ({
  useVersionReload: (options: { hasUnsavedWork?: () => boolean }) => {
    versionReload.latest = options
    return { current: null, available: null, chunkFailure: null, readError: null, reportChunkFailure: () => {}, clearChunkFailure: () => {}, retryHeldReload: () => {} }
  },
}))

// The stream is up and has said nothing yet: every reading is unknown.
installFakeTelemetryStream({ seeds: { 'vessel-state': {} } })
// The first GET /api/anchor-watch has answered: no watch is set.
beforeEach(() => { landAnchorWatch() })

describe('App-wide voice (ADR 0093)', () => {
  beforeEach(() => {
    instances = []
    mockAssistantVoice.voiceInput = true
    mockAssistantVoice.readAloud = false
    mockAssistantVoice.wakeWord = false
    mockAuth.canWrite = true
    vi.spyOn(console, 'error').mockImplementation(() => {})
    stubFetch()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows the mic button when voice input is on and the session can write', () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    render(<App />)

    expect(screen.getByRole('button', { name: 'Talk to Mate' })).toBeInTheDocument()
  })

  it('hides the mic button when voice input is off', () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    mockAssistantVoice.voiceInput = false
    render(<App />)

    expect(screen.queryByRole('button', { name: 'Talk to Mate' })).not.toBeInTheDocument()
  })

  it('hides the mic button for a read-only session', () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    mockAuth.canWrite = false
    render(<App />)

    expect(screen.queryByRole('button', { name: 'Talk to Mate' })).not.toBeInTheDocument()
  })

  // ADR 0122: no speech API at all hides the button entirely, matching the
  // rule components/dictation.tsx's in-field mic already follows, rather
  // than showing a disabled MicOff that names a cause nobody on a
  // touchscreen would see anyway (a `title` tooltip needs a hover).
  it('hides the mic button entirely when there is no recognition API', () => {
    render(<App />)

    expect(screen.queryByRole('button', { name: 'Talk to Mate' })).not.toBeInTheDocument()
  })

  // An insecure origin is different from no API at all: the button stays
  // visible, disabled, with MicOff and a title naming why - the operator
  // can fix this one (open the https address) where "no recognition API"
  // has no fix to offer.
  it('shows the mic button disabled, with MicOff and a title, on an insecure origin', () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true })

    render(<App />)

    const button = screen.getByRole('button', { name: 'Talk to Mate' })
    expect(button).toBeDisabled()
    expect(button).toHaveAttribute('title', 'Voice input needs the app opened over https')

    Object.defineProperty(window, 'isSecureContext', { value: undefined, configurable: true })
  })

  it('opens the Mate sheet with the transcript once the mic delivers a final result', async () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Talk to Mate' }))
    expect(instances).toHaveLength(1)

    currentRecognition().emitResult('how does the passage look', true)

    expect(await screen.findByRole('heading', { name: 'Mate' })).toBeInTheDocument()
    expect(await screen.findByText('how does the passage look')).toBeInTheDocument()
  })

  // Only a question that came in by voice asks Mate for an answer to be
  // spoken; one typed into the sheet does not. Both say which screen the
  // operator is on.
  it('asks for a spoken answer to a voice question, and not to a typed follow-up', async () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Talk to Mate' }))
    currentRecognition().emitResult('how does the passage look', true)
    await waitFor(() => expect(sentQuestions).toHaveLength(1))
    expect(sentQuestions[0]).toEqual(expect.objectContaining({ content: 'how does the passage look', spoken: true, screen: expect.any(Object) }))

    const composer = await screen.findByPlaceholderText('Ask Mate')
    await waitFor(() => expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled())
    fireEvent.change(composer, { target: { value: 'and tomorrow?' } })
    fireEvent.keyDown(composer, { key: 'Enter' })
    await waitFor(() => expect(sentQuestions).toHaveLength(2))
    expect(sentQuestions[1]).not.toHaveProperty('spoken')
    expect(sentQuestions[1]).toEqual(expect.objectContaining({ content: 'and tomorrow?', screen: expect.any(Object) }))
  })

  it('an unsent message in the Mate sheet holds an update reload until it is cleared', async () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    render(<App />)
    expect(versionReload.latest?.hasUnsavedWork?.()).toBe(false)

    fireEvent.keyDown(window, { altKey: true, code: 'KeyM' })
    await waitFor(() => expect(instances).toHaveLength(1))
    currentRecognition().emitResult('how does the passage look', true)
    await screen.findByText('how does the passage look')

    const composer = await screen.findByPlaceholderText('Ask Mate')
    fireEvent.change(composer, { target: { value: 'half a thought' } })
    await waitFor(() => expect(versionReload.latest?.hasUnsavedWork?.()).toBe(true))

    fireEvent.change(composer, { target: { value: '' } })
    await waitFor(() => expect(versionReload.latest?.hasUnsavedWork?.()).toBe(false))
  })

  it('Alt+M triggers push-to-talk from anywhere in the shell', async () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    render(<App />)

    fireEvent.keyDown(window, { altKey: true, code: 'KeyM' })
    await waitFor(() => expect(instances).toHaveLength(1))

    currentRecognition().emitResult('what about tomorrow', true)

    expect(await screen.findByRole('heading', { name: 'Mate' })).toBeInTheDocument()
    expect(await screen.findByText('what about tomorrow')).toBeInTheDocument()
  })

  // ADR 0122: the same primary-filled treatment as the in-field
  // DictateButton, so "this is recording" reads at a glance rather than
  // depending on the ghost icon's low-contrast text-primary tint.
  it('fills the mic button solid (primary) while listening, not just a tinted icon', async () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    render(<App />)

    const button = screen.getByRole('button', { name: 'Talk to Mate' })
    expect(button.className).not.toMatch(/(?:^|\s)bg-primary(?:\s|$)/)

    fireEvent.click(button)
    await waitFor(() => expect(button).toHaveAttribute('aria-pressed', 'true'))

    expect(button.className).toMatch(/(?:^|\s)bg-primary(?:\s|$)/)
  })

  it('Escape cancels push-to-talk while listening', async () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Talk to Mate' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Talk to Mate' })).toHaveAttribute('aria-pressed', 'true'))

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(currentRecognition().aborted).toBe(true)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Talk to Mate' })).toHaveAttribute('aria-pressed', 'false'))
  })

  // ADR 0094: "Open the Mate page" (renamed from "Open in Mate" - impeccable
  // critique 2026-09-12 P2) hands the sheet's active conversation to the
  // full panel and navigates there, closing the sheet - without it, the
  // sheet was the only way to see a thread at all. Mate UI cycle ("Mate
  // opens on an empty chat"): "New conversation" no longer creates anything
  // by itself (conversations.startNew's own doc comment) - a real
  // conversation only exists once the operator sends a first message, so
  // that's what this test does to get one, the same way the "what about
  // tomorrow" voice test above it does.
  it('Open the Mate page from the sheet lands on the Mate panel with that conversation requested', async () => {
    const fetchMock = stubFetch()
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: 'Ask Mate' }))
    const textarea = await screen.findByPlaceholderText('Ask Mate')
    fireEvent.change(textarea, { target: { value: 'What about tomorrow?' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/assistant/conversations', expect.objectContaining({ method: 'POST' })))

    fireEvent.click(screen.getByRole('button', { name: 'Open the Mate page' }))

    await waitFor(() => expect(document.title).toBe('Mate · Helmcentral'))
    expect(screen.queryByRole('heading', { name: 'Mate' })).not.toBeInTheDocument()
    // The panel's own hook instance opened the same conversation the sheet
    // had active, not whatever it would otherwise have picked as newest.
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/assistant/conversations/new-1', expect.anything()))
  })

  // ADR 0105: the Mate page's thread scrolls inside MessageScroller's own
  // viewport, which needs a bounded height to scroll at all. The page shell
  // only sets min-h-svh, so on the Mate panel the inset is capped at the
  // viewport; otherwise a long conversation grows the page past the screen
  // and the viewport's overscroll-contain stops the page scrolling too.
  // jsdom can't measure layout, so this checks the class that bounds it.
  it('caps the page at the viewport height on the Mate panel only', async () => {
    stubFetch()
    render(<App />)

    const main = document.querySelector('main')
    expect(main).not.toBeNull()
    expect(main!.className).not.toMatch(/(?:^|\s)h-svh(?:\s|$)/)

    fireEvent.click(screen.getByRole('button', { name: 'Ask Mate' }))
    await screen.findByRole('heading', { name: 'Mate' })
    fireEvent.click(screen.getByRole('button', { name: 'Open the Mate page' }))
    await waitFor(() => expect(document.title).toBe('Mate · Helmcentral'))

    expect(document.querySelector('main')!.className).toMatch(/(?:^|\s)h-svh(?:\s|$)/)
  })

  // [P1, impeccable critique 2026-09-12] the sheet's overlay used to be a
  // flat 80% black scrim that dimmed a live, unacknowledged alarm on the
  // page behind it. Lowering the overlay's own opacity (mate-sheet.test.tsx
  // covers that) only helps so much - the banner also needs to sit above
  // the sheet's stacking context outright so it reads at full strength
  // rather than through a haze. jsdom can't measure actual paint order, so
  // this checks the one thing that determines it: the banner's own z-index
  // class has to be numerically higher than the overlay's (z-50).
  it('keeps the alarm banner above the Mate sheet overlay but below the header controls', async () => {
    vi.stubGlobal('SpeechRecognition', FakeSpeechRecognition)
    render(<App />)

    const bannerStack = await screen.findByTestId('alarm-banner-stack')
    const header = document.querySelector('header')
    expect(screen.getByText(/Test alarm firing/)).toBeInTheDocument()
    expect(header).not.toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Ask Mate' }))
    await screen.findByRole('heading', { name: 'Mate' })

    const overlay = document.querySelector('[role="presentation"][data-open]')
    expect(overlay).not.toBeNull()

    const zIndexOf = (className: string): number => {
      const match = className.match(/z-\[(\d+)\]|(?:^|\s)z-(\d+)(?:\s|$)/)
      return match ? Number(match[1] ?? match[2]) : 0
    }

    expect(zIndexOf(bannerStack.className)).toBeGreaterThan(zIndexOf(overlay!.className))
    expect(zIndexOf(header!.className)).toBeGreaterThan(zIndexOf(bannerStack.className))
  })
})

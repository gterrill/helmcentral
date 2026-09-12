import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

import type { ManualTarget } from '@/lib/manual-links'

// ADR 0095: same fetch-router pattern as mate-sheet.test.tsx - a fake
// `GET /api/manual/<id>` keyed by a small set of fixture ids, driving the
// sheet's own useManualPage hook exactly the way the real backend endpoint
// would (200 page / 404 unknown id / 503 manual not staged, per the
// contract backend/manual_handlers.go implements).
//
// use-manual.ts keeps a module-level page cache (deliberately - see its own
// comment: the manual can't change without a new binary, and the cache is
// what makes the sheet's Back button instant). That means every test needs
// its own fresh copy of the module graph, not just a fresh fetch mock, or an
// earlier test's cached page would silently answer a later test that expects
// different content for the same id. `vi.resetModules()` plus a dynamic
// import gets a clean cache per test, the same pattern
// use-radar-capabilities.test.ts and web-push-section.test.tsx already use
// for the same reason.

interface ManualPageFixture {
  id: string
  title: string
  body: string
}

type ManualFixtureEntry = ManualPageFixture | { status: number; error: string }

function buildFetch(pages: Record<string, ManualFixtureEntry>) {
  return vi.fn(async (url: string) => {
    const match = /\/api\/manual\/(.+)$/.exec(url)
    if (!match) throw new Error(`Unhandled fetch in test: ${url}`)
    const id = decodeURIComponent(match[1])
    const entry = pages[id]
    if (!entry) throw new Error(`no fixture registered for manual page "${id}"`)
    if ('status' in entry) {
      return { ok: false, status: entry.status, json: async () => ({ error: entry.error }) }
    }
    return { ok: true, status: 200, json: async () => entry }
  })
}

interface RenderManualSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  target: ManualTarget | null
  onAskMate: (question: string) => void
}

async function renderManualSheet(props: RenderManualSheetProps) {
  vi.resetModules()
  const { ManualSheet } = await import('@/components/manual-sheet')
  return render(<ManualSheet {...props} />)
}

describe('ManualSheet', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.resetModules()
  })

  it('does not render while closed, and fetches nothing', async () => {
    const fetchMock = buildFetch({})
    vi.stubGlobal('fetch', fetchMock)

    await renderManualSheet({ open: false, onOpenChange: vi.fn(), target: null, onAskMate: vi.fn() })

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('opens on the given target, fetching and showing that page', async () => {
    vi.stubGlobal(
      'fetch',
      buildFetch({
        'features/forecast': { id: 'features/forecast', title: 'Forecast', body: '# Forecast\n\nBody text.' },
      }),
    )

    await renderManualSheet({
      open: true,
      onOpenChange: vi.fn(),
      target: { page: 'features/forecast' },
      onAskMate: vi.fn(),
    })

    expect(await screen.findByRole('heading', { name: 'Forecast' })).toBeInTheDocument()
    expect(screen.getByText('Body text.')).toBeInTheDocument()
    // The group line under the title names which part of the manual this is.
    expect(screen.getByText('Feature')).toBeInTheDocument()
  })

  it('shows three loading skeletons before the page arrives', async () => {
    let resolveFetch: (value: unknown) => void = () => {}
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise((resolve) => { resolveFetch = resolve })),
    )

    await renderManualSheet({
      open: true,
      onOpenChange: vi.fn(),
      target: { page: 'features/forecast' },
      onAskMate: vi.fn(),
    })

    expect(await screen.findByRole('heading', { name: 'Manual' })).toBeInTheDocument()
    expect(screen.getByText('--')).toBeInTheDocument()
    expect(screen.getByTestId('manual-sheet-skeleton').children).toHaveLength(3)

    resolveFetch({
      ok: true,
      status: 200,
      json: async () => ({ id: 'features/forecast', title: 'Forecast', body: '# Forecast\n\nBody text.' }),
    })

    expect(await screen.findByRole('heading', { name: 'Forecast' })).toBeInTheDocument()
  })

  it('scrolls the target heading into view once the page loads', async () => {
    vi.stubGlobal(
      'fetch',
      buildFetch({
        'features/dashboard': {
          id: 'features/dashboard',
          title: 'The dashboard',
          body: '# The dashboard\n\n### Route planning\n\nPlanning detail.',
        },
      }),
    )
    const scrollIntoViewSpy = vi.spyOn(Element.prototype, 'scrollIntoView')

    await renderManualSheet({
      open: true,
      onOpenChange: vi.fn(),
      target: { page: 'features/dashboard', heading: 'Route planning' },
      onAskMate: vi.fn(),
    })

    await screen.findByText('Planning detail.')
    const heading = screen.getByText('Route planning')
    expect(heading).toHaveAttribute('id', 'route-planning')
    await waitFor(() => expect(scrollIntoViewSpy).toHaveBeenCalled())
  })

  it('shows a one-line notice, and never a blank page, when the target heading is not on the page', async () => {
    vi.stubGlobal(
      'fetch',
      buildFetch({
        'features/dashboard': {
          id: 'features/dashboard',
          title: 'The dashboard',
          body: '# The dashboard\n\nIntro text.',
        },
      }),
    )

    await renderManualSheet({
      open: true,
      onOpenChange: vi.fn(),
      target: { page: 'features/dashboard', heading: 'Nonexistent section' },
      onAskMate: vi.fn(),
    })

    expect(await screen.findByText('Section "Nonexistent section" is not on this page')).toBeInTheDocument()
    expect(screen.getByText('Intro text.')).toBeInTheDocument()
  })

  it('a page link pushes history, revealing Back, and Back returns to the previous page', async () => {
    vi.stubGlobal(
      'fetch',
      buildFetch({
        'features/dashboard': {
          id: 'features/dashboard',
          title: 'The dashboard',
          body: '# The dashboard\n\nSee [Alarms](alarms.md) for the rules list.',
        },
        'features/alarms': {
          id: 'features/alarms',
          title: 'Alarms',
          body: '# Alarms\n\nAlarm detail.',
        },
      }),
    )

    await renderManualSheet({
      open: true,
      onOpenChange: vi.fn(),
      target: { page: 'features/dashboard' },
      onAskMate: vi.fn(),
    })

    await screen.findByText(/rules list/)
    expect(screen.queryByRole('button', { name: 'Back' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('link', { name: 'Alarms' }))

    expect(await screen.findByRole('heading', { name: 'Alarms' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Back' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Back' }))

    expect(await screen.findByRole('heading', { name: 'The dashboard' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Back' })).not.toBeInTheDocument()
  })

  it('the contents button loads the index page, and hides itself once there', async () => {
    vi.stubGlobal(
      'fetch',
      buildFetch({
        'features/dashboard': { id: 'features/dashboard', title: 'The dashboard', body: '# The dashboard\n\nIntro.' },
        index: { id: 'index', title: 'Helmcentral documentation', body: '# Helmcentral documentation\n\nContents body.' },
      }),
    )

    await renderManualSheet({
      open: true,
      onOpenChange: vi.fn(),
      target: { page: 'features/dashboard' },
      onAskMate: vi.fn(),
    })

    await screen.findByText('Intro.')
    expect(screen.getByRole('button', { name: 'Manual contents' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Manual contents' }))

    expect(await screen.findByText('Contents body.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Manual contents' })).not.toBeInTheDocument()
  })

  it('shows the backend sentence verbatim when the manual is not staged (503)', async () => {
    vi.stubGlobal(
      'fetch',
      buildFetch({
        'features/dashboard': {
          status: 503,
          error: 'the manual is not embedded in this build (run make manual-stage)',
        },
      }),
    )

    await renderManualSheet({
      open: true,
      onOpenChange: vi.fn(),
      target: { page: 'features/dashboard' },
      onAskMate: vi.fn(),
    })

    expect(await screen.findByText('The manual is not in this build.')).toBeInTheDocument()
    expect(
      screen.getByText('the manual is not embedded in this build (run make manual-stage)'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })

  it('quotes the missing id and offers the contents button on a 404', async () => {
    vi.stubGlobal(
      'fetch',
      buildFetch({
        'features/nope': { status: 404, error: 'unknown manual page "features/nope"' },
      }),
    )

    await renderManualSheet({
      open: true,
      onOpenChange: vi.fn(),
      target: { page: 'features/nope' },
      onAskMate: vi.fn(),
    })

    expect(await screen.findByText('This build\'s manual has no page "features/nope".')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open the contents' })).toBeInTheDocument()
  })

  it('omits the contents button when the index page itself 404s', async () => {
    vi.stubGlobal('fetch', buildFetch({ index: { status: 404, error: 'unknown manual page "index"' } }))

    await renderManualSheet({ open: true, onOpenChange: vi.fn(), target: null, onAskMate: vi.fn() })

    expect(await screen.findByText('This build\'s manual has no page "index".')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Open the contents' })).not.toBeInTheDocument()
  })

  it('falls back to a plain HTTP status for anything other than 404/503', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({
        ok: false,
        status: 502,
        json: async () => {
          throw new Error('not json')
        },
      })),
    )

    await renderManualSheet({
      open: true,
      onOpenChange: vi.fn(),
      target: { page: 'features/dashboard' },
      onAskMate: vi.fn(),
    })

    expect(await screen.findByText('Could not load the manual.')).toBeInTheDocument()
    expect(screen.getByText('HTTP 502')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })

  it('Ask Mate seeds a question naming the loaded page title', async () => {
    const onAskMate = vi.fn()
    vi.stubGlobal(
      'fetch',
      buildFetch({
        'features/forecast': { id: 'features/forecast', title: 'Forecast', body: '# Forecast\n\nBody.' },
      }),
    )

    await renderManualSheet({
      open: true,
      onOpenChange: vi.fn(),
      target: { page: 'features/forecast' },
      onAskMate,
    })

    await screen.findByText('Body.')
    fireEvent.click(screen.getByRole('button', { name: 'Ask Mate about this page' }))

    expect(onAskMate).toHaveBeenCalledWith('How does the Forecast page work?')
  })

  it('Ask Mate seeds the contents question on the index page', async () => {
    const onAskMate = vi.fn()
    vi.stubGlobal(
      'fetch',
      buildFetch({
        index: { id: 'index', title: 'Helmcentral documentation', body: '# Helmcentral documentation\n\nBody.' },
      }),
    )

    await renderManualSheet({ open: true, onOpenChange: vi.fn(), target: null, onAskMate })

    await screen.findByText('Body.')
    fireEvent.click(screen.getByRole('button', { name: 'Ask Mate about this page' }))

    expect(onAskMate).toHaveBeenCalledWith('What can Helmcentral do?')
  })

  it('matches the Mate sheet width', async () => {
    vi.stubGlobal(
      'fetch',
      buildFetch({
        'features/forecast': { id: 'features/forecast', title: 'Forecast', body: '# Forecast\n\nBody.' },
      }),
    )

    await renderManualSheet({
      open: true,
      onOpenChange: vi.fn(),
      target: { page: 'features/forecast' },
      onAskMate: vi.fn(),
    })

    const dialog = await screen.findByRole('dialog')
    expect(dialog.className).toEqual(expect.stringContaining('sm:max-w-xl'))
  })
})

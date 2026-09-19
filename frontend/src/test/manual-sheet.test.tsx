import { useEffect, useState } from 'react'
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
    // A no-op unless the deep-linked-heading test below registered it -
    // clears that per-test mock so it can never leak into a later test that
    // needs the real ManualMarkdown.
    vi.doUnmock('@/components/manual-markdown')
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
    // Body content renders through the now-lazy ManualMarkdown, so this is
    // the assertion that has to await - the sheet's own title (above) loads
    // independently of that chunk.
    expect(await screen.findByText('Body text.')).toBeInTheDocument()
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

    expect(await screen.findByRole('heading', { name: 'Help' })).toBeInTheDocument()
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

  // ManualMarkdown renders behind React.lazy now (kiosk bundle-split), so
  // the scroll-to-heading effect above can no longer assume the heading
  // element exists the moment manual.page/current.heading settle - the impl
  // chunk might still be loading. manual-sheet.tsx's fix is a second trigger
  // (onRendered -> renderTick) tied to the renderer actually mounting. A
  // real React.lazy() resolves in a single microtask, too fast to expose
  // the race, so this mocks ManualMarkdown itself with a fake that holds
  // back both the heading markup and the onRendered call until a
  // test-controlled promise resolves - same "capture a resolver, control
  // the timing by hand" idiom as the "shows three loading skeletons before
  // the page arrives" test above, applied to the renderer instead of fetch.
  it('scrolls to a deep-linked heading only once the lazy renderer has mounted it', async () => {
    let resolveRendered: () => void = () => {}
    const rendered = new Promise<void>((resolve) => {
      resolveRendered = resolve
    })

    // Inlines renderManualSheet's own reset+import steps instead of calling
    // it, because vi.doMock has to be registered *after* vi.resetModules()
    // - the same order buildFetch's sibling suite (web-push-section.test.tsx)
    // uses - or the reset wipes out the registration before manual-sheet.tsx
    // ever gets a chance to import the mocked module.
    vi.resetModules()
    vi.doMock('@/components/manual-markdown', () => ({
      ManualMarkdown: ({ onRendered }: { onRendered?: () => void }) => {
        const [ready, setReady] = useState(false)
        useEffect(() => {
          let cancelled = false
          void rendered.then(() => {
            if (!cancelled) setReady(true)
          })
          return () => {
            cancelled = true
          }
        }, [])
        useEffect(() => {
          if (ready) onRendered?.()
        }, [ready, onRendered])
        return ready ? <h3 id="route-planning">Route planning</h3> : null
      },
    }))
    const { ManualSheet } = await import('@/components/manual-sheet')

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
    // vi.spyOn on an already-spied prototype method (the preceding test
    // spies the same Element.prototype.scrollIntoView) returns the existing
    // spy rather than a fresh one, and this suite never restores mocks
    // between tests - so its call count has to be cleared explicitly, or a
    // call from an earlier test would already satisfy "not called" below.
    const scrollIntoViewSpy = vi.spyOn(Element.prototype, 'scrollIntoView')
    scrollIntoViewSpy.mockClear()

    render(
      <ManualSheet
        open
        onOpenChange={vi.fn()}
        target={{ page: 'features/dashboard', heading: 'Route planning' }}
        onAskMate={vi.fn()}
      />,
    )

    // The page itself has loaded (the sheet's own title renders it), but the
    // fake renderer is still holding back its heading markup - the bug this
    // guards against would have the scroll effect give up right here.
    await screen.findByRole('heading', { name: 'The dashboard' })
    expect(scrollIntoViewSpy).not.toHaveBeenCalled()

    resolveRendered()

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
    // Body content renders through the now-lazy ManualMarkdown independently
    // of the notice above, so this still needs its own await.
    expect(await screen.findByText('Intro text.')).toBeInTheDocument()
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
    expect(screen.getByRole('button', { name: 'Help contents' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Help contents' }))

    expect(await screen.findByText('Contents body.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Help contents' })).not.toBeInTheDocument()
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

    expect(onAskMate).toHaveBeenCalledWith('What does the manual say about "Forecast"?')
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

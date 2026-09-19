import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'

// Kiosk bundle-split: assistant-markdown.tsx and help-markdown.tsx now
// load react-markdown's whole module graph - and, through it,
// mdast-util-gfm-autolink-literal's regex lookbehind literal, which a
// pre-16.4 Safari (the wall-display kiosk's WPE WebKit 2.38.5) can't even
// parse - behind React.lazy rather than as a static import. These tests
// prove both halves of that contract: the real markdown output still shows
// up once the lazy chunk resolves, and react-markdown itself is never
// invoked as part of the wrapper's own synchronous render.
//
// React.lazy() memoises its resolved promise on the lazy()-wrapped object
// itself, which lives on the module instance created the first time
// assistant-markdown.tsx/help-markdown.tsx is imported - once any test
// resolves it, every later render of that same component within the same
// module graph mounts synchronously, never suspending again. Each test below
// therefore resets the module registry and re-imports the wrapper fresh, the
// same pattern help-sheet.test.tsx already uses for its own module-level
// cache (use-help.ts's page cache) - otherwise only the first test to
// touch each wrapper would ever see the pre-resolution state.

const { markdownSpy } = vi.hoisted(() => ({ markdownSpy: vi.fn() }))

// Wraps the *real* react-markdown default export in a spy, rather than
// replacing it with a fake - the assertions below need genuine rendering
// behaviour (findByText has to see real output), just observed. This is
// option (i) from the brief: a spy on react-markdown itself is a more
// direct proof that its module graph hasn't run yet than inferring it from
// the Suspense fallback's own markup (option ii) would be.
vi.mock('react-markdown', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-markdown')>()
  markdownSpy.mockImplementation(actual.default)
  return { ...actual, default: markdownSpy }
})

async function importFreshAssistantMarkdown() {
  vi.resetModules()
  const { AssistantMarkdown } = await import('@/components/assistant-markdown')
  return AssistantMarkdown
}

async function importFreshHelpMarkdown() {
  vi.resetModules()
  const { HelpMarkdown } = await import('@/components/help-markdown')
  return HelpMarkdown
}

describe('markdown renderers are lazy (kiosk bundle-split)', () => {
  beforeEach(() => {
    markdownSpy.mockClear()
  })

  it('AssistantMarkdown renders the real markdown output once the lazy chunk resolves', async () => {
    const AssistantMarkdown = await importFreshAssistantMarkdown()
    render(<AssistantMarkdown content="Hook Reef anchorages" />)

    expect(await screen.findByText('Hook Reef anchorages')).toBeInTheDocument()
  })

  it('HelpMarkdown renders the real markdown output once the lazy chunk resolves', async () => {
    const HelpMarkdown = await importFreshHelpMarkdown()
    render(<HelpMarkdown content="## The kiosk feed" pageId="features/dashboard" onNavigate={vi.fn()} />)

    expect(await screen.findByText('The kiosk feed')).toBeInTheDocument()
  })

  it('never calls react-markdown synchronously when AssistantMarkdown first mounts', async () => {
    const AssistantMarkdown = await importFreshAssistantMarkdown()
    render(<AssistantMarkdown content="Safe text" />)

    // Nothing has awaited yet since render() - if react-markdown were part
    // of the wrapper's own synchronous module graph (rather than behind
    // React.lazy), it would already have been invoked here.
    expect(markdownSpy).not.toHaveBeenCalled()

    expect(await screen.findByText('Safe text')).toBeInTheDocument()
    expect(markdownSpy).toHaveBeenCalled()
  })

  it('never calls react-markdown synchronously when HelpMarkdown first mounts', async () => {
    const HelpMarkdown = await importFreshHelpMarkdown()
    render(<HelpMarkdown content="Safe text" pageId="features/dashboard" onNavigate={vi.fn()} />)

    expect(markdownSpy).not.toHaveBeenCalled()

    expect(await screen.findByText('Safe text')).toBeInTheDocument()
    expect(markdownSpy).toHaveBeenCalled()
  })
})

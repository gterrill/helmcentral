import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'

import { AssistantMarkdown } from '@/components/assistant-markdown'

describe('AssistantMarkdown', () => {
  // AssistantMarkdown now renders its content through a React.lazy-loaded
  // impl chunk (kiosk bundle-split), so the markdown output is not there on
  // the first synchronous render - only the Suspense fallback is. The first
  // content-bearing assertion in each test below awaits it with findBy*;
  // whatever follows on the same rendered tree can stay a plain getBy* once
  // that first await has resolved.
  it('renders a GFM table', async () => {
    const content = [
      '| Anchorage | Wind |',
      '| --- | --- |',
      '| Tongue Bay | 12kt SE |',
      '| Blue Pearl Bay | 15kt SE |',
    ].join('\n')

    render(<AssistantMarkdown content={content} />)

    expect(await screen.findByRole('table')).toBeInTheDocument()
    expect(screen.getByText('Tongue Bay')).toBeInTheDocument()
  })

  it('does not render a raw <script> or <img> tag from the input', async () => {
    const content = '<script>window.__pwned = true</script>\n\n![alt](https://example.com/x.png)\n\nSafe text'

    const { container } = render(<AssistantMarkdown content={content} />)

    expect(await screen.findByText('Safe text')).toBeInTheDocument()
    expect(container.querySelector('script')).toBeNull()
    expect(container.querySelector('img')).toBeNull()
  })

  // [P3, impeccable critique 2026-09-12] h1 and h2 used to render identically
  // (both `text-base`) - a reply with both levels had no visible hierarchy
  // between them.
  it('gives h1 a visible step above h2', async () => {
    const content = '# Top level\n\n## Second level\n\nBody text.'

    render(<AssistantMarkdown content={content} />)

    const h1 = await screen.findByText('Top level')
    const h2 = screen.getByText('Second level')

    expect(h1.className).toEqual(expect.stringContaining('text-lg'))
    expect(h1.className).not.toEqual(expect.stringContaining('text-base'))
    expect(h2.className).toEqual(expect.stringContaining('text-base'))
  })

  it('gives a link target=_blank and rel=noreferrer', async () => {
    render(<AssistantMarkdown content="[OpenRouter](https://openrouter.ai)" />)

    const link = await screen.findByRole('link', { name: 'OpenRouter' })
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noreferrer')
    expect(link).toHaveAttribute('href', 'https://openrouter.ai')
  })
})

// Same idiom as the design-token guard in forecast-drawer.test.tsx (read
// there first): reads the component source directly off disk so a raw
// colour literal or an off-scale micro type size can't creep back in
// unnoticed. assistant-drawer.tsx ships in the same feature (ADR 0093) and
// is held to the same bar here rather than duplicating this guard in its
// own test file.
const GUARD_RELATIVE_PATHS = [
  '../components/assistant-markdown-impl.tsx',
  '../components/assistant-drawer.tsx',
]

function readGuardSources() {
  const testDir = dirname(fileURLToPath(import.meta.url))
  return GUARD_RELATIVE_PATHS.map((relativePath) => ({
    label: relativePath.replace(/^\.\.\//, ''),
    lines: readFileSync(resolve(testDir, relativePath), 'utf8').split('\n'),
  }))
}

describe('assistant components design tokens', () => {
  it('contains no raw colour literals - only semantic tokens', () => {
    // hsl(var(--...)) and bare Tailwind utility classes (bg-muted, text-foreground,
    // etc.) are untouched by this pattern; only a hand-typed rgb()/rgba()/hex
    // literal or an arbitrary bg-[#...] / text-[#...] utility matches.
    const colorLiteralPattern = /rgb\(|rgba\(|#[0-9a-fA-F]{3,8}\b/g

    const offenders: string[] = []
    for (const { label, lines } of readGuardSources()) {
      lines.forEach((line, idx) => {
        const matches = line.match(colorLiteralPattern)
        if (matches) offenders.push(`  ${label}:${idx + 1} (${matches.length}x): ${line.trim()}`)
      })
    }

    expect(
      offenders,
      `Found raw colour literal(s) - replace with a semantic token:\n${offenders.join('\n')}`,
    ).toEqual([])
  })

  it('uses only text-[11px]/[10px]/[9px] for arbitrary micro type - AGENTS.md scale', () => {
    const arbitraryTextSizePattern = /text-\[[^\]]*\]/g
    const allowed = new Set(['text-[11px]', 'text-[10px]', 'text-[9px]'])

    const offenders: string[] = []
    for (const { label, lines } of readGuardSources()) {
      lines.forEach((line, idx) => {
        const matches = line.match(arbitraryTextSizePattern) ?? []
        for (const match of matches) {
          if (!allowed.has(match)) offenders.push(`  ${label}:${idx + 1}: ${match} in: ${line.trim()}`)
        }
      })
    }

    expect(
      offenders,
      `Found an off-scale arbitrary text size - only text-[11px]/[10px]/[9px] are allowed:\n${offenders.join('\n')}`,
    ).toEqual([])
  })
})

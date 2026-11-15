import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join, relative, resolve } from 'node:path'

import { describe, it, expect } from 'vitest'

// ADR 0109: the operator has one word for a component on a dashboard page,
// and it is "tile". This test is the thing that keeps that true. Without it
// the rename is a one-time cleanup that drifts back the first time someone
// writes a page from memory, which is exactly how docs/features/dashboard.md
// ended up carrying "## Widgets" and "## Tile state" as peer headings for the
// same object, and how the #widgets anchor in forecast.md outlived the
// heading it pointed at.
//
// Scope is the user-facing Diataxis tree only. docs/adr/ is deliberately
// excluded: thirty-four ADRs say "widget" because that was the word when they
// were written, and they are the historical record, not documentation.
// docs/examples/ is third-party plugin source, not our prose.
//
// Resolved the same way help-links.test.ts resolves the docs root: relative
// to the frontend package dir, one level up to the repo root, then into docs/.
const USER_FACING_DIRS = ['features', 'how-to', 'reference', 'tutorials']

function repoRoot(): string {
  const testDir = dirname(fileURLToPath(import.meta.url))
  const frontendDir = resolve(testDir, '..', '..')
  return resolve(frontendDir, '..')
}

function docsRoot(): string {
  return join(repoRoot(), 'docs')
}

function markdownFilesUnder(dir: string): string[] {
  let entries
  try {
    entries = readdirSync(dir, { withFileTypes: true })
  } catch {
    // A tree that does not exist yet (docs/tutorials, per the help-pages
    // skill's table, is created only when something needs it) is not a
    // failure.
    return []
  }

  const files: string[] = []
  for (const entry of entries) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) files.push(...markdownFilesUnder(full))
    else if (entry.name.endsWith('.md')) files.push(full)
  }
  return files
}

function offendingLines(pattern: RegExp = /widget/i): string[] {
  const root = docsRoot()
  const pages = [
    ...USER_FACING_DIRS.flatMap((d) => markdownFilesUnder(join(root, d))),
    join(root, 'index.md'),
    // The README is the project's front page and describes the product to an
    // operator, so it is bound by the same rule as the help pages. It sat
    // outside this check on the first pass and kept saying "widgets" while
    // every page it links to had stopped.
    join(repoRoot(), 'README.md'),
  ]

  const offences: string[] = []
  for (const page of pages) {
    let body
    try {
      body = readFileSync(page, 'utf8')
    } catch {
      continue
    }
    body.split('\n').forEach((line, i) => {
      if (pattern.test(line)) {
        offences.push(`${relative(repoRoot(), page)}:${i + 1}: ${line.trim()}`)
      }
    })
  }
  return offences
}

// ADR 0285: passage Plans replaced the planned stay, and "Plan a stay" went
// with it. The old words in a help page or a string the operator reads say the
// product still has a feature it no longer has.
const RETIRED_PLAN_WORDS = /plan a stay|planned stay/i

function sourceFilesUnder(dir: string): string[] {
  const files: string[] = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) {
      if (entry.name !== 'test') files.push(...sourceFilesUnder(full))
    } else if (/\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name)) {
      files.push(full)
    }
  }
  return files
}

// A comment explaining the history is not a string the operator reads.
function isCommentLine(line: string): boolean {
  return /^\s*(\/\/|\/\*|\*)/.test(line)
}

describe('retired planned-stay wording (ADR 0285)', () => {
  it('is gone from the help pages and the README', () => {
    const offences = offendingLines(RETIRED_PLAN_WORDS)
    expect(offences, `Say "plan" or "passage plan", not "Plan a stay" or "planned stay".\n\n${offences.join('\n')}\n`).toEqual([])
  })

  it('is gone from the strings the frontend shows', () => {
    const srcDir = join(resolve(repoRoot(), 'frontend'), 'src')
    const offences: string[] = []
    for (const file of sourceFilesUnder(srcDir)) {
      const rel = relative(join(repoRoot(), 'frontend'), file)
      readFileSync(file, 'utf8')
        .split('\n')
        .forEach((line, i) => {
          if (!isCommentLine(line) && RETIRED_PLAN_WORDS.test(line)) offences.push(`${rel}:${i + 1}: ${line.trim()}`)
        })
    }
    expect(offences, `Say "plan" or "passage plan", not "Plan a stay" or "planned stay".\n\n${offences.join('\n')}\n`).toEqual([])
  })
})

describe('operator-facing docs vocabulary', () => {
  it('never says "widget" - a component on a dashboard page is a tile (ADR 0109)', () => {
    const offences = offendingLines()
    expect(
      offences,
      `Operator-facing documentation must call a dashboard component a "tile", not a "widget".\n` +
        `See ADR 0109 and the Marine Terminology rule in AGENTS.md.\n\n${offences.join('\n')}\n`,
    ).toEqual([])
  })

  it('actually scans the pages it claims to, so a passing result means something', () => {
    // Guards against the test silently passing because the docs root moved or
    // the walk broke: if this ever finds nothing, the check above is vacuous.
    const scanned = USER_FACING_DIRS.flatMap((d) => markdownFilesUnder(join(docsRoot(), d)))
    expect(scanned.length).toBeGreaterThan(10)
  })
})

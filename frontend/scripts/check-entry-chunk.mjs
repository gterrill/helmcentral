#!/usr/bin/env node
// Build guard: every chunk that loads eagerly - the entry chunk
// dist/index.html loads directly (via <script type="module" src="...">),
// plus every chunk that entry chunk (or one of its own static imports)
// pulls in with a static `import`/`export ... from` - must be parseable by
// the wall display kiosk's browser, WPE WebKit 2.38.5, a Safari 16.0-era
// JavaScriptCore. That engine throws a SyntaxError - and refuses to run
// *any* of the script, blanking the kiosk - on regex lookbehind assertions,
// `(?<=...)` / `(?<!...)`, unsupported before Safari 16.4. Unlike most
// syntax, an invalid regex literal is an early (parse-time) error per the
// ECMAScript grammar, so one bad literal anywhere in an eagerly loaded
// chunk takes down the whole module graph, not just the call site that used
// it.
//
// This is exactly what shipped once already: react-markdown's dependency
// tree (mdast-util-gfm-autolink-literal) carried a lookbehind literal into
// the entry chunk and blanked the kiosk. The fix was code-splitting the
// markdown renderers behind React.lazy so that literal no longer ships in
// the entry chunk (see assistant-markdown.tsx / help-markdown.tsx and
// docs/adr/0046-frontend-build-toolchain-and-css-browser-floor.md's
// addendum). This script is the mechanical guard so the next dependency (or
// hand-written regex) that reintroduces the pattern into an eagerly loaded
// chunk fails the build instead of shipping.
//
// manualChunks (vite.config.ts) means the entry chunk is no longer the only
// thing that loads before the kiosk gets a chance to render anything: it
// statically imports vendor chunks like dashboard-vendor and map-vendor, and
// those in turn can statically import further chunks. A React.lazy() panel
// (AlarmsDrawer, SettingsPage, ...) loads through a *dynamic* `import()`
// instead, deferred until the panel is actually opened, so a lookbehind
// sitting only in one of those (e.g. markdown-vendor, pulled in by the
// lazy-loaded markdown renderers) never reaches the kiosk at startup and is
// out of scope for this guard on purpose - see the walk below.
//
// APPROACH
// --------
// A whole-file substring search for "(?<=" would false-positive constantly:
// a 2.6 MB minified bundle carries plenty of string literals that
// legitimately contain that substring as plain text - a quoted regex
// example, or another library's error message. So this script tracks enough
// lexical state to tell "inside a regex literal" apart from "inside a
// string, template literal, or comment": a single left-to-right character
// scan that recognises line/block comments, '...'/"..." strings, `...`
// template literals (including arbitrarily nested `${...}` interpolation,
// via a small stack), and regex literals.
//
// This is deliberately not a real parser, and it has two known limits:
//
//   1. Division vs. regex-literal disambiguation ("is this `/` the start of
//      a regex, or a division operator?") is the same ambiguity every real
//      JS tokenizer resolves using parser state. This script uses the
//      standard shallow heuristic instead: look at the previous significant
//      character (and, for keywords like `return`, the previous word) and
//      decide from a fixed set of "a regex can start here" tokens. It is not
//      exhaustive (e.g. `}` closing an object literal vs. a block are
//      indistinguishable here), and on genuine ambiguity it is biased toward
//      treating the `/` as a regex literal - a missed real regex is a false
//      negative that could ship a blank kiosk again; a `/` wrongly treated
//      as a regex-start is merely a wasted scan.
//   2. A regex literal built by string concatenation or interpolation
//      (`new RegExp('(?<' + '=x)')`) cannot be caught by any syntactic scan,
//      including a real parser's - the pattern does not exist as source text
//      until runtime. Out of scope, and it always will be for this kind of
//      guard.
//
// A misfired "this `/` starts a regex" guess could otherwise run away
// scanning for a closing `/` across the rest of a very long minified line
// (this project's entry chunk has single lines past 700 KB). A genuine
// regex literal in bundler output is never remotely that long, so the scan
// for a closing delimiter gives up after MAX_REGEX_LITERAL_SCAN characters
// and falls back to treating the `/` as an ordinary character - bounding
// the cost of a wrong guess without giving up any real detection.
//
// No existing parser dependency (acorn, esprima, meriyah, ...) is a direct
// devDependency of this project; acorn/espree are present only transitively
// (via eslint) and could disappear on an unrelated devDependency bump. For
// one narrow pattern, this hand-rolled scanner is easier to read, review
// and trust than adding a dependency on that unstable a foundation.

import { existsSync, readFileSync } from 'node:fs'
import { basename, dirname, join, resolve as resolvePath } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const MAX_REGEX_LITERAL_SCAN = 4096
const CONTEXT_RADIUS = 60

// Previous significant character after which a `/` can only be a regex
// literal (never division): opening brackets, most operators, and
// separators. Deliberately excludes identifier/number characters, `)` and
// `]` (the common "a value just ended" cases) which are treated as division.
const REGEX_OK_AFTER_CHAR = new Set([
  '(', ',', '=', ':', ';', '[', '!', '&', '|', '?', '{', '}',
  '+', '-', '*', '%', '^', '~', '<', '>',
])

// Keywords after which a `/' starts a regex even though the previous
// character is alphanumeric (part of the keyword itself), e.g. `return /x/`.
const REGEX_OK_AFTER_WORD = new Set([
  'return', 'typeof', 'instanceof', 'in', 'of', 'new', 'delete',
  'void', 'throw', 'yield', 'case', 'do', 'else',
])

function isWordChar(ch) {
  return (
    (ch >= 'a' && ch <= 'z') ||
    (ch >= 'A' && ch <= 'Z') ||
    (ch >= '0' && ch <= '9') ||
    ch === '_' ||
    ch === '$'
  )
}

function contextSnippet(source, matchStart, matchLength) {
  const start = Math.max(0, matchStart - CONTEXT_RADIUS)
  const end = Math.min(source.length, matchStart + matchLength + CONTEXT_RADIUS)
  const prefix = start > 0 ? '...' : ''
  const suffix = end < source.length ? '...' : ''
  return prefix + source.slice(start, end) + suffix
}

/**
 * Scans built JS source text for regex-literal occurrences of Safari
 * 16.0-breaking syntax: lookbehind assertions, `(?<=...)` / `(?<!...)`.
 * Occurrences of either substring inside strings, template literals, or
 * comments are not reported - see the file header for the scanning approach
 * and its deliberately out-of-scope cases.
 *
 * @param {string} source
 * @returns {Array<{kind: 'lookbehind', pattern: string, index: number, snippet: string}>}
 */
export function scanForViolations(source) {
  const violations = []
  const len = source.length
  // Stack of open `${...}` template interpolations. Each frame tracks how
  // many *nested* `{`/`}` pairs have opened inside that interpolation, so a
  // `}` at depth 0 is recognised as the one that closes the interpolation
  // (and hands scanning back to that template literal's string part)
  // rather than a nested block or object literal.
  const templateExprStack = []

  let i = 0
  let lastChar = ''
  let lastWord = ''

  function regexCanStartHere() {
    if (lastChar === '') return true // start of file
    if (REGEX_OK_AFTER_WORD.has(lastWord)) return true
    if (isWordChar(lastChar) || lastChar === ')' || lastChar === ']') return false
    if (REGEX_OK_AFTER_CHAR.has(lastChar)) return true
    // Anything else (rare, ambiguous punctuation) - bias toward treating it
    // as a regex literal rather than silently missing one.
    return true
  }

  // Scans the literal-text part of a template literal starting at `i`
  // (already positioned just past the opening backtick, or just past a
  // `}` that closed an interpolation). Stops - and returns to the outer
  // loop - on an unescaped closing backtick or the start of `${`.
  function scanTemplateStringPart() {
    while (i < len) {
      const c = source[i]
      if (c === '\\') {
        i += 2
        continue
      }
      if (c === '`') {
        i += 1
        lastChar = '`'
        lastWord = ''
        return
      }
      if (c === '$' && source[i + 1] === '{') {
        i += 2
        templateExprStack.push({ braceDepth: 0 })
        return
      }
      i += 1
    }
    // Unterminated template literal at EOF: nothing more to scan.
  }

  // Attempts to read a regex literal starting at the `/` at index `start`.
  // Returns null if no unescaped closing `/` turns up within
  // MAX_REGEX_LITERAL_SCAN characters (almost certainly a division that was
  // misclassified as a possible regex start, not a genuine literal).
  function scanRegexLiteral(start) {
    let j = start + 1
    let inClass = false
    let closed = false
    const limit = Math.min(len, start + 1 + MAX_REGEX_LITERAL_SCAN)
    while (j < limit) {
      const c = source[j]
      if (c === '\\') {
        j += 2
        continue
      }
      if (c === '\n') break // unterminated on this line: not a real literal
      if (inClass) {
        if (c === ']') inClass = false
        j += 1
        continue
      }
      if (c === '[') {
        inClass = true
        j += 1
        continue
      }
      if (c === '/') {
        closed = true
        j += 1
        break
      }
      j += 1
    }
    if (!closed) return null
    const body = source.slice(start + 1, j - 1)
    // Consume trailing flags (e.g. `/x/gi`) so the outer scan resumes after
    // them rather than reinterpreting a flag letter as the start of a new
    // token.
    let k = j
    while (k < len && /[a-zA-Z]/.test(source[k])) k += 1
    return { end: k, body }
  }

  function recordRegexViolations(body, bodyStart) {
    const lookbehindPatterns = ['(?<=', '(?<!']
    for (const pattern of lookbehindPatterns) {
      let from = 0
      let idx
      while ((idx = body.indexOf(pattern, from)) !== -1) {
        const absoluteIndex = bodyStart + idx
        violations.push({
          kind: 'lookbehind',
          pattern,
          index: absoluteIndex,
          snippet: contextSnippet(source, absoluteIndex, pattern.length),
        })
        from = idx + 1
      }
    }
  }

  while (i < len) {
    const ch = source[i]

    if (ch === ' ' || ch === '\t' || ch === '\n' || ch === '\r') {
      i += 1
      continue
    }

    if (isWordChar(ch)) {
      let j = i + 1
      while (j < len && isWordChar(source[j])) j += 1
      lastWord = source.slice(i, j)
      lastChar = source[j - 1]
      i = j
      continue
    }

    // Line comment.
    if (ch === '/' && source[i + 1] === '/') {
      let j = i + 2
      while (j < len && source[j] !== '\n') j += 1
      i = j
      continue
    }

    // Block comment.
    if (ch === '/' && source[i + 1] === '*') {
      const end = source.indexOf('*/', i + 2)
      i = end === -1 ? len : end + 2
      continue
    }

    // String literal.
    if (ch === "'" || ch === '"') {
      const quote = ch
      let j = i + 1
      while (j < len) {
        if (source[j] === '\\') {
          j += 2
          continue
        }
        if (source[j] === quote) {
          j += 1
          break
        }
        j += 1
      }
      i = j
      lastChar = quote
      lastWord = ''
      continue
    }

    // Template literal open.
    if (ch === '`') {
      i += 1
      scanTemplateStringPart()
      continue
    }

    // `}` that closes an open `${...}` interpolation at depth 0 hands
    // scanning back to that template literal's string part.
    if (
      ch === '}' &&
      templateExprStack.length > 0 &&
      templateExprStack[templateExprStack.length - 1].braceDepth === 0
    ) {
      templateExprStack.pop()
      i += 1
      lastChar = '}'
      lastWord = ''
      scanTemplateStringPart()
      continue
    }

    if (ch === '{') {
      if (templateExprStack.length > 0) templateExprStack[templateExprStack.length - 1].braceDepth += 1
      lastChar = '{'
      lastWord = ''
      i += 1
      continue
    }

    if (ch === '}') {
      if (templateExprStack.length > 0) templateExprStack[templateExprStack.length - 1].braceDepth -= 1
      lastChar = '}'
      lastWord = ''
      i += 1
      continue
    }

    if (ch === '/') {
      if (regexCanStartHere()) {
        const result = scanRegexLiteral(i)
        if (result) {
          recordRegexViolations(result.body, i + 1)
          i = result.end
          lastChar = '/'
          lastWord = ''
          continue
        }
      }
      lastChar = '/'
      lastWord = ''
      i += 1
      continue
    }

    lastChar = ch
    lastWord = ''
    i += 1
  }

  return violations
}

/**
 * Finds the single `<script type="module" ... src="...">` tag in a built
 * index.html and returns its src attribute. Throws if there is not exactly
 * one such tag, or if it has no src - this project's build is expected to
 * emit exactly one, and guessing which of several is the real entry chunk
 * would defeat the point of this guard.
 *
 * @param {string} html
 * @returns {string}
 */
export function findModuleEntryScriptSrc(html) {
  const scriptTagRe = /<script\b[^>]*>/gi
  const moduleTags = []
  let match
  while ((match = scriptTagRe.exec(html)) !== null) {
    if (/\btype\s*=\s*["']module["']/i.test(match[0])) {
      moduleTags.push(match[0])
    }
  }

  if (moduleTags.length !== 1) {
    throw new Error(
      `expected exactly one <script type="module" src="..."> tag in dist/index.html, found ${moduleTags.length}. ` +
        'Refusing to guess which chunk is the entry chunk - update this script if the build now legitimately emits a different number.',
    )
  }

  const srcMatch = /\bsrc\s*=\s*["']([^"']+)["']/i.exec(moduleTags[0])
  if (!srcMatch) {
    throw new Error(`found the module <script> tag but it has no src attribute: ${moduleTags[0]}`)
  }
  return srcMatch[1]
}

// STATIC IMPORT GRAPH
// --------------------
// A statically imported chunk parses the moment its importer does - that's
// the whole reason it needs the same scan as the entry chunk. A dynamically
// imported one (`import("./x.js")`, and Vite's own
// `__vitePreload(()=>import("./x.js"),...)` wrapper around a React.lazy()
// panel) doesn't run at all until something calls it, so it must not be
// followed here even though the two look almost identical in minified
// output.
//
// extractStaticImportSpecifiers below tells them apart the same way
// scanForViolations tells a regex literal from a division above: not a real
// parser, just enough lexical anchoring to be right on actual bundler
// output. It looks for the `import`/`export` keyword NOT immediately
// followed by `(` (that's what rules out both plain `import("./x.js")` and
// the __vitePreload-wrapped form - both always call `import(`), then takes
// everything up to the next `from "..."` (or, for a bare `import "./x.js"`,
// the very next quoted string) as the specifier. Two things fall out of that
// on their own rather than needing special-casing: a dynamic import's
// specifier is only ever legal as a parenthesised expression - never a bare
// `from "..."` clause - so `import(` is unambiguous grounds for exclusion;
// and Vite's own dependency-manifest arrays (`m.f=["assets/a.js","assets/b.js"]`,
// used to preload a lazy chunk's own dependencies) are just string literals
// with no `import`/`export` keyword anywhere near them, so they were never
// going to match in the first place.
//
// Known limits, same spirit as scanForViolations' own:
//   - This is a keyword anchor, not a parser. `export const from = "./x.js"`
//     would misread as a re-export if real minified output ever produced
//     it - it doesn't; bundlers do not emit that. The one thing every real
//     import/export declaration guarantees that this scan leans on is the
//     quote landing immediately (whitespace aside) after the keyword or
//     after `from` - an ordinary assignment like `from = "./x.js"` has an
//     `=` in the way and is correctly skipped.
//   - The import/export clause between the keyword and `from` is matched
//     non-greedily against "anything but a quote, backtick, semicolon, or
//     parenthesis" - no fixed length cap, because a real renamed named-import
//     clause in this project's own vendor chunk runs past 700 characters.
//     Excluding `(`/`)` from that clause is what keeps a `export function
//     foo(){...}` declaration (no `from` involved) from being scanned into
//     the next unrelated import statement.

const BARE_IMPORT_RE = /\bimport\s*(["'])((?:\\.|(?!\1)[^\\])*)\1/g
const FROM_CLAUSE_RE = /\b(?:import|export)\b(?!\s*\()[^'"`;()\n]*?\bfrom\b\s*(["'])((?:\\.|(?!\1)[^\\])*)\1/g

/**
 * Returns the relative (or root-absolute) specifiers of a chunk's STATIC
 * imports and re-exports only: `import ... from "./a.js"`, `import"./a.js"`
 * (bare, no clause), `export ... from "./a.js"`, `export*from"./a.js"` -
 * with or without whitespace, single or double quotes, exactly as minified
 * bundler output writes them. Dynamic `import(...)` calls are excluded on
 * purpose - see the STATIC IMPORT GRAPH section above for how and why, and
 * its documented limits.
 *
 * @param {string} source
 * @returns {string[]}
 */
export function extractStaticImportSpecifiers(source) {
  const specifiers = []
  for (const match of source.matchAll(BARE_IMPORT_RE)) {
    specifiers.push(match[2])
  }
  for (const match of source.matchAll(FROM_CLAUSE_RE)) {
    specifiers.push(match[2])
  }
  return specifiers
}

// Resolves one specifier extracted from `importingChunkPath`'s source to an
// absolute path on disk. A root-absolute specifier (`/assets/a.js`, the form
// Vite emits for cross-chunk references) resolves against distDir, the same
// way the browser would resolve it against the site root; anything else is
// relative to the importing chunk's own directory, per normal ES module
// resolution.
function resolveChunkSpecifier(specifier, importingChunkPath, distDir) {
  if (specifier.startsWith('/')) {
    return resolvePath(distDir, specifier.replace(/^\//, ''))
  }
  return resolvePath(dirname(importingChunkPath), specifier)
}

/**
 * Walks the static-import graph starting at `entryChunkPath` and returns the
 * absolute path of every chunk reachable through a STATIC import/re-export
 * (the entry chunk included) - i.e. every chunk that parses eagerly, before
 * the kiosk gets a chance to render anything. A dynamically imported chunk
 * (a React.lazy() panel, and anything only that panel pulls in) is never
 * enqueued, so it never appears in the result even if it happens to exist on
 * disk.
 *
 * De-duplicates and tolerates cycles via a visited set - a static import
 * cycle isn't expected in practice, but nothing here depends on that.
 *
 * A specifier that resolves to a path with no file on disk is a broken
 * build (or a bug in this scan) and fails loudly rather than being skipped:
 * silently dropping a chunk from the scan is exactly the failure mode this
 * guard exists to prevent.
 *
 * @param {string} entryChunkPath
 * @param {string} distDir
 * @returns {string[]}
 */
export function walkStaticImportGraph(entryChunkPath, distDir) {
  const visited = new Set()
  const chunkPaths = []
  const queue = [{ path: resolvePath(entryChunkPath), importedBy: null }]

  while (queue.length > 0) {
    const { path: chunkPath, importedBy } = queue.shift()
    if (visited.has(chunkPath)) continue
    visited.add(chunkPath)

    if (!existsSync(chunkPath)) {
      const context = importedBy ? ` (statically imported by ${importedBy})` : ''
      throw new Error(`check-entry-chunk: chunk does not exist on disk: ${chunkPath}${context}`)
    }

    chunkPaths.push(chunkPath)
    const source = readFileSync(chunkPath, 'utf8')
    for (const specifier of extractStaticImportSpecifiers(source)) {
      const resolved = resolveChunkSpecifier(specifier, chunkPath, distDir)
      if (!visited.has(resolved)) queue.push({ path: resolved, importedBy: chunkPath })
    }
  }

  return chunkPaths
}

// PER-MODULE CHUNK NAME ASSERTION
// --------------------------------
// The lookbehind scan above is a mechanical property of source text - it
// catches ANY dependency that introduces the pattern, known or not. This
// second guard is narrower and deliberate: certain vite.config.ts
// `codeSplitting.groups` chunks are named for dependency graphs that must
// NEVER load eagerly regardless of what syntax they contain, because their
// sheer size (not a parse error) is the risk. `editor-vendor`
// (frontend/vite.config.ts, ADR 0117) is the first of these - Slate plus
// Plate, the note editor's dependency graph, by far the largest this
// project has ever put behind a React.lazy() boundary. A regression here
// wouldn't blank the kiosk the way a lookbehind literal does; it would just
// make the kiosk (an ODROID on WPE WebKit, ADR 0088/0110) download and
// parse several hundred KB of a rich-text editor it never opens, on every
// boot, forever - silent enough that nobody would notice until someone
// measured kiosk cold-start time.
//
// This is a straightforward name check on eagerly-loaded chunks'
// FILENAMES, not their contents - the codeSplitting group's `name` field
// becomes the chunk's filename prefix (`<name>-<hash>.js`, verified against
// this project's own `dist/assets/` output for every existing group:
// `dashboard-vendor-*.js`, `chart-vendor-*.js`, `markdown-vendor-*.js`,
// ...), so a chunk reachable by a STATIC import (walkStaticImportGraph's
// own result, exactly the set the lookbehind scan above already treats as
// "loads eagerly") whose filename starts with a forbidden group's name is
// definitive proof that group ended up in the eager graph - no source
// inspection needed, unlike the lookbehind case.
const FORBIDDEN_EAGER_CHUNK_NAME_PREFIXES = ['editor-vendor']

/**
 * Filters `chunkPaths` (as returned by walkStaticImportGraph - i.e. every
 * chunk reachable by a STATIC import from the entry chunk) down to the ones
 * whose filename starts with one of `forbiddenNamePrefixes` - a
 * `codeSplitting.groups` chunk name (vite.config.ts) that must never load
 * eagerly.
 *
 * @param {string[]} chunkPaths
 * @param {string[]} forbiddenNamePrefixes
 * @returns {string[]}
 */
export function findForbiddenEagerChunks(chunkPaths, forbiddenNamePrefixes) {
  return chunkPaths.filter((chunkPath) => {
    const name = basename(chunkPath)
    return forbiddenNamePrefixes.some((prefix) => name.startsWith(`${prefix}-`) || name === `${prefix}.js`)
  })
}

function formatViolation(violation, chunkPath) {
  return (
    `Regex lookbehind assertion "${violation.pattern}" found in ${chunkPath}\n` +
    `  ${violation.snippet}`
  )
}

function main() {
  const scriptDir = dirname(fileURLToPath(import.meta.url))
  const distDir = resolvePath(scriptDir, '..', 'dist')
  const indexHtmlPath = join(distDir, 'index.html')

  if (!existsSync(indexHtmlPath)) {
    console.error(`check-entry-chunk: ${indexHtmlPath} does not exist - run "vite build" first.`)
    process.exit(1)
  }

  const html = readFileSync(indexHtmlPath, 'utf8')

  let entrySrc
  try {
    entrySrc = findModuleEntryScriptSrc(html)
  } catch (err) {
    console.error(`check-entry-chunk: ${err.message}`)
    process.exit(1)
    return
  }

  const chunkRelPath = entrySrc.replace(/^\//, '')
  const entryChunkPath = join(distDir, chunkRelPath)

  let chunkPaths
  try {
    chunkPaths = walkStaticImportGraph(entryChunkPath, distDir)
  } catch (err) {
    console.error(`check-entry-chunk: ${err.message}`)
    process.exit(1)
    return
  }

  const forbiddenEagerChunks = findForbiddenEagerChunks(chunkPaths, FORBIDDEN_EAGER_CHUNK_NAME_PREFIXES)
  if (forbiddenEagerChunks.length > 0) {
    console.error(
      `check-entry-chunk: FAILED - found ${forbiddenEagerChunks.length} chunk(s) reachable by a STATIC ` +
        `import from the entry chunk whose name marks them as a chunk that must never load eagerly:`,
    )
    console.error('')
    for (const chunkPath of forbiddenEagerChunks) console.error(`  ${chunkPath}`)
    console.error('')
    console.error(
      'These chunk names (vite.config.ts codeSplitting.groups) are reserved for dependency graphs this ' +
        "project deliberately keeps behind a React.lazy() boundary - editor-vendor is the note editor's " +
        "own Plate/Slate dependency tree (ADR 0117), which the wall-display kiosk never opens. Something " +
        'now imports it (or a module the group\'s test() also matches) via a static `import`/`export ... ' +
        'from`, not a dynamic `import()`, pulling the whole graph into the startup bundle. Find the static ' +
        'import chain and move it behind a dynamic import() instead.',
    )
    process.exit(1)
    return
  }

  const violationsByChunk = []
  for (const chunkPath of chunkPaths) {
    const chunkSource = readFileSync(chunkPath, 'utf8')
    const violations = scanForViolations(chunkSource)
    if (violations.length > 0) violationsByChunk.push({ chunkPath, violations })
  }

  if (violationsByChunk.length === 0) {
    console.log(
      `check-entry-chunk: OK - scanned ${chunkPaths.length} eagerly loaded chunk(s) (entry: ${entrySrc}), ` +
        'no regex lookbehind syntax.',
    )
    process.exit(0)
    return
  }

  const totalViolations = violationsByChunk.reduce((sum, entry) => sum + entry.violations.length, 0)
  console.error(
    `check-entry-chunk: FAILED - found ${totalViolations} occurrence(s) of Safari-16.0-breaking regex ` +
      `lookbehind syntax across ${violationsByChunk.length} of ${chunkPaths.length} eagerly loaded chunk(s).`,
  )
  console.error('')
  for (const { chunkPath, violations } of violationsByChunk) {
    for (const violation of violations) {
      console.error(formatViolation(violation, chunkPath))
      console.error('')
    }
  }
  console.error(
    'Every chunk above loads eagerly - the entry chunk itself, or one of its own static imports - and must ' +
      'parse on the wall display kiosk\'s browser floor, WPE WebKit 2.38.5 / Safari 16.0, which cannot parse this ' +
      'pattern. Move whatever introduced this behind a dynamic import() into a lazily-loaded chunk instead - see ' +
      'docs/adr/0046-frontend-build-toolchain-and-css-browser-floor.md.',
  )
  process.exit(1)
}

const isMainModule = process.argv[1] !== undefined && import.meta.url === pathToFileURL(process.argv[1]).href
if (isMainModule) {
  main()
}

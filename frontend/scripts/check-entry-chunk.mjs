#!/usr/bin/env node
// Build guard: the entry chunk (the module dist/index.html loads directly,
// via <script type="module" src="...">) must be parseable by the wall
// display kiosk's browser, WPE WebKit 2.38.5, a Safari 16.0-era
// JavaScriptCore. That engine throws a SyntaxError - and refuses to run
// *any* of the script, blanking the kiosk - on regex lookbehind assertions,
// `(?<=...)` / `(?<!...)`, unsupported before Safari 16.4. Unlike most
// syntax, an invalid regex literal is an early (parse-time) error per the
// ECMAScript grammar, so one bad literal anywhere in the file takes down
// the whole module, not just the call site that used it.
//
// This is exactly what shipped once already: react-markdown's dependency
// tree (mdast-util-gfm-autolink-literal) carried a lookbehind literal into
// the entry chunk and blanked the kiosk. The fix was code-splitting the
// markdown renderers behind React.lazy so that literal no longer ships in
// the entry chunk (see assistant-markdown.tsx / manual-markdown.tsx and
// docs/adr/0046-frontend-build-toolchain-and-css-browser-floor.md's
// addendum). This script is the mechanical guard so the next dependency (or
// hand-written regex) that reintroduces the pattern into the entry chunk
// fails the build instead of shipping.
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
import { dirname, join, resolve as resolvePath } from 'node:path'
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
  const chunkPath = join(distDir, chunkRelPath)
  if (!existsSync(chunkPath)) {
    console.error(
      `check-entry-chunk: entry chunk "${entrySrc}" referenced by index.html does not exist at ${chunkPath}.`,
    )
    process.exit(1)
    return
  }

  const chunkSource = readFileSync(chunkPath, 'utf8')
  const violations = scanForViolations(chunkSource)

  if (violations.length === 0) {
    console.log(`check-entry-chunk: OK - ${entrySrc} has no regex lookbehind syntax.`)
    process.exit(0)
    return
  }

  console.error(
    `check-entry-chunk: FAILED - found ${violations.length} occurrence(s) of Safari-16.0-breaking regex ` +
      'lookbehind syntax in the entry chunk.',
  )
  console.error('')
  for (const violation of violations) {
    console.error(formatViolation(violation, chunkPath))
    console.error('')
  }
  console.error(
    'The entry chunk loads synchronously and must parse on the wall display kiosk\'s browser floor, ' +
      'WPE WebKit 2.38.5 / Safari 16.0, which cannot parse this pattern. Move whatever introduced this behind a ' +
      'dynamic import() into a lazily-loaded chunk instead - see ' +
      'docs/adr/0046-frontend-build-toolchain-and-css-browser-floor.md.',
  )
  process.exit(1)
}

const isMainModule = process.argv[1] !== undefined && import.meta.url === pathToFileURL(process.argv[1]).href
if (isMainModule) {
  main()
}

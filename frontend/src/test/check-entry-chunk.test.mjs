import { describe, it, expect, afterEach } from 'vitest'
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, dirname } from 'node:path'
import {
  scanForViolations,
  findModuleEntryScriptSrc,
  extractStaticImportSpecifiers,
  walkStaticImportGraph,
} from '../../scripts/check-entry-chunk.mjs'

// This is the guard from frontend/scripts/check-entry-chunk.mjs that fails
// the build if the entry chunk carries a regex lookbehind assertion WPE
// WebKit 2.38.5 (the wall-display kiosk's browser, Safari 16.0-era) cannot
// parse. These fixtures prove both halves of the contract the script's file
// header describes: it catches the real pattern (a genuine regex literal),
// and it leaves alone the false positives a naive substring search would
// trip on (the same substring appearing in a string or template literal
// instead, or a `/` that is actually division).
//
// A plain .mjs file, not .test.ts: frontend/scripts/check-entry-chunk.mjs
// lives outside tsconfig.json's "include": ["src"], so a .ts test importing
// it would fail tsc with no declaration file for the module. Vitest picks up
// .test.mjs the same as .test.ts (matches its default include glob), and
// tsc never sees this file at all, so nothing needs an allowJs flip just for
// this one script.

describe('scanForViolations - lookbehind assertions', () => {
  it('flags a positive lookbehind inside a real regex literal', () => {
    const source = String.raw`var re = /(?<=^|\s)x/;`
    const violations = scanForViolations(source)

    expect(violations).toHaveLength(1)
    expect(violations[0]).toMatchObject({ kind: 'lookbehind', pattern: '(?<=' })
  })

  it('flags a negative lookbehind inside a real regex literal', () => {
    const source = String.raw`var re = /(?<!foo)bar/;`
    const violations = scanForViolations(source)

    expect(violations).toHaveLength(1)
    expect(violations[0]).toMatchObject({ kind: 'lookbehind', pattern: '(?<!' })
  })

  it('does not flag the same substring inside a plain string literal', () => {
    const source = String.raw`var s = "(?<=this is just a string)";`
    const violations = scanForViolations(source)

    expect(violations).toHaveLength(0)
  })

  it('does not flag the same substring inside a template literal', () => {
    const source = String.raw`var s = ` + '`text with (?<=fake lookbehind) inside`' + ';'
    const violations = scanForViolations(source)

    expect(violations).toHaveLength(0)
  })

  it('does not flag the same substring inside a line comment', () => {
    const source = String.raw`// example: (?<=foo)bar` + '\nvar x = 1;'
    const violations = scanForViolations(source)

    expect(violations).toHaveLength(0)
  })

  it('reports a context snippet around the match', () => {
    const source = String.raw`var re = /(?<=^|\s)x/;`
    const violations = scanForViolations(source)

    expect(violations[0].snippet).toContain('(?<=^|\\s)x')
  })
})

describe('scanForViolations - division vs. regex disambiguation', () => {
  it('does not treat a division after a closing paren as a regex literal', () => {
    // `(a + b) / c` - the `/` here is division, following the `)` that
    // closes the parenthesised expression. If this were misread as the
    // start of a regex literal, the scanner would run off looking for a
    // closing `/` and could swallow or misclassify the rest of the line.
    const source = 'function avg(a, b, c) { return (a + b) / c; }'
    const violations = scanForViolations(source)

    expect(violations).toHaveLength(0)
  })

  it('still finds a real lookbehind later on the same line as a division', () => {
    const source = String.raw`var avg = (a + b) / c; var re = /(?<=x)y/;`
    const violations = scanForViolations(source)

    expect(violations).toHaveLength(1)
    expect(violations[0].kind).toBe('lookbehind')
  })

  it('returns no violations for a clean chunk with an ordinary division', () => {
    const source = 'function add(a, b) { return a / b; }\nvar re = /^[a-z]+$/i;'
    const violations = scanForViolations(source)

    expect(violations).toHaveLength(0)
  })
})

describe('scanForViolations - mixed content', () => {
  it('finds a real violation even when preceded by look-alike strings and comments', () => {
    const source = [
      '// (?<=not a real lookbehind)',
      'var a = "(?<!also not real)";',
      'var b = `(?<=also not real in a template)`;',
      'var re = /(?<=real)x/;',
    ].join('\n')

    const violations = scanForViolations(source)

    expect(violations).toHaveLength(1)
    expect(violations[0].kind).toBe('lookbehind')
  })
})

describe('findModuleEntryScriptSrc', () => {
  it('returns the src of the single module script tag', () => {
    const html = `<!doctype html><html><head>
      <script type="module" crossorigin src="/assets/index-ABC123.js"></script>
      <link rel="stylesheet" href="/assets/index-XYZ.css">
    </head><body></body></html>`

    expect(findModuleEntryScriptSrc(html)).toBe('/assets/index-ABC123.js')
  })

  it('throws when there is no module script tag', () => {
    const html = '<script src="/assets/legacy.js"></script>'

    expect(() => findModuleEntryScriptSrc(html)).toThrow(/found 0/)
  })

  it('throws when there is more than one module script tag', () => {
    const html = [
      '<script type="module" src="/assets/a.js"></script>',
      '<script type="module" src="/assets/b.js"></script>',
    ].join('\n')

    expect(() => findModuleEntryScriptSrc(html)).toThrow(/found 2/)
  })
})

// extractStaticImportSpecifiers has to work on real Rollup/Vite minified
// output, which drops every optional space and can put dozens of renamed
// bindings between `import`/`export` and `from` (a real chunk in this
// project has one 718-character import clause). The fixtures below are
// written the way that output actually looks (see dist/assets/index-*.js
// after `npm run build`), not a hand-wavy approximation of it.
describe('extractStaticImportSpecifiers', () => {
  it('extracts a default import with a from clause, minified (no space before the quote)', () => {
    const source = 'import a from"./x.js";'
    expect(extractStaticImportSpecifiers(source)).toEqual(['./x.js'])
  })

  it('extracts a minified named import with no spaces at all', () => {
    const source = 'import{a,b}from"./x.js";'
    expect(extractStaticImportSpecifiers(source)).toEqual(['./x.js'])
  })

  it('extracts a long renamed named-import clause the same way real vendor chunks write it', () => {
    // Mirrors the shape (not the exact length) of the ui-vendor import this
    // project's own entry chunk emits: dozens of "X as Y" renames with no
    // whitespace, still ending in `}from"..."`.
    const source = 'import{a as A,b as B,c as C,d as D,e as E}from"./ui-vendor.js";'
    expect(extractStaticImportSpecifiers(source)).toEqual(['./ui-vendor.js'])
  })

  it('extracts a bare side-effect import (no clause, no from)', () => {
    const source = 'import"./x.js";'
    expect(extractStaticImportSpecifiers(source)).toEqual(['./x.js'])
  })

  it('extracts a named export-from re-export', () => {
    const source = 'export{a,b}from"./x.js";'
    expect(extractStaticImportSpecifiers(source)).toEqual(['./x.js'])
  })

  it('extracts a star export-from re-export', () => {
    const source = 'export*from"./x.js";'
    expect(extractStaticImportSpecifiers(source)).toEqual(['./x.js'])
  })

  it('extracts an ordinary, non-minified import with single quotes', () => {
    const source = "import { a } from './x.js';"
    expect(extractStaticImportSpecifiers(source)).toEqual(['./x.js'])
  })

  it('does not extract a dynamic import() call', () => {
    const source = "const p = import('./x.js');"
    expect(extractStaticImportSpecifiers(source)).toEqual([])
  })

  it('does not extract a __vitePreload-wrapped dynamic import with a template-literal specifier', () => {
    // The exact shape Vite's build emits for a React.lazy()-driven chunk:
    // a template literal specifier (only legal for a dynamic import), plus
    // a plain-string dependency-manifest array beside it that isn't an
    // import/export statement at all.
    const source = '__vite__mapDeps.f=["assets/lazy.js"];' +
      'const C=React.lazy(()=>__vitePreload(()=>import(`./lazy.js`),__vite__mapDeps([0])));'
    expect(extractStaticImportSpecifiers(source)).toEqual([])
  })

  it('extracts only the static imports when a static and a dynamic import both appear in the same source', () => {
    const source = 'import a from"./eager.js";' +
      'const lazy=()=>import(`./lazy.js`);' +
      'export{a}from"./reexport.js";'
    expect(extractStaticImportSpecifiers(source)).toEqual(['./eager.js', './reexport.js'])
  })
})

// walkStaticImportGraph resolves specifiers on disk, so these use real temp
// directories (node:fs mkdtemp) rather than fixture strings.
describe('walkStaticImportGraph', () => {
  const tempDirs = []

  afterEach(() => {
    for (const dir of tempDirs) rmSync(dir, { recursive: true, force: true })
    tempDirs.length = 0
  })

  function makeTempDist() {
    const dir = mkdtempSync(join(tmpdir(), 'check-entry-chunk-test-'))
    tempDirs.push(dir)
    return dir
  }

  function writeChunk(distDir, relPath, content) {
    const filePath = join(distDir, relPath)
    mkdirSync(dirname(filePath), { recursive: true })
    writeFileSync(filePath, content)
    return filePath
  }

  it('walks a nested static import chain and includes every chunk it finds', () => {
    const distDir = makeTempDist()
    const entryPath = writeChunk(distDir, 'assets/entry.js', 'import"./a.js";')
    const aPath = writeChunk(distDir, 'assets/a.js', 'import"./b.js";')
    const bPath = writeChunk(distDir, 'assets/b.js', 'console.log(1);')

    const chunkPaths = walkStaticImportGraph(entryPath, distDir)

    expect(new Set(chunkPaths)).toEqual(new Set([entryPath, aPath, bPath]))
  })

  it('de-duplicates and tolerates a cycle instead of looping forever', () => {
    const distDir = makeTempDist()
    const entryPath = writeChunk(distDir, 'assets/entry.js', 'import"./a.js";')
    const aPath = writeChunk(distDir, 'assets/a.js', 'import"./entry.js";')

    const chunkPaths = walkStaticImportGraph(entryPath, distDir)

    expect(chunkPaths).toHaveLength(2)
    expect(new Set(chunkPaths)).toEqual(new Set([entryPath, aPath]))
  })

  it('resolves an absolute /assets/... specifier against distDir', () => {
    const distDir = makeTempDist()
    const entryPath = writeChunk(distDir, 'assets/entry.js', 'import"/assets/a.js";')
    const aPath = writeChunk(distDir, 'assets/a.js', 'console.log(1);')

    const chunkPaths = walkStaticImportGraph(entryPath, distDir)

    expect(new Set(chunkPaths)).toEqual(new Set([entryPath, aPath]))
  })

  it('does not follow a dynamically imported chunk, even one that exists on disk', () => {
    const distDir = makeTempDist()
    const entryPath = writeChunk(
      distDir,
      'assets/entry.js',
      'import"./eager.js";const lazy=()=>import(`./lazy.js`);',
    )
    const eagerPath = writeChunk(distDir, 'assets/eager.js', 'console.log(1);')
    writeChunk(distDir, 'assets/lazy.js', 'console.log(2);')

    const chunkPaths = walkStaticImportGraph(entryPath, distDir)

    expect(new Set(chunkPaths)).toEqual(new Set([entryPath, eagerPath]))
  })

  it('throws when a statically imported chunk does not exist on disk', () => {
    const distDir = makeTempDist()
    const entryPath = writeChunk(distDir, 'assets/entry.js', 'import"./missing.js";')

    expect(() => walkStaticImportGraph(entryPath, distDir)).toThrow(/missing\.js/)
  })

  it('does not throw over a chunk that is only ever referenced dynamically and is missing', () => {
    const distDir = makeTempDist()
    const entryPath = writeChunk(
      distDir,
      'assets/entry.js',
      'const lazy=()=>import(`./never-written.js`);',
    )

    expect(() => walkStaticImportGraph(entryPath, distDir)).not.toThrow()
  })
})

// Integration: a violation reachable only through a static import is part of
// what ships to the kiosk on startup and must be caught; a violation behind
// a dynamic import (React.lazy) never loads eagerly and must not be.
describe('static-import closure vs. scanForViolations', () => {
  const tempDirs = []

  afterEach(() => {
    for (const dir of tempDirs) rmSync(dir, { recursive: true, force: true })
    tempDirs.length = 0
  })

  function makeTempDist() {
    const dir = mkdtempSync(join(tmpdir(), 'check-entry-chunk-test-'))
    tempDirs.push(dir)
    return dir
  }

  function writeChunk(distDir, relPath, content) {
    const filePath = join(distDir, relPath)
    mkdirSync(dirname(filePath), { recursive: true })
    writeFileSync(filePath, content)
    return filePath
  }

  it('reaches a violation behind a static import but not one behind a dynamic import', () => {
    const distDir = makeTempDist()
    const entryPath = writeChunk(
      distDir,
      'assets/entry.js',
      'import"./eager-bad.js";const lazy=()=>import(`./lazy-bad.js`);',
    )
    writeChunk(distDir, 'assets/eager-bad.js', 'var re=/(?<=x)y/;')
    writeChunk(distDir, 'assets/lazy-bad.js', 'var re=/(?<=x)y/;')

    const chunkPaths = walkStaticImportGraph(entryPath, distDir)

    const violationsByChunk = chunkPaths.map((chunkPath) => ({
      chunkPath,
      violations: scanForViolations(readFileSync(chunkPath, 'utf8')),
    }))

    const eagerEntry = violationsByChunk.find((entry) => entry.chunkPath.endsWith('eager-bad.js'))
    expect(eagerEntry.violations).toHaveLength(1)

    const lazyEntry = violationsByChunk.find((entry) => entry.chunkPath.endsWith('lazy-bad.js'))
    expect(lazyEntry).toBeUndefined()
  })
})

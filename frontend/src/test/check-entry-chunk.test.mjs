import { describe, it, expect } from 'vitest'
import { scanForViolations, findModuleEntryScriptSrc } from '../../scripts/check-entry-chunk.mjs'

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

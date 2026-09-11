import { describe, it, expect } from 'vitest'

import { extractSpokenSummary, stripWakeWord } from '@/lib/spoken-summary'

// ADR 0093 voice phase: the backend appends `## Spoken summary` (a heading,
// exact case, followed by up to three plain sentences) to a reply's markdown
// whenever the question came in with `spoken: true`. This module is the
// frontend's half of that contract - pulling the section out to read aloud -
// plus the wake-word parser for push-to-talk and "Hey Mate" mode.

describe('extractSpokenSummary', () => {
  it('returns the text after the heading, trimmed', () => {
    const markdown = [
      '## Passage plan',
      '',
      'Hamilton Island to Gloucester Island looks fine for Monday.',
      '',
      '## Spoken summary',
      '',
      'Fine for Monday. Light winds from the southeast. Pick the morning tide.',
    ].join('\n')

    expect(extractSpokenSummary(markdown)).toBe(
      'Fine for Monday. Light winds from the southeast. Pick the morning tide.',
    )
  })

  it('is case-insensitive on the heading text', () => {
    const markdown = '## SPOKEN SUMMARY\n\nAll clear.'
    expect(extractSpokenSummary(markdown)).toBe('All clear.')
  })

  it('stops at the next level-2 heading rather than reading to the end of the document', () => {
    const markdown = [
      '## Spoken summary',
      '',
      'Fine for Monday.',
      '',
      '## Cost',
      '',
      'anthropic/claude-sonnet-4.5 · 1,200 tokens',
    ].join('\n')

    expect(extractSpokenSummary(markdown)).toBe('Fine for Monday.')
  })

  it('does not stop at a level-3 heading nested under the summary', () => {
    const markdown = [
      '## Spoken summary',
      '',
      'Fine for Monday.',
      '',
      '### Not actually a new section',
      '',
      'More detail.',
    ].join('\n')

    expect(extractSpokenSummary(markdown)).toBe('Fine for Monday.\n\n### Not actually a new section\n\nMore detail.')
  })

  it('strips emphasis, links and inline code to plain text', () => {
    const markdown = '## Spoken summary\n\n**Fine** for _Monday_. Check the [tide table](https://example.com) and `depth.sounder`.'

    expect(extractSpokenSummary(markdown)).toBe(
      'Fine for Monday. Check the tide table and depth.sounder.',
    )
  })

  it('returns null when there is no spoken summary section', () => {
    const markdown = '## Passage plan\n\nHamilton Island to Gloucester Island looks fine for Monday.'
    expect(extractSpokenSummary(markdown)).toBeNull()
  })

  it('returns null when the heading is present but the section is blank', () => {
    const markdown = '## Spoken summary\n\n'
    expect(extractSpokenSummary(markdown)).toBeNull()
  })

  it('does not match a heading of the wrong level', () => {
    const markdown = '### Spoken summary\n\nFine for Monday.'
    expect(extractSpokenSummary(markdown)).toBeNull()
  })
})

describe('stripWakeWord', () => {
  it('strips "Hey Mate," and returns the trimmed remainder', () => {
    expect(stripWakeWord('Hey Mate, how does the passage look')).toBe('how does the passage look')
  })

  it('is case-insensitive and tolerates no comma', () => {
    expect(stripWakeWord('hey mate how does the passage look')).toBe('how does the passage look')
  })

  it('recognises "Hi Mate"', () => {
    expect(stripWakeWord('Hi Mate, what is the tide doing')).toBe('what is the tide doing')
  })

  it('recognises "Ok Mate" and "Okay Mate"', () => {
    expect(stripWakeWord('Ok Mate, raise the anchor watch radius')).toBe('raise the anchor watch radius')
    expect(stripWakeWord('Okay Mate, raise the anchor watch radius')).toBe('raise the anchor watch radius')
  })

  it('returns an empty string when the wake phrase stands alone', () => {
    expect(stripWakeWord('Mate.')).toBe('')
    expect(stripWakeWord('Hey Mate')).toBe('')
    expect(stripWakeWord('Hey Mate.')).toBe('')
  })

  it('matches bare "mate" followed by punctuation', () => {
    expect(stripWakeWord('Mate, what is the wind doing')).toBe('what is the wind doing')
  })

  it('matches bare "mate" followed by a pause word', () => {
    expect(stripWakeWord('Mate um what is the wind doing')).toBe('what is the wind doing')
  })

  it('does not match bare "mate" with no pause or punctuation, to avoid tripping on ordinary address', () => {
    expect(stripWakeWord('Mate how is the wind looking')).toBeNull()
    expect(stripWakeWord('no mate I reckon we hold off')).toBeNull()
  })

  it('does not match "mate" as part of a longer word', () => {
    expect(stripWakeWord('checkmate')).toBeNull()
  })

  it('returns null when there is no wake word at all', () => {
    expect(stripWakeWord("what's the tide doing")).toBeNull()
  })

  it('returns null for a blank transcript', () => {
    expect(stripWakeWord('')).toBeNull()
    expect(stripWakeWord('   ')).toBeNull()
  })
})

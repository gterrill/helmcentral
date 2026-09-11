// ADR 0093 voice phase. Two small, pure parsers with nothing in common
// except that both exist to make voice interaction possible without a
// server round trip:
//
// - extractSpokenSummary reads the `## Spoken summary` section the backend
//   appends to a reply's markdown whenever the question was `spoken: true`
//   (see hooks/use-assistant-chat.ts), so the frontend can hand a short,
//   plain-text answer to speechSynthesis instead of the full briefing.
// - stripWakeWord recognises a leading "Hey Mate" (and near variants) at the
//   front of a speech-recognition transcript, for push-to-talk (where the
//   wake word is optional) and for the always-listening mode (where it is
//   the trigger).

const HEADING_RE = /^##\s+spoken summary\s*$/i
const ANY_H2_RE = /^\s{0,3}##(?:\s|$)/

/**
 * Pulls the plain-text body of a `## Spoken summary` section out of a
 * reply's markdown - case-insensitive on the heading, running to the end of
 * the document or the next level-2 heading (a level-3 or deeper heading
 * inside the section doesn't end it). Markdown emphasis, links and inline
 * code are stripped so the result reads naturally aloud. Null when the
 * heading is absent, or present with nothing after it.
 */
export function extractSpokenSummary(markdown: string): string | null {
  const lines = markdown.split(/\r?\n/)

  let startIndex = -1
  for (let i = 0; i < lines.length; i++) {
    if (HEADING_RE.test(lines[i].trim())) {
      startIndex = i + 1
      break
    }
  }
  if (startIndex === -1) return null

  let endIndex = lines.length
  for (let i = startIndex; i < lines.length; i++) {
    if (ANY_H2_RE.test(lines[i])) {
      endIndex = i
      break
    }
  }

  const section = lines.slice(startIndex, endIndex).join('\n').trim()
  if (section === '') return null

  return stripMarkdownEmphasis(section)
}

function stripMarkdownEmphasis(text: string): string {
  return text
    // [label](url) -> label
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    // `code` -> code
    .replace(/`([^`]+)`/g, '$1')
    // ***bold italic***, **bold**, *italic*, ___/__/_ variants -> inner text
    .replace(/(\*{1,3}|_{1,3})([^*_]+)\1/g, '$2')
    .trim()
}

const WAKE_PHRASES = ['hey mate', 'hi mate', 'ok mate', 'okay mate']
const PAUSE_WORDS = ['um', 'uh', 'erm', 'ah', 'er']
const LEADING_PUNCTUATION_RE = /^[\s,.;:!?-]+/
const TRAILING_PAUSE_RE = new RegExp(`^\\s*(?:${PAUSE_WORDS.join('|')})\\b[\\s,.;:!?-]*`, 'i')

function stripLeadingPunctuation(text: string): string {
  return text.replace(LEADING_PUNCTUATION_RE, '').trim()
}

/**
 * Recognises a leading wake phrase and returns the remainder of the
 * transcript, trimmed (empty string when the wake phrase stood alone).
 * Null when the transcript doesn't start with one.
 *
 * A bare "mate" (no "hey"/"hi"/"ok"/"okay" in front) only counts when it's
 * followed by punctuation, a pause word, or nothing at all - plain "Mate
 * how's the wind" doesn't trigger, since "mate" is ordinary Australian
 * address and would otherwise fire on nearly every sentence.
 */
export function stripWakeWord(transcript: string): string | null {
  const trimmed = transcript.trim()
  if (trimmed === '') return null

  for (const phrase of WAKE_PHRASES) {
    const re = new RegExp(`^${phrase.replace(' ', '\\s+')}\\b`, 'i')
    const match = re.exec(trimmed)
    if (match) return stripLeadingPunctuation(trimmed.slice(match[0].length))
  }

  const bareMatch = /^mate\b/i.exec(trimmed)
  if (!bareMatch) return null

  const rest = trimmed.slice(bareMatch[0].length)
  if (rest.trim() === '') return ''
  if (/^\s*[,.;:!?-]/.test(rest)) return stripLeadingPunctuation(rest)

  const pauseMatch = TRAILING_PAUSE_RE.exec(rest)
  if (pauseMatch) return rest.slice(pauseMatch[0].length).trim()

  return null
}

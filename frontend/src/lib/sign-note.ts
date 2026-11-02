// How a sign card's note is wrapped. The page's card, the photo moment and the
// video's canvas all wrap the note here, with one measure of the text, so a
// card is the same height wherever it is drawn and its size is exact.

import { FONT_STACK, wrapLines, type Measure } from '@/lib/passage-export-layout'
import { SIGN_CARD_PAD_PX, SIGN_NOTE_MAX_LINES, signCardBox, type ReplaySign } from '@/lib/replay-signs'
import { measureWithCanvas } from '@/lib/text-measure'

/** The note's font on a card. */
export const NOTE_FONT = `400 12px ${FONT_STACK}`

let shared: Measure | null = null
/** The one measure of text the cards use, made when first asked for. Throws in a browser that cannot measure text. */
export function signMeasure(): Measure {
  shared ??= measureWithCanvas()
  return shared
}

/** The lines of a sign's note on its card in a map `containerW` wide: at most two, the last shortened if there is more. */
export function signNoteLines(sign: Pick<ReplaySign, 'note' | 'photo_url'>, containerW: number, measure: Measure): string[] {
  const inner = signCardBox(sign, containerW, 0).w - 2 * SIGN_CARD_PAD_PX
  return wrapLines(sign.note, inner, NOTE_FONT, measure, SIGN_NOTE_MAX_LINES)
}

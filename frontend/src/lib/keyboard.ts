import type { KeyboardEvent } from 'react'

/**
 * True while an IME (Chinese, Japanese, Korean) is composing. Its keys (Enter to
 * pick, Escape to cancel, arrows to move) belong to the IME, not to the field's
 * own shortcuts. Safari sends the confirming keydown with isComposing false and
 * keyCode 229, so check both.
 */
export function isComposing(e: KeyboardEvent | globalThis.KeyboardEvent): boolean {
  const native = 'nativeEvent' in e ? e.nativeEvent : e
  return native.isComposing || e.keyCode === 229
}

/** True for an Enter that means "submit", not an Enter that picks an IME candidate. */
export function isSubmitEnter(e: KeyboardEvent): boolean {
  return e.key === 'Enter' && !isComposing(e)
}

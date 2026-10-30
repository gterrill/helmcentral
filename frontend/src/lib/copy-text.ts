/** Copies the text a visible field or block shows to the clipboard.
 * Tries the clipboard API, which browsers only offer on secure pages, then
 * `execCommand('copy')` on the selection, which also works over plain http.
 * Resolves false when both fail; the text stays selected so the reader can
 * copy it by hand. */
export async function copyTextFrom(source: HTMLElement | null): Promise<boolean> {
  if (!source) return false
  const text = isField(source) ? source.value : (source.textContent ?? '')
  if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // Permission refused: try the selection route below.
    }
  }
  select(source)
  try {
    return typeof document.execCommand === 'function' && document.execCommand('copy')
  } catch {
    return false
  }
}

function isField(source: HTMLElement): source is HTMLInputElement | HTMLTextAreaElement {
  return source instanceof HTMLInputElement || source instanceof HTMLTextAreaElement
}

function select(source: HTMLElement) {
  if (isField(source)) {
    source.focus()
    source.select()
    return
  }
  // A focused field copies its own selection, not the document's.
  if (document.activeElement instanceof HTMLElement && isField(document.activeElement)) document.activeElement.blur()
  const selection = window.getSelection()
  if (!selection) return
  const range = document.createRange()
  range.selectNodeContents(source)
  selection.removeAllRanges()
  selection.addRange(range)
}

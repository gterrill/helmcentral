/** Copies text to the clipboard from a selected, visible textarea. Tries the
 * clipboard API, which browsers only offer on secure pages, then
 * `execCommand('copy')` on the selection, which also works over plain http.
 * Resolves false when both fail; the text stays selected so the reader can
 * copy it by hand. */
export async function copyTextFrom(textarea: HTMLTextAreaElement, text: string): Promise<boolean> {
  textarea.focus()
  textarea.select()
  if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // Permission refused: try the selection route below.
    }
  }
  textarea.focus()
  textarea.select()
  try {
    return typeof document.execCommand === 'function' && document.execCommand('copy')
  } catch {
    return false
  }
}

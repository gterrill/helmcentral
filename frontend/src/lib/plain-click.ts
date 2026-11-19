/**
 * A click that means "go there in this app": the primary button with no
 * modifier. Anything else (a new-tab click, a context click) is left to the
 * browser, which has the address in the link's href.
 */
export function isPlainLeftClick(event: Pick<MouseEvent, 'button' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey'>): boolean {
  return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey
}

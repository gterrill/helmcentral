import { expect } from 'vitest'

/** An identifier box: no capital first letter, no autocorrect, no spell-check underline. */
export function expectIdentifierInput(el: HTMLElement | null, label?: string): void {
  const target = label ? expect(el, label) : expect(el)
  target.toHaveAttribute('spellcheck', 'false')
  target.toHaveAttribute('autocapitalize', 'none')
  target.toHaveAttribute('autocorrect', 'off')
}

/** A third-party secret box: the browser must not offer the saved sign-in. */
export function expectSecretInput(el: HTMLElement | null, label?: string): void {
  const target = label ? expect(el, label) : expect(el)
  target.toHaveAttribute('autocomplete', 'new-password')
  target.toHaveAttribute('spellcheck', 'false')
}

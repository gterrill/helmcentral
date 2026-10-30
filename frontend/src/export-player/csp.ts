import { errorMessage } from '@/lib/errors'
// A failed request on a blog that sets a content security policy looks like any
// other network failure. One console line says which directive to check.

/** What a failed fetch most likely means on a blog that sets a content security
 * policy, or null when it looks like something else. */
export function cspHint(err: unknown, directive: string): string | null {
  const name = err instanceof Error ? err.name : ''
  const text = errorMessage(err)
  if (name === 'SecurityError' || /content security policy|csp|refused to/i.test(text) || (name === 'TypeError' && /failed to fetch|load failed|networkerror/i.test(text))) {
    return `If this blog sets a Content-Security-Policy, it may be blocking this request: allow ${directive}.`
  }
  return null
}

const WARNED_KEY = '__hcpCspWarned'

/** Logs the hint once per page and directive, however many requests fail. */
export function warnCsp(err: unknown, directive: string): void {
  const hint = cspHint(err, directive)
  if (!hint) return
  const page = globalThis as { [WARNED_KEY]?: Set<string> }
  const warned = (page[WARNED_KEY] ??= new Set())
  if (warned.has(directive)) return
  warned.add(directive)
  console.warn(hint)
}

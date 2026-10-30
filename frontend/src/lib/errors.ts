/** The text of whatever a catch block received: an Error's message, otherwise the value as a string. */
export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

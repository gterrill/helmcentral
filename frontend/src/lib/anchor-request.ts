/** Preserve explicit backend partial-failure messages. */
export async function anchorRequest(init: RequestInit): Promise<Response> {
  const response = await fetch('/api/anchor-watch', init)
  if (!response.ok) {
    let message = `HTTP ${response.status}`
    try {
      const body = await response.json() as { error?: unknown }
      if (typeof body.error === 'string') message = body.error
    } catch {
      // Non-JSON errors still surface their HTTP failure status.
    }
    throw new Error(message)
  }
  return response
}
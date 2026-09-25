/**
 * Thrown by anchorRequest on a non-OK response, carrying the HTTP status
 * alongside the message so a caller can tell a validation/not-found refusal
 * (400/404 — retrying with the same input can never succeed) from a
 * transient failure (network error, 5xx — worth a Retry action). A plain
 * network error (fetch() itself rejecting, no response at all) is not
 * wrapped in this type; see isRetryableAnchorError below for how the two
 * are told apart.
 */
export class AnchorRequestError extends Error {
  status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'AnchorRequestError'
    this.status = status
  }
}

/**
 * Whether a failure from anchorRequest is worth offering a Retry action for.
 * A network error (not an AnchorRequestError — fetch rejected before a
 * response existed) and a 5xx are both transient, the same condition might
 * not be there on a second try. A 4xx (bad radius, no active anchor watch,
 * validation) will fail again with the exact same input, so Retry would just
 * repeat a doomed request.
 */
export function isRetryableAnchorError(error: unknown): boolean {
  return !(error instanceof AnchorRequestError) || error.status >= 500
}

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
    throw new AnchorRequestError(message, response.status)
  }
  return response
}
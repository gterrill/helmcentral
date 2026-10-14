import { ApiError } from '@/lib/api-query'

/**
 * What the anchor-watch writes throw on a non-OK response: an ApiError,
 * carrying the HTTP status alongside the server's own message so a caller can
 * tell a validation/not-found refusal (400/404, retrying with the same input
 * can never succeed) from a transient failure (network error, 5xx, worth a
 * Retry action). A plain network error (fetch() itself rejecting, no response
 * at all) is not an ApiError; see isRetryableAnchorError below.
 */
export const AnchorRequestError = ApiError
export type AnchorRequestError = ApiError

/**
 * Whether a failure from an anchor-watch write is worth offering a Retry
 * action for. A network error (not an ApiError, fetch rejected before a
 * response existed) and a 5xx are both transient, the same condition might
 * not be there on a second try. A 4xx (bad radius, no active anchor watch,
 * validation) will fail again with the exact same input, so Retry would just
 * repeat a doomed request.
 */
export function isRetryableAnchorError(error: unknown): boolean {
  return !(error instanceof ApiError) || error.status >= 500
}

// Shared by every hook that PATCHes/POSTs/DELETEs a REST resource and wants a
// server-supplied reason in its failure toast (use-dashboard-pages.ts,
// use-displays.ts). Split out once a second hook needed the exact same
// parsing rather than a second, driftable copy of it.

// Reads the server's `{"error": "<message>"}` body off a failed response for
// use in a toast description. Parsing must never throw: a non-JSON or empty
// body (e.g. a 500 from a proxy/load balancer) falls back to `HTTP <status>`.
export async function readErrorMessage(res: Response): Promise<string> {
  try {
    const data = (await res.json()) as { error?: string }
    if (data && typeof data.error === 'string' && data.error.length > 0) {
      return data.error
    }
  } catch {
    // body missing or not JSON — fall through to the status-based message
  }
  return `HTTP ${res.status}`
}

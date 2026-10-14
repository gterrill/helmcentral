# ADR 0173: One query module under the resource hooks

## Status

Accepted (2026-10-14). Builds on the response-patching rule of
[ADR 0013](0013-multi-page-dashboard.md). Every request/response hook moved over in
the same change.

## Context

About 66 hooks each hand-roll the same fetch state: `items`, `loading`, `error`, a refresh
function and an effect. They guard against a late response in three different ways: a
`seqRef` counter in 9 files, a `cancelled` flag in about 27, an `AbortController` in 6.
`submitJSON` exists in 7 copies, one of which grew field-error handling the others lack.
46 files call relative `/api` URLs while 50 go through `apiBaseUrl`, so the build-time base
URL works for some features and not others. There is no shared test helper: each test file
defines its own `jsonResponse` or `stubFetch`.

Leaving a page and coming back also shows a spinner every time, because each hook keeps its
state in the component and loses it on unmount.

## Decision

1. **Our own module, `frontend/src/lib/api-query.ts`, with no provider.** The store is
   module level. Names follow TanStack Query (`useApiQuery`, `setQueryData`,
   `invalidateQueries`, `refetchInterval`, `enabled`, `select`) so a later swap is
   mechanical.
2. **`apiRequest` and `ApiError`.** `apiRequest` prefixes `apiBaseUrl`, sends `json` as a
   body, returns `undefined` for 204 and the parsed JSON otherwise, and throws `ApiError`
   for a non-2xx response. The error carries the status, the server's own message,
   `fieldErrors` when the body names fields, and the parsed `body` for the few callers that
   read more (a 409's `affected` list, a proposal's `proposal_status`). This replaces `readErrorMessage` and the copies of `submitJSON`.
   A network failure is not wrapped.
3. **A keyed store.** Entries are keyed by a stable hash of the query key. A request is
   shared by every reader of the key, and only the response to the latest request lands, so
   the three guard idioms become one. An entry is dropped five minutes after its last reader
   leaves. While any reader asks for polling, the entry polls at the smallest interval asked. A
   reader whose interval changes (the anchor watch going idle) retunes the poll in place
   rather than counting as a fresh mount.
4. **Stale while revalidate.** Returning to a page shows the last data at once and refetches
   behind it, so there is no spinner. The first mount for a key shows loading. React
   StrictMode's double mount shares one request. A key change switches entries and does not
   carry the previous data over. After a failed refetch the error is set and the last good
   data stays beside it until the next success.
5. **The per-resource hooks keep their return shapes.** Components and their `vi.mock`
   calls do not change. Only the body of each hook moves onto `useApiQuery` and
   `apiRequest`.
6. **Writes invalidate by default.** A create, update or delete calls `invalidateQueries`
   with the resource's key prefix. Where a hook already patched local state from the
   response (ADR 0013) it uses `setQueryData` instead and skips the extra read.
7. **One test helper.** `stubApi` takes routes keyed `GET /api/inventory/zones`, installs the
   fetch stub, and throws on a request no route matches. The store is cleared after every
   test.

## Consequences

- A hook shrinks to a key, a path and the writes that invalidate it.
- Every request goes through `apiBaseUrl`, which fixes the 46 relative callers as they move.
- A cached read can be up to a refetch old when a page opens. The background refetch
  corrects it within one round trip.
- The store is global state. Tests rely on the clear in `setup.ts`.

## What the migration found

- **Not every request is a cached read.** These went to `apiRequest` alone and keep their
  own state: server trails (they accumulate from a `since` cursor), POI (backs off on 502
  and 503), the GSHHG coastline (a multi-MB payload the five-minute eviction would
  re-download), the settings form and provider drafts (a local edit seeded from the
  server), checklist runs (a POST on mount), the Mate telemetry watch, the Mate thread's
  messages (streaming appends to them), and the version-reload probe.
- **Two places already want `keepPreviousData`.** The model catalog keeps the page it is
  leaving on screen until the next lands, and the Documents search keeps its results
  while the next keystroke loads. Both do it locally. A third is the signal to switch.
- **Sibling refreshes are gone.** The deck editor, the Documents panel's tree refresh, the
  maintenance callbacks, the bin page and the display-delete page refetch in `App.tsx` now
  rely on the writing hook invalidating the shared key.

## Rejected alternatives

- **TanStack Query now.** It needs a `QueryClientProvider` in every test render across 364
  files, and adds about 12 KB gzip to the eager chunk the wall browser has to parse.
- **`apiRequest` alone.** It fails the deletion test: the duplication is the state machine
  (loading, error, the late-response guard), not the fetch call. Removing the helper would
  leave all 66 hooks as they are.

## When to switch to TanStack Query

Move when an index page needs server-side paging or sorting, keeps the previous rows while a
filter changes (`keepPreviousData`), or needs retries. The names above are chosen so the
change is a rewrite of the module's imports, not of the hooks.

## Out of scope

SSE and WebSocket streams, the `use-auth` and `use-app-config` stores, and file downloads.

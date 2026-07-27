# ADR 0033: Removing Storm Glass — Tides Become Plugin-Only

## Status
Accepted

Amends ADR 0017, which introduced the WASM tide-plugin registry alongside two native providers; with the last native provider gone, its "natives register first, so a plugin can never shadow them" rule describes a situation that can no longer arise.

Resolves the item ADR 0018 deferred under Consequences — surfacing per-plugin caches in the caches-admin UI — by deleting that UI rather than extending it.

Also removes the last consumer of the encrypted `STORMGLASS_API_KEY` secret introduced in ADR 0023, and the last non-sandboxed provider that ADR 0024's `sandboxed` flag existed to distinguish.

## Context

Storm Glass was the only tide provider compiled into the Go binary. Everything else — BOM, NOAA, and every weather, wave, and forecast-warnings source — had already become a sandboxed WASM plugin under ADRs 0017 and 0018. It was registered unconditionally at startup whether or not an operator had a key for it.

It was also load-bearing in three ways that had nothing to do with tides, which is what made removing it a larger change than deleting one file:

- **It owned the JSON tide cache.** `tideCacheStore`, its hit/miss counters, and the `loadTideCacheFromDisk`/`persistTideCacheToDisk` pair lived inside `tide_provider_stormglass.go`, but were read by `/api/caches`. WASM tide plugins never used them — they use the generic `wasmPluginCache[T]` from `wasm_plugin.go`.
- **It was the last entry in `jsonCacheDescriptors`.** Weather and waves had already migrated to per-plugin caches. The `"tide"` descriptor — whose TTL was literally `stormGlassTideCacheTTL` — was the only thing left for the `/admin` page's Cache Control panel to report on.
- **It was the last non-sandboxed provider anywhere**, in any of the four domains.

The reason to remove it is that it was never really a *default*. It requires a paid API key, so on a fresh install it registers, appears in the settings dropdown, and then fails at fetch time with "Storm Glass API key not configured". Commit 5cf27d8 had already had to stop two separate code paths from silently forcing `ui.tide_provider` to `"stormglass"`, precisely because presenting a keyed paid service as a fallback produced confusing failures. A provider that cannot work out of the box is not a default; it is a plugin that happens to be compiled in.

## Decision

1. **Delete the provider outright; do not port it to a plugin.** The port path is real — BOM was itself a native provider ported to WASM — and is left open rather than closed off. It was not taken now because it is not a mechanical port: the plugin contract's `fetch_tide_chart` receives only a `station_id`, and Storm Glass is not station-based at all. It queries the vessel's live position, which is why it had a `"vessel-position"` pseudo-station that the host resolved by calling SignalK.

   **Recorded so a future port does not rediscover it:** this fits the existing contract without changing it, by having the host resolve the position and pass `"<lat>,<lon>"` as the `station_id` for a plugin that declares itself position-based. `tideNearestHandler` already resolves vessel position host-side. No new host function or contract revision is required.

2. **Tides are plugin-only, and an empty registry is a legitimate state.** This is what weather, waves, and forecast warnings already do. With no plugins installed, `/api/tide-providers` returns an empty list and `tideToday` returns 502 naming what is missing. Nothing is assumed on the operator's behalf.

   ADR 0017's ordering guarantee ("because natives register first, a WASM plugin can never shadow `bom` or `stormglass`") is now vacuous — there are no natives to register first. Plugin ids are only in contention with each other, resolved by the same first-registered-wins rule.

3. **Delete the JSON-cache subsystem and the `/admin` page.** `jsonCacheDescriptors`, `listCaches`, `invalidateCache`, both `/api/caches` routes, the disk cache helpers, and `caches-page.tsx` all go.

   ADR 0018 deferred wiring per-plugin caches into this UI as "a nice-to-have, not core to pluggability". Rather than finally doing that work for a page whose last real row had just disappeared, the page is deleted. The `/admin` route's only other content was a "System" tile reading *"Additional admin tools can be added here."*

   `cacheFilePath` and `writeJSONFileAtomic` are **kept** — six other subsystems use them (anchor watch, secrets, nearby contacts, plugin overrides, routes, the WASM plugin cache).

4. **Fix the latent bugs the removal exposed, rather than deleting only the Storm Glass half of each.** Four places branched on one provider id as a proxy for "the other case". While Storm Glass existed, `tideProvider === 'bom'` happened to mean "station-based provider"; NOAA had already made that false, and removing Storm Glass would have left it silently wrong:

   | Site | Was | Now |
   | --- | --- | --- |
   | "Change Station" button | rendered only for `bom` | every provider |
   | Station-picker empty state | `bom` only | every provider |
   | "Auto-update station as vessel moves" | `bom` only | every provider |
   | "Data:" attribution line | `bom ? 'BOM' : 'Storm Glass'` | real name from `/api/tide-providers` |

   The attribution line falls back to the raw provider id when no match is found, rather than to a hardcoded display name — a provider that is configured but not installed should read as unfamiliar, not be relabelled as something else.

   `useTideSettings` also defaulted `tideProvider` to `'stormglass'`. Left alone that would have pointed at a provider that no longer exists; it is now empty, and consumers already gate on `loading`.

5. **Replace speculative native-provider support with an explicit invariant.** With no native providers left, `provider.(pluginPathProvider)` cannot fail. That branch previously returned a quietly-degraded success response, justified in comments as "generic infrastructure kept for any future native provider" — speculative code masking an impossible state, which the Fallback Policy prohibits on both counts. It now surfaces the violation explicitly.

   The `sandboxed` field stays in `pluginInfoResponse`. That struct is annotated as a fixed HTTP contract with an external consumer; the field is now invariantly true, which is a fact about the deployment rather than a reason to break the shape.

## Consequences

- **Anyone outside Australia and the United States now has no tide data at all** until they install or write a plugin. This is the real cost, accepted deliberately. It is less of a regression than it appears — the alternative was a provider that also did nothing without a paid key — but it is a genuine narrowing, and Decision 1 exists so the way back is written down rather than remembered.
- Tides now match every other provider domain exactly. There is one plugin mechanism, one sandbox story, one place a provider can come from.
- The app ships with no compiled-in third-party API dependency for tides, and no key to leak or bill.
- **Per-plugin cache state is now unobservable.** Plugins still cache to disk with TTLs; there is simply no UI or endpoint reporting hits, misses, sizes, or ages, and no way to invalidate one short of deleting its file. This was already true for weather and waves since ADR 0018; it is now true for tides too. If cache observability is wanted, it should be rebuilt against `wasmPluginCache` for all four domains at once, not restored for one.
- `sandboxed` is now always true, so the settings modal always renders the allowlist editor. The flag is retained for contract stability, not because it varies.
- **Four display bugs existed before this change and would have survived it** had the removal only deleted the Storm Glass branches. Using one provider id as a stand-in for "not that other provider" is the pattern to watch: it stays correct exactly as long as there are two providers.

## Related
- ADR 0017: Sandboxed WASM Plugin Tide Providers (amended — native-shadowing rule now vacuous)
- ADR 0018: WASM Plugin Weather And Wave Providers (its deferred caches-UI item resolved by deletion)
- ADR 0023: Encrypted Secrets Store (`STORMGLASS_API_KEY` no longer registered)
- ADR 0024: Plugin Descriptions And DB-Backed Allowlist Overrides (`sandboxed` now invariant)

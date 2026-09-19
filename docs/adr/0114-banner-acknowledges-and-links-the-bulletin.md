# ADR 0114: The Banner Acknowledges Alarms and Links the Forecast Bulletin

## Status
Accepted

Extends ADR 0038 (SignalK notification vocabulary, the acknowledge/silence split), ADR 0082 (the alarm banner's worst-first triage) and ADR 0087 (forecast warnings are alarms, the two derived paths and the "Details on the Forecast page" sentence this ADR partly rewords).

## Context

The alarm banner (ADR 0082) sits above every dashboard page and already says what is wrong, worst first. It has one button, View, which opens the Alarms drawer, and the drawer is where Acknowledge and Silence actually live, one pair of buttons per card. Acknowledging is a single stateless call to the server (ADR 0038's `POST .../acknowledge`) — nothing about it needs the drawer's rule list, its history tile or a second card repeating the sentence the banner already shows. The drawer held the only Acknowledge button because that is where the buttons happened to get built first, not because acknowledging needs anything the banner doesn't already have.

A screen mounted at the helm with no keyboard nearby, or a phone glanced at from the cockpit, feels that gap directly. The banner already says a gale warning is in force; silencing the sound means leaving whatever page is up, finding the Alarms item, and tapping Acknowledge on a card that repeats exactly what the banner just said. For an alarm that clears itself once the reading recovers, that round trip buys nothing except delay, and a delay is exactly what an alarm sound should not have.

Separately, the two forecast-warning sentences ADR 0087 wrote ("Gale warning in force. Details on the Forecast page.") point at a page rather than at anything. The Forecast page already resolves the actual bulletin — `findActiveWindBulletin`/`findActiveSurfBulletin` in `use-forecast-warnings.ts` — and links to it. The alarm card and the banner, which already render that same sentence, can resolve and show the same link instead of sending the operator hunting for it on another panel.

## Decision

### 1. An Acknowledge button, next to View

`AlarmBanner` takes a new `onAcknowledge: (ruleId: string) => Promise<void>` prop — the same signature the drawer already takes, wired in `App.tsx` to the one `acknowledgeAlarm` from `useAlarms()` both now share. The button renders only when at least one alarm the banner is currently showing has `can_acknowledge === true` and `phase !== 'acknowledged'`; in the muted "all acknowledged, still live" variant every shown alarm already fails that test, so no button appears there; there is nothing left to acknowledge.

Clicking it acknowledges every qualifying shown alarm in sequence, one `await` at a time rather than in parallel. Sequential and fail-fast matters here in a way it wouldn't for an operation with no failure mode: a partial failure has to leave the operator able to reason about which alarms went through and which didn't, and a `Promise.all` racing four acknowledgements would report one combined failure with no way to tell which of the four actually landed. The button disables for the duration and a rejection surfaces its message on the banner itself rather than nowhere (the previous behaviour: `onAcknowledge` had no caller on this component at all). The label is "Acknowledge" for one qualifying alarm and "Acknowledge all" for more than one, matching the banner's own existing habit of naming a count rather than hiding it.

The loop is sequential, so it awaits between calls, and the alarm set can move underneath it: an alarm cleared by the server mid-loop answers 409 to an acknowledge (`errNotificationNotLive`), and fail-fast would then abandon the alarms that are still sounding over one that has already stopped. So each iteration re-reads the live set through a ref before acting, and skips an alarm that is no longer live or no longer acknowledgeable. That is not a swallowed error: a refusal for an alarm that *is* still live stops the loop and surfaces exactly as described above. For the same reason the surfaced message is dropped once the alarm set itself changes — a refusal describes the set it was raised against, and a stale one sitting under a live alarm reads as a refusal of that alarm.

### 2. Acknowledge, not silence

Only Acknowledge is offered here, not Silence. `can_silence` and `can_acknowledge` are independent server-reported capabilities (ADR 0038 §1) and a card in the drawer can offer either, both, or neither. Acknowledging is the stronger, more final action of the two — it stops the sound and the visual alert and moves the alarm out of the active phase — and it is also the one every alarm that can be actioned at all from the board is most likely to offer, since silence is comparatively rare in this codebase's rule set. Adding a second button here would mean deciding, for every alarm shape, which of two independently-true booleans wins the banner's one remaining slot next to View; Acknowledge already covers the case that matters (stop the noise) and View is always available for anything short of that, including reaching Silence on a card that offers it and not Acknowledge.

### 3. The bulletin link, joined client-side

`forecastWarningDetailsUrl(warnings, path)`, new in `use-forecast-warnings.ts`, maps an alarm's path to a category with the same two lookups `ForecastWarningNotice` already uses (`FORECAST_WIND_WARNING_PATH` → `findActiveWindBulletin`, `FORECAST_SURF_WARNING_PATH` → `findActiveSurfBulletin`) and returns that bulletin's `detailsUrl`, or null for any other path, for no active bulletin, or for an empty URL. `alarmConditionSentence` takes an optional `{ forecastDetailsLinked?: boolean }` second argument; when true, the two forecast sentences drop their trailing " Details on the Forecast page." clause, since a caller passing `true` is about to render a real link in its place. Every existing call site keeps calling the function with no second argument at all, and its behaviour is byte-for-byte unchanged.

The banner (for the one condition sentence it ever renders — the worst shown alarm's) and every forecast card in the drawer resolve this the same way: join the alarm's own `path` against the `ForecastWarnings` payload `App` already holds from `useForecastWarnings()`, and when the join succeeds, render "View details →" right after the sentence, opening in a new tab.

On the banner the sentence and the link are siblings in a flex row rather than one run of text: the banner's condition line is `truncate`, which is `white-space: nowrap` plus `overflow: hidden`, and a link inside that clipped line is unreachable at phone or kiosk width — there is no horizontal scroll to bring it back. The sentence clips; the link does not.

The alarm itself carries no URL. It carries a path and a small ranked value (ADR 0087), the same two-line contract every other rule alarm has, and a fifth field added only for two of the many alarm shapes would be exactly the kind of special-casing ADR 0087 already avoided by keeping the sentence itself in `alarm-display.ts` rather than on the wire. The join happens wherever the sentence is rendered instead, using data the caller already has in scope.

## What was rejected

**Carrying `detailsUrl` on the `ActiveAlarm` payload.** The backend would have to duplicate the same `findActiveWindBulletin`/`findActiveSurfBulletin` lookup it already does for the Forecast page's own API response, on every alarm tick, for a value two of many alarm shapes would ever use. The frontend already holds the forecast payload in the same component tree that renders the alarm; joining them there is one function call, not a second source of truth for a URL that can change out from under a cached alarm between fetches.

**A confirmation dialog before acknowledging from the banner.** The drawer's own Acknowledge button has never asked for confirmation, on the same reasoning ADR 0038 already settled: acknowledging doesn't clear the alarm, it stops the sound and the visual alert, and the alarm stays on the board exactly as before until the underlying value recovers. A dialog here would be a new, inconsistent bar for the same action depending on which button was tapped.

**A Silence button on the banner.** See decision 2. Rejected for now rather than ruled out permanently — if a rule set ever leans on `can_silence` the way the built-ins lean on `can_acknowledge`, the banner has room for a second button in the same `flex gap-2` group this ADR adds.

## Consequences

- Acknowledging a live alarm no longer requires opening the Alarms drawer from any page, including one with no drawer button in easy reach.
- A rejected acknowledgement is visible where the operator acted, not silently dropped and not requiring a trip to the drawer to discover it failed.
- The forecast sentence on both the banner and the drawer card now reads "Gale warning in force." with its own link immediately after, rather than pointing at the Forecast page as a separate step. The Forecast page's own notice (`forecast-warning-notice.tsx`) is unchanged; it already carried its own link.
- `alarmConditionSentence`'s signature grew an optional second argument. Every caller that predates this ADR is untouched.

## Verification

Test-first: `alarm-banner.test.tsx`, `alarms-drawer-forecast-link.test.tsx`, `use-forecast-warnings.test.ts` and `alarm-display.test.ts` were extended with the new cases below before `onAcknowledge`, `forecastWarnings` or `forecastWarningDetailsUrl` existed, confirmed failing, then made to pass:

- the Acknowledge button appears only when a shown alarm qualifies, calls `onAcknowledge` with its rule id, labels itself "Acknowledge all" for more than one qualifying alarm, acknowledges them in order, disables itself while in flight, and stops at the first rejection, surfacing the message and leaving the rest un-acknowledged;
- an alarm the server clears while the loop is in flight is skipped rather than acknowledged, and the alarms after it are still acknowledged;
- a surfaced refusal is dropped once the alarm it failed on is no longer on the board;
- no button renders in the muted "all acknowledged, still live" variant;
- `forecastWarningDetailsUrl` is covered for the wind path, the surf path, an unrelated path, no active bulletin, and an empty `detailsUrl`;
- `alarmConditionSentence`'s `forecastDetailsLinked` option is covered for both forecast sentences and confirmed to have no effect on a non-forecast sentence, alongside the untouched pre-existing default-behaviour assertions;
- the banner and the drawer card both render the "View details →" link with the bulletin's href, `target="_blank"` and `rel="noopener noreferrer"` when a matching active bulletin exists, and keep the old sentence with no link when it doesn't.

Full suite: 248 test files, 2961 tests pass. `npm run lint` reports zero errors (pre-existing warnings only, none touching these files). `tsc --noEmit` is clean.

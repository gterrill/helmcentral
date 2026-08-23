# ADR 0053: Engine Profiles as Drop-In JSON

## Status
Accepted

Supersedes ADR 0043 (Service-Schedule Provider Plugins), which was Proposed and never implemented. Extends ADR 0049 (grouped gauge tiles) and ADR 0050 (gauge zones as the alarm source).

## Context

Building the Port and Starboard engine tiles worked, but every number in them was typed by hand: paths, scales, and — since ADR 0050 — the thresholds that fire alarms. For a Cummins QSB 6.7 550 that is five gauges twice over, from a manual that is not open at the time.

The same file that knows an engine's gauges also knows its service intervals, because both answer one question: which engine do you have.

## Decision

### 1. JSON, not a WASM plugin

Every other `plugins/` directory holds `.wasm`. This one holds `.json`, and the reason is specific rather than convenient.

The existing categories (ADR 0017/0018/0019) fetch data over a network, and the sandbox exists to run *untrusted code* safely. A profile runs no code and fetches nothing — it is a table of numbers. And since ADR 0050 those numbers are **alarm thresholds**.

That inverts the usual tradeoff. For a network provider an opaque binary is fine, because the sandbox is the protection. For safety thresholds the requirement is the opposite one: the operator must be able to open the file and read what their oil-pressure alarm will fire at. WASM would protect against the wrong thing while removing the property that actually matters. It also drops the toolchain barrier ADR 0017 named as WASM's real cost, so a profile someone has worked out is shareable as a text file.

If a profile ever needs to *compute* or *fetch* — look a spec up by engine serial, say — a WASM provider can sit behind the same `GET /api/engine-profiles`. Nothing here forecloses it.

### 2. Superseding ADR 0043

ADR 0043 put service schedules in a WASM plugin category with `search_models` and `fetch_schedule`. Its scope rested on "records are planned to live in a separate application" — HelmLocker. HelmLocker is inventory management, not maintenance, so that premise does not hold, and Helmcentral will own maintenance and service history.

0043 rejected a data format because "real schedules need computation, not lookup". Its own decision 3 undercuts that: the plugin returns raw intervals and *the host derives everything*. "250 hours or 12 months, whichever comes first" is two fields plus host-side logic. "Conditional on engine variant" collapses into the profile id — `cummins-qsb67-550` and `-480` are separate files. "Items that supersede one another" is a `supersedes` list. 0043 also expected schedule plugins to ship no `allowed_hosts.json` and run entirely offline; a plugin category that uses none of the plugin system is a data file wearing a sandbox. 0043 explicitly invited this revisit, and it is warranted.

**0043's strongest objection dissolves outright.** It argued a service reminder cannot be an alarm: "runtime is monotonic, so such a rule fires once at the threshold and can never clear." That was true when written. ADR 0050 has since established rules *derived* from config the backend owns, re-evaluated every tick. A service-due rule is `runTime above (last_completed_hours + interval)` — logging work **moves the threshold**. The value never comes back down; the bar goes up. It clears and re-arms through the existing engine with no new state machine and no change to `alarmRule`.

What 0043 got right and is kept: the host derives what is due, there is no default provider, and a completion record stops at the fact that work happened.

### 3. Advisory bands and alarm thresholds are different things

Cummins does not publish QSB 6.7 setpoints; they are in the operator's manual and QuickServe. What *is* publicly sourceable, from Seaboard Marine, is **normal operating guidance**: coolant "in the 160F to 185F range", oil pressure "40-80 PSI at medium to high RPM's" and "10-20 PSI when idling (engine hot)".

Those are not alarm points, and the distinction is critical now that a zone fires an alarm. A profile that alarmed at 25 psi because a forum says cruise pressure is 40–80 would raise alarms the engine's own ECU does not, and would teach the operator to ignore them. **That is a worse outcome than shipping no profile at all.**

The format separates the two, and the existing code already supported it exactly: `normal`-state zones colour the gauge and are explicitly skipped by `zoneDerivedAlarmRules()` and by `validateGaugeConfig`'s threshold check. A shipped profile is all-`normal` advisory bands, each citing its source, with `warn` and `alarm` slots present but **null** until the owner fills them from the manual.

`TestBundledProfilesShipNoAlarmThresholds` asserts this against the shipped file directly: no bundled zone may be non-`normal` with a non-null threshold, and every advisory band must cite a source. A later edit cannot quietly add a plausible-looking alarm point.

### 4. Slots are a first-class idea, in two places

A `null` threshold is not missing data. It is a threshold the manufacturer defines that the profile does not know, and it survives loading so the apply dialog can render "not set — from your manual". It is dropped before anything is saved, so it can never become a zone at some default number nobody chose.

The same idea applies to service items: an item with both intervals null names a service the engine has without inventing how often it wants it. The bundled Cummins profile lists oil, fuel filter, coolant, impeller, air filter and zincs this way. That is the checklist, and it tells the operator what to go and look up — strictly more useful than omitting the block.

### 5. Zones are direction plus threshold, matching the editor

Stored zones are `{from, to}`, but a profile authors them as a direction and a threshold, anchored against the gauge's own scale at apply time. This is the ADR 0050 editor model, and it means a profile *cannot express* a mid-range band — the same thing the editor cannot express and the backend rejects. The constraint is enforced by the shape rather than by a check.

### 6. Applying to an existing tile appends, it does not only update

Applying a profile to a tile that already exists fills in the settings of members
already there *and* appends the ones the profile has that the tile lacks. The first
version only updated, and a half-built Port tile stayed half-built — the gauges you had
not thought to add were exactly the ones still missing, which is the opposite of what a
profile is for.

The instance prefix for those appended gauges comes from **the tile's own gauges**, not
from the first path the server happens to publish. Seeding from the published list would
append starboard gauges to the Port tile whenever starboard sorted first. The published
list remains the fallback for a tile with no paths yet.

The dialog reports what applying will do — "1 gauge updated, 1 added" — before it does it.

### 7. Failure is per-file and reported

A malformed profile is skipped, logged, and returned in `problems` alongside the profiles that loaded. One bad drop-in file must not take the server down, and must not vanish silently either — the apply dialog shows the failures. Validation reuses `validGaugeDisplays`, `convertToSI` (which rejects an unknown quantity or unit, the same guard ADR 0050 rests on) and `alarmStateRank` rather than restating them, so a profile can never describe a gauge the dashboard would reject.

There is deliberately **no apply endpoint**. Applying produces an ordinary `PATCH /api/dashboard-pages/:id` that the existing validator already checks. A profile is a source of values, not a privileged path into the page store.

## Consequences

- An engine tile is two clicks: pick the profile, pick the instance. The instance prefix is what makes one profile serve both engines, which is the same problem ADR 0049's find/replace solves, moved to before the tile exists.
- Nothing bundled can raise a false alarm, and a test enforces it. The cost is that the bundled profile's alarm slots are empty on delivery and the operator must fill them once from the manual.
- The format generalises beyond engines. `path_suffix` plus an instance prefix works the same for `tanks.fuel.0` or a genset.
- `normal` zones needed two small fixes to be usable at all: the severity select had no `normal` option, so a profile's bands would have landed un-editable, and `zoneColorFor` rendered them in the border colour, which made an advisory band invisible. They are now green, completing the colour language the ADR 0052 lamps already speak.
- Applying is additive but never subtractive: a gauge the tile has and the profile does not is left alone. Removing it is a deliberate act, not a side effect of applying a profile.
- Service intervals are validated and served but nothing consumes them yet. The maintenance store, completion logging and `serviceDerivedAlarmRules()` are the next build, on a format already proven.
- Profiles go stale silently if a manufacturer revises a schedule — the same concern ADR 0043 raised, unaddressed here for the same reason: there is no evidence yet that it matters, and a freshness signal is easy to add later.

## Verification

`go test -short ./...` and 764 frontend tests pass. Backend tests cover a clean load, per-file rejection of unknown units, displays, zone states, directions and inverted ranges while other profiles still load, duplicate ids, a missing directory, service slots, and the bundled-profile safety assertion. Frontend tests cover path composition, both anchoring directions, slot dropping, the alarm count, suffix merging, and the dialog's preview and safety gate.

Not yet exercised against a live vessel. The check still owed: apply the bundled profile to `propulsion.port`, confirm the preview reads **0 alarm thresholds**, and confirm the alarms drawer gains no derived rules afterwards.

# ADR 0058: The Collision Profile Follows Vessel State

## Status
Accepted

Extends ADR 0057 (AIS collision alarms from target contexts).

## Context

ADR 0057 landed the alarms and the first night at anchor produced three of them: MINNOW, TARA IV and MY TERMS all in `alarm`, none of them moving. That is the failure mode the ADR warned about in its own consequences, arriving faster than expected.

It is not a threshold that needs nudging. It is the wrong profile, on top of a calculation that goes unstable in exactly this situation.

### The profile is wrong

The plugin ships `current: "offshore"` and nothing has ever changed it. Offshore's danger tier is CPA 2 NM, TCPA 15 minutes, speed 0, and that last field is the problem. From `calcAlarms`:

```js
(profile.danger.speed === 0 || isValidNumber(sog) && sog > profile.danger.speed / KNOTS_PER_M_PER_S)
```

`speed: 0` does not mean "no minimum". The `=== 0` short-circuits the entire clause to true, so the speed filter is switched off and a boat sitting motionless on its anchor is eligible for a collision alarm. MY TERMS was at 0.0 knots with a CPA of 0.56 NM and a TCPA of 6.2 minutes, which satisfies all three conditions.

### The calculation is degenerate at anchor

Both alarming targets reported **CPA exactly equal to current range**: 0.56 and 0.56, 3.91 and 3.91. That is the signature.

Own speed over ground was 0.008 m/s and the targets were near zero, so the relative velocity vector is essentially nothing. CPA collapses to present range, and TCPA becomes range divided by GPS jitter. Today the jitter produced 6.2 minutes. The same two stationary boats could produce 300 minutes or 3. There is no bearing-rate signal to recover here, so no threshold on CPA or TCPA can separate signal from noise while both vessels are stopped. Only the speed filter can, and offshore has it disabled.

### The boat already knows

`navigation.state` reads `anchored`, published by `@meri-imperiumi/signalk-autostate`, which is running. Its vocabulary is `anchored`, `moored`, `sailing`, `motoring`.

The plugin never consumes it. Profile selection is a manual click in its webapp, which means the correct profile depends on someone remembering, at the exact moment they are busy anchoring.

### The plugin exposes the control

```
GET /plugins/signalk-ais-target-prioritizer/loadCollisionProfiles
PUT /plugins/signalk-ais-target-prioritizer/saveCollisionProfiles
```

The save handler is `Object.assign(collisionProfiles, req.body)`, a shallow merge. A PUT of `{"current":"anchor"}` therefore switches the active profile and leaves all four threshold sets untouched.

## Decision

### 1. Helmcentral sets `current` from `navigation.state`, and never sets anything else

The body is `{"current":"<profile>"}` and nothing more. Thresholds are the operator's, informed by their own waters. Helmcentral choosing numbers on their behalf is how an alarm system ends up tuned by someone who is not aboard.

The mapping:

| navigation.state | profile |
| --- | --- |
| `anchored` | `anchor` |
| `moored` | `harbor` |
| `sailing` | `coastal` |
| `motoring` | `coastal` |

`offshore` is never selected automatically. Autostate has no notion of it, and inferring blue water from "moving" would be a guess. It stays a deliberate manual choice.

### 2. Edge-triggered on the state transition, not on disagreement

The syncer remembers the last `navigation.state` it acted on and does nothing until that value changes. It does not reconcile continuously.

This is what makes a manual override stick. Under way the mapping says `coastal`, so a syncer that corrected any disagreement would stomp a deliberate switch to `offshore` on its next tick and the operator would never work out why the setting refuses to hold. Acting only on transitions means a manual choice survives until the boat next anchors or gets under way, which is exactly when a fresh decision is wanted anyway.

It also keeps the cost at nothing. The state is read from the in-memory snapshot every tick; the HTTP round trip happens only on a real transition, a handful of times a day.

### 3. Refuse to select a profile that cannot fire, and say so

The shipped `anchor` profile is `cpa: 0` for both tiers and `guard.range: 0`. The comparisons are `cpa < 0` and `range < 0`, so nothing can ever trip it. It is not a quiet profile, it is a silent one.

Selecting it on anchoring would take the AIS collision alarm offline at the moment the boat is unattended, and would look exactly like a well-behaved night. That is the masking pattern the fallback policy exists to prevent, so the syncer validates before it writes:

- the named profile must exist in the document (`getActiveCollisionProfile` returns `collisionProfiles[current]`, and an undefined profile makes `calcAlarms` throw inside a `try/catch` that swallows it, which is total silent failure)
- it must be able to fire at all: `warning.cpa > 0 || danger.cpa > 0 || guard.range > 0`

Failing either, the profile is left alone and Helmcentral raises its own alarm saying which profile was refused and why. Staying loud on the wrong profile is a worse night's sleep and a better outcome than going quiet on the right one.

### 4. The refusal clears itself

A raise with no matching clear leaves an open row in the alarm log forever. The syncer tracks what it refused and emits a clear once a later transition selects a profile successfully.

## Consequences

The first anchoring after this ships will refuse, because the `anchor` profile is still all zeros. That is the intended introduction: it surfaces a silent profile that has been sitting there since install, rather than quietly adopting it.

Configuring that profile is the operator's job, but the analysis that produced this ADR suggests a starting point: a speed floor of about 1 knot on both tiers, which removes every anchored neighbour currently jittering at 0.0 to 0.2 knots, plus a guard ring of about 0.25 NM at the same floor. Guard is the better instrument at anchor because it asks "is something moving, close to me" and never touches the unstable TCPA.

Helmcentral now writes to another plugin's configuration. That is a new kind of coupling and it is deliberately as narrow as it can be: one key, one endpoint, only on a state transition, and never a threshold. If the plugin changes that endpoint the sync fails loudly through the same refusal path rather than silently drifting.

The plugin's routes sit behind SignalK's auth, so this depends on Helmcentral's service account (ADR 0040) having access to them. Without it every transition refuses with a 401 and raises the alarm in decision 3, which is the designed failure but is noise if the cause is a missing credential rather than a bad profile. The account is not configured in the dev settings, so the live document shape could not be read from a workstation; the profile fixtures in the tests are the plugin's own shipped defaults, taken from its published source rather than from this vessel's saved config.

This does not fix the degenerate CPA at anchor. Nothing on our side can; the numbers genuinely carry no information when both vessels are stopped. It removes the conditions under which those meaningless numbers are allowed to sound a klaxon.

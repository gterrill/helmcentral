# ADR 0065: Inventory Records in the Binary

## Status

*Proposed.* This records a design ahead of its implementation, so the data model
can be argued with before code freezes it. Follows the precedent of
[ADR 0043](0043-service-schedule-provider-plugins.md), which did the same.

Reverses the remaining scope boundary of ADR 0043 and completes the direction
[ADR 0053](0053-engine-profiles.md) started.

## Context

ADR 0043 drew a line: maintenance and inventory *records* live in a separate
application, HelmLocker, and Helmcentral integrates with it through an embed
tile and the SignalK notifications tree. ADR 0053 moved half of that line when
maintenance and service history came back in-house. This moves the rest.
Inventory tracking, and behind it document management, are built here.

The reason is deployment arithmetic rather than architecture. Three applications
means three deployments, three backup regimes and three auth models on a boat
with one operator and one box. Nothing about the split earned that.

### The objection ADR 0043 raised, and why it does not survive

ADR 0043's strongest argument was startup safety. Helmcentral is safety-adjacent
and fails fast: if the secrets store or the plugin-override database cannot be
opened, the process exits rather than running degraded. Adding a CRUD store to
the same binary means "a CRUD store's problem can refuse to boot the anchor
alarm", and the escapes it named were a two-tier fatality policy inside one
process or a weakened invariant.

Both escapes exist to protect other people's installations from a bad upgrade.
There are no other installations. One operator, no installed base, and an
inventory database that fails to open is a broken box that gets looked at, not a
fleet incident that needs a policy. The store follows the same convention as
`nearby_contacts.go` and `plugin_overrides_store.go`: its own SQLite file,
`CREATE TABLE IF NOT EXISTS` at boot, and that is the whole of it.

The rest of ADR 0043's reasoning about record lifecycles still stands and is
acted on below: inventory is a durable business record with real export and
backup obligations, unlike the deliberately ephemeral telemetry ring buffer of
[ADR 0020](0020-in-memory-telemetry-history-optional-influxdb.md).

### What makes this worth building at all

Cheap EPC Gen 2 passive UHF tags, read through a USB reader in HID
keyboard-wedge mode, turn a stocktake into a walk through the boat instead of a
morning spent opening lockers. That is the differentiator, and every decision
below is downstream of what that hardware can and cannot actually do.

## Decision

### 1. An identifier is not an item

The item is the record. An identifier is a label bound to it, and one item may
carry several: an RFID EPC, a printed barcode, a typed part number.

This separation is load-bearing because the two identifier kinds mean different
things. A GTIN identifies a **product**, and sixteen identical tins of tomatoes
share one. An EPC identifies an **instance**, and no two tags match. Collapsing
them into a single "code" column produces a schema that cannot represent either
case correctly, and it does so silently.

### 2. Two item kinds in one table

- **Serialised**: one physical thing. Quantity one, may carry a serial number
  and warranty date. This is what RFID suits.
- **Quantity**: consumables counted in a bin. Fuel filters, tinned food, fuses.
  Identified by product code, counted rather than tagged.

One table with a kind discriminator, because browse, search, photo and location
behaviour are identical across both and only counting differs. Two tables would
duplicate all of it to distinguish one column.

### 3. A scan never infers absence

**This is the rule the feature is built around, and it is not negotiable.**

Passive UHF is absorbed by liquid and reflected by metal, and a boat locker is
mostly liquid and metal. A closed aluminium locker of tinned food may read
nothing at all. Read failure is the normal case, not the exception.

Therefore no scan sweep may mark an item lost, move it, or delete it. A pass
confirms what it finds and records `last_confirmed_at`. Items not seen are
reported as "not seen since" with that date, and the operator interprets it.

A design that let a sweep mark items missing would generate false losses on
every single pass, and the operator would learn to ignore the output. That is
the same failure mode [ADR 0038](0038-alarms.md) identifies as the reason people
switch marine alarms off: an alert that is usually wrong trains you to dismiss
it.

### 4. Location comes from bin tags, not from signal strength

HID keyboard-wedge mode delivers an EPC as keystrokes followed by Enter. There
is no RSSI, no read count, no antenna identity. The reader cannot tell the host
how close a tag was, so the host can never infer which bin holds an item.

Bins therefore carry tags of their own. Scanning a bin sets the session context,
and subsequent item scans are recorded against it until the context changes.

This also fixes the reader integration surface at one mechanism. Keyboard-wedge
covers UHF readers, 1D barcode scanners and 2D imagers with no per-device code,
no vendor SDK, no serial protocol and no driver. Anything needing more than
keystrokes is out of scope.

On the frontend, a wedge reader is indistinguishable from a fast typist. Scan
capture is one hook that buffers keystrokes, treats an inter-key gap above a
threshold as human typing, and emits a scan only on the terminating Enter. Every
scanning surface shares it.

### 5. Photos are files, vision extraction is optional

Photos are downscaled on ingest and written as files under the state directory
with a per-item cap, not as database blobs. A few hundred full-resolution phone
photos otherwise outweigh every other piece of state Helmcentral holds combined,
and blobs would put that weight inside a file that gets opened at boot.

Label reading uses an OpenRouter vision model, defaulting to something fast and
cheap such as `google/gemini-2.5-flash`, called through
`github.com/sashabaranov/go-openai` pointed at OpenRouter's OpenAI-compatible
endpoint. Four constraints:

1. **Off by default, and per-photo operator-triggered.** No background pass over
   the library. No photo leaves the boat without the operator asking for that
   photo to be read.
2. **It proposes, the operator confirms.** Extracted fields land in the form as
   editable suggestions. An unreviewed serial number is worse than an empty
   field, because it gets trusted months later when ordering a part and fails
   at the worst moment.
3. **Failure is visible and never blocking.** Boat internet is intermittent by
   nature. The call runs off the request path with a timeout, a failure says so,
   and the item saves without it.
4. **The key is a stored secret.** `OPENROUTER_API_KEY` joins `knownSecretKeys`
   in the encrypted store ([ADR 0023](0023-encrypted-secrets-store.md)) and is
   read directly from it, **not** through `LoadIntoEnv`. It must never enter the
   process environment, where every WASM plugin's `${VAR}` config expansion
   could reach it. This is the same reasoning that excludes `WEATHERKIT_*`.

This is the first outbound LLM call in the binary and the first runtime
dependency on a paid third party. Keeping it optional, per-photo and
non-blocking is what stops it becoming a hard one. If outbound LLM calls later
become a general capability rather than one feature's option, that earns its own
ADR.

### 6. Expiry reaches the alarm engine as a derived path

Expiry dates are the part of a boat inventory that bites: flares, EPIRB
batteries, liferaft service, extinguishers, medications.

A date is not a SignalK path, so it does not fit an `alarmRule`, which is
`{Path, Op, Value, Hysteresis, DwellSeconds}` over a live value.
[ADR 0055](0055-host-derived-vessel-paths.md) already solves this shape:
host-computed values published under the `helmcentral.` prefix onto the
gauge-values stream, where every widget and every alarm rule binds them with no
new machinery.

A derived `helmcentral.inventory.expiredCount` makes "flares out of date" an
ordinary alarm, with dwell, transports and acknowledgement already working, and
adds nothing to the alarm engine. A count rather than a path per item, because
per-item paths would grow the namespace without bound and make the path picker
useless.

This is the decision in this ADR with the least certainty behind it. It is
recorded so it gets reviewed rather than assumed.

### 7. Storage and export

Its own SQLite file in the state directory, following the one-store-one-file
convention. Photos as files beside it. Full JSON and CSV export, because ADR
0043 was right that this is a durable business record and the obligation does
not disappear because the records moved in-house.

`docs/reference/configuration.md` needs its backup guidance updated when this
lands: the photo library will become the largest thing in the state directory,
which currently calls out only `data/secrets.key` as backup-critical.

## Consequences

Positive:

- One deployment, one backup, one auth model.
- Expiry alarms need no alarm-engine change, only a derived path.
- Keyboard-wedge as the sole reader contract means no vendor SDK, no driver, and
  a barcode scanner works the day it arrives.
- The identifier split makes "16 tins" and "this exact pump" both representable
  without either being a special case.

Tradeoffs:

- Helmcentral's binary now carries CRUD-shaped code with a schema that will
  churn faster than the alarm schema. Accepted deliberately; the alternative was
  a second deployment.
- The state directory grows without bound in photos, and backup guidance has to
  keep up.
- RFID coverage on a boat is genuinely partial. The feature is honest about it
  rather than tuned around it, which means a stocktake is a confirming pass, not
  an authoritative one. An operator expecting warehouse-grade completeness will
  be disappointed, and the documentation says so up front.
- One paid third-party dependency exists, for one optional feature.

## Build order

1. Store, zones and bins, items, manual entry, browse and search.
2. Scan capture and stocktake mode, barcode scanners first since they need no
   radio and prove the wedge path.
3. RFID enrolment, bin tags, not-seen-since reporting.
4. Photos, then vision extraction behind its switch.
5. Widgets, then the derived expiry path and its alarm rule.
6. Floor plans.

Each stage is usable alone. A typed inventory with working search already beats
what the boat has now; everything after it is speed.

## Open questions

- Whether bins nest (a toolbox inside a lazarette locker) or whether zone and
  bin are the only two levels. Two levels is simpler and probably enough.
- How an item in a fixed installed position rather than a container is modelled.
  The spare alternator bolted to a bulkhead has a location but no bin.
- Whether quantity items hold a per-bin count or a single total. This matters
  the moment the same filters are stowed in two places, and getting it wrong
  means a migration.
- Whether floor plans justify their build cost, or whether a text description
  per zone gets most of the value. They are last in the build order for that
  reason.

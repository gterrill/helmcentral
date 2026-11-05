# ADR 0258: A Plain Setting Is Declared Once In The Browser

## Status

Accepted (2026-11-05). Changes how the settings form is built. Leaves the
settings wire format and file layout of ADRs 0025, 0027 and 0151 as they are.

## Context

Adding the Offline coastline switch (`offline_coastline.enabled`) took ten
edits for one boolean. In the browser it took five in `settings-draft.ts`: the
draft interface, its starting value, the read from the server, the
unsaved-changes comparison and the save patch. A sixth went into the payload
type in `use-settings-form.ts`. Go took four more in `settings_handlers.go`: the
struct, the write to `settings.yaml`, the read back and the normalising step.

The five browser edits were copies of one fact. Forgetting the comparison left
a field that never showed as unsaved. Forgetting the read left a field that
reset on every reload. Neither failed to compile.

## Decision

`settings-draft.ts` holds one table, `draftFields`, with an entry for every
draft field except `roles`. An entry says where the field sits in the settings
payload, its starting value, how the server's value becomes the draft value, how
the draft value becomes the saved one, and, where `===` will not do, how two
values compare. The starting draft, `hydrateDraftFromSettings`, `draftsEqual`
and `buildRegularSettingsPatch` all loop over the table.

A plain setting now costs an entry in the table and its type in the payload.
The draft's type, `RegularSettingsDraft`, is read off the table (plus `roles`),
so the interface is no longer written by hand. A field the table lacks is a
field the draft lacks, and every section that reads it fails `tsc`.

Each constructor accepts only the payload paths whose type matches what it
writes. `text` cannot point at `anchor.loa_m`, which holds a number, and a
section or name the payload lacks is not a path at all. A number box keeps its
default once: text that is not a number saves as the field's starting value.

Small constructors (`text`, `flag`, `choice`, `numberField`, `port`,
`modelList`) cover the common shapes. A field with rules of its own writes its
`read` and `write` out in its entry, next to a comment saying so: the
transducer depth (the server sends 0 for unset), the cost tier, and the passage
limits. `roles` stays out of the table because the patch leaves it out when the
page never read it.

`settings-draft-fields.test.ts` gives every field a changed value and checks
that the comparison, the patch and a save-then-reload each see it. It also
holds the patch the hand-written code produced, for a draft with spaces round
every text value and unusable numbers, so a swapped path or a dropped trim
fails. The same tests pass against the code the table replaced.

## What does not change

The payload the page sends, the saved files, the starting values, the numeric
fallbacks and the messages are all as before. The existing settings tests pass
unchanged.

## Rejected

- **Generating both sides from one schema:** Go and the browser would share a
  build step and a schema language for about forty fields. The two sides also
  differ on purpose (see below).
- **A generic settings framework:** The form has real exceptions, and a
  framework would carry them as options.

## Why Go stays as it is

`settings_handlers.go` reads and writes the payload in four hand-written
places, and a plain field repeats its name in each. A table there would need a
pointer to each struct field, or reflection, and the places do not agree about
fields. Whether a missing key keeps a default or counts as blank, whether a
zero is a value or an absence, and whether `normalizeSettingsPayload` or
`buildSettingsPayload` owns the default differ field by field (ADRs 0099, 0106,
0135 and 0159 each turn on one). A table would have to carry those rules as
per-row closures, which is the code now with an extra indirection. A Go field
still costs four edits.

## Limits

- **Same-typed paths can be swapped:** The type check separates strings from
  numbers, booleans and lists. It cannot tell `tide_station_id` from
  `tide_station_name`, or one number from another. The expected patch in the
  test pins those.
- **Two lists to keep:** The payload type in `use-settings-form.ts` and the
  table both name each field. The table checks its paths against the payload,
  so they cannot drift apart unnoticed.

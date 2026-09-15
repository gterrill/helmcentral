# ADR 0100: Plugins Declare Their Own Settings

## Status

Accepted. Rewritten from this ADR's own first draft, which never landed on
main (see "History" below). References ADR 0024 (plugin allowlist
overrides) and ADR 0091 (points of interest as a plugin kind).

## Context

`OVERPASS_API_URL` was an env var read once at startup (`main.go`), used to
override the Overpass endpoint for three consumers: place-name resolution
(`place_name.go`), the assistant's `find_places` tool, and the osm-overpass
POI plugin's `overpass_url` config value (wired through its
`osm-overpass.config.json` sidecar's `"${OVERPASS_API_URL}"` reference).
Commit e704651 needed a working mirror to reach Overpass at all from the
boat network (`overpass-api.de` refuses that network outright; the POI
plugin's bbox-bounded query fix landed in the same commit), which is what
surfaced how awkward this was to actually use: setting it meant editing
`docker-compose.yml` (or the systemd unit's `Environment=` lines) and
restarting the container, on a machine reached over SSH, for a value that
has nothing to do with build or deployment - it is the operator picking
which public API endpoint to hit today.

## History: the env var, then a rejected global setting

The first version of this ADR moved the value to a global `overpass.url`
`settings.yaml` field: `settingsPayload` gained an `Overpass.URL` field,
`currentOverpassAPIURL()` read it fresh on every lookup, and a Settings ->
General field edited it from the UI. That draft was built but never
committed, and a second look at it - specifically, designing the analogous
"the osm-overpass plugin's Google Places sibling might someday need its own
per-plugin editable setting too" case - surfaced the actual mistake: the
Overpass mirror is not a Helmcentral-wide concept. Nothing else in this app
reads `overpass.url` for its own sake; it exists purely because the
osm-overpass plugin (and, transitively, two backend call sites that
piggyback on the same endpoint) needs a mirror to query. Giving it a global
settings.yaml key made every future plugin-specific "let the operator edit
this from Settings" need look like it wanted the same treatment - a new
top-level settings.yaml field and a new General-section input, forever,
one per plugin, regardless of whether the value means anything outside that
plugin's own config.json.

That is the wrong shape. A plugin's config is the plugin's own business,
the same way its `allowed_hosts.json` and `allowed_secrets.json` already
are (ADR 0024). The fix is a general mechanism for a plugin to declare which
of its own config keys an operator may edit, and where - not a bespoke
global setting for this one plugin's one field.

**Env var and global setting are both rejected** for the same underlying
reason: neither respects that this is the plugin's setting, not the host's.
An env var requires a restart and SSH access; a global setting requires a
reviewed, host-side code change (a new `settingsPayload` field, a new
General-section input) for every single plugin-specific value a future
plugin author might want to expose, forever conflating "the operator should
be able to edit this from a browser" with "this is meaningful outside the
plugin that defines it."

## Decision

### A new sidecar: `<name>.config_fields.json`

A plugin ships a `<name>.config_fields.json` sidecar next to its `.wasm`:
a JSON array of field declarations,

```jsonc
[
  {
    "key": "overpass_url",
    "label": "Overpass server",
    "type": "url",
    "placeholder": "https://overpass-api.de/api/interpreter",
    "help": "Blank uses the public overpass-api.de. Use a mirror such as https://overpass.openstreetmap.fr/api/interpreter if your network refuses it. Any mirror other than those two must also be added to this plugin's allowed hosts."
  }
]
```

`key` must be unique and non-empty within the file; `type` is `"url"` or
`"text"` (a closed set - an unrecognised type is an authoring mistake, not
a forward-compat signal to silently ignore). `label`, `help` and
`placeholder` are cosmetic, shown by the Settings provider modal. A
malformed sidecar - bad JSON, an empty or duplicate key, an unknown type -
fails plugin load outright
(`pluginConfigFieldsForWasmPlugin`/`newWasmPluginBase`, `wasm_plugin.go`),
the same "fail loudly on an author mistake" treatment every other malformed
sidecar file gets in this package.

This is a genuinely new mechanism, not the old `"${settings.<path>}"`
config.json reference syntax carried over: that syntax (and its allowlist,
`wasmPluginExposedSettings`) is deleted entirely, along with the global
`overpass.url` `settings.yaml` field, the General-section Overpass field,
and `settingsValidationError`'s `Status` field (added for that field's 400
response and used nowhere else).

### Storage: a new table in the existing plugin overrides database

`plugin_overrides_store.go`'s SQLite database (already holding
`plugin_overrides`, the allowlist-override table from ADR 0024) gains
`plugin_config_values(wasm_path, key, value, updated_at, PRIMARY
KEY(wasm_path, key))` - one row per declared field an operator has actually
set, keyed by the plugin's full wasm path (matching `plugin_overrides`'
own keying, for the same reason: two plugins in different domains can
report the same self-reported id). `GetConfigValues`/`SetConfigValues`/
`DeleteConfigValues` round out the store; `SetConfigValues` treats a blank
value as "delete this row," not "store an empty string" - the field reverts
to whatever `config.json` provides for that key, or the plugin's own
built-in default if `config.json` has no entry either, matching how a blank
value has always meant "unset" everywhere else in this app.

### Per-call resolution, not per-load

Exactly like the rejected settings-ref mechanism it replaces,
a stored config value is resolved **fresh on every plugin call**, not baked
in once at load: `wasmPluginBase.call`'s `applyConfigValues` reads
`plugin_config_values` for this plugin's `configFieldKeys` (extracted from
the sidecar at load time) and overlays any stored values onto a **clone**
of `instance.Config` before the guest call. The clone matters for the same
reason it did before: `extism/go-sdk`'s `Instance()` sets `instance.Config`
to the exact same map as the compiled plugin's `manifest.Config`, by
reference, shared by every instance that compiled plugin will ever create -
mutating it in place would leak one call's resolved value into the
manifest, and therefore into every other call, a bug that looks like a
value that never updates rather than a momentary one. A missing store (nil
- no plugin has ever saved a value in this process) leaves `config.json`'s
own value in place; a store read error fails the call outright rather than
masking it as "operator hasn't configured this yet."

### Backend read/write: extending the existing plugin-info API

`GET /api/plugins/:type/:id` (already returning allowlist state, ADR 0024)
gains `config_fields: [{key, label, type, help, placeholder, value}]` -
always an array, empty when the plugin declares none. `POST
/api/plugins/:type/:id/config` (new, same admin tier as the overrides
endpoints) accepts `{"values": {"<key>": "<value>"}}`, rejects an unknown
key or a non-blank `"url"`-typed value that isn't an absolute `http(s)` URL
(400, naming the offending key/field), saves via `SetConfigValues`, and
returns the updated info. The URL check here is deliberately generic (any
absolute `http`/`https` URL) - a plugin's own domain-specific constraints
(osm-overpass requires `https` specifically) are enforced by the plugin
itself when the value is actually used, not duplicated at the host layer
for every field type a future plugin might declare.

### Frontend: the modal renders what the plugin declares

`provider-settings-modal.tsx` renders `config_fields` as inputs (label,
help text, placeholder) above the existing allowed-hosts/allowed-secrets
editor, seeded from each field's current `value`. Save posts `/config`
first (awaited on its own - a 400 there stops before touching the
allowlist or secrets, so a rejected config edit never looks like the other
two succeeded), then proceeds with the existing overrides/secrets save.
This is the same one-Save-button shape ADR 0024 established for the
allowlist editor, extended rather than replaced. The UI notes the two
different effective-timing contracts side by side: a config field applies
on the very next plugin call, while an allowlist change still needs a
restart (ADR 0024, unchanged).

### The allowlist stays manual

Nothing about moving `overpass_url` from a global setting to a
plugin-declared one changes ADR 0024's allowlist boundary. Saving a new
Overpass mirror does not by itself grant osm-overpass network access to it:
`osm-overpass.allowed_hosts.json` already lists `overpass-api.de` and
`overpass.openstreetmap.fr` (the mirror added for e704651's bbox fix), and
any other mirror still needs adding there, or to the Settings allowlist
override, before the plugin's calls to it succeed. **Rejected, again:**
auto-granting whatever host a `"url"`-typed config field names. That would
make an ordinary config-field save silently widen the plugin sandbox's
network allowlist, collapsing two independent operator decisions - "which
value should this field hold" and "should this plugin be allowed to reach
that host" - into one, for every future `"url"`-typed field any plugin ever
declares. ADR 0024 already treats the allowlist as something the operator
reviews explicitly; this mechanism doesn't touch that boundary at all.

### osm-overpass: the first (and so far only) adopter

`osm-overpass.config.json` is deleted - it had exactly one key
(`overpass_url`, previously `"${settings.overpass.url}"`), and that key
moves entirely to the new `osm-overpass.config_fields.json` sidecar and
`plugin_config_values`, leaving nothing for `config.json` to carry.
`osm-overpass.go`'s `resolveOverpassURL` is unchanged in every way that
matters to the plugin itself: it still just reads the `overpass_url` config
value it's handed, with no idea whether the host resolved that from
`config.json`, an env var, or a stored operator value.

### Phase B: place names and find_places still reach into the plugin directly

`place_name.go`'s `currentOverpassAPIURL()` - used by place-name resolution
and, via `postOverpassQuery`, the assistant's `find_places` tool - is not
itself the osm-overpass plugin, but needs an Overpass endpoint before either
of those code paths can run. For this phase it looks up the registered
`osm-overpass` POI provider, reads its stored `overpass_url` value from
`plugin_config_values` via that provider's wasm path, and falls back to the
public default when nothing is stored or the provider isn't registered at
all (not itself a masked error: "no plugin-specific override exists" is a
different, valid state from a broken value). This is a deliberate stopgap,
marked as such in the code: a later phase moves both call sites into the
plugin itself (`find_places` becoming a plugin-backed tool, place-name
resolution going through the POI provider interface), at which point
`currentOverpassAPIURL` is deleted rather than generalized further.

## Rejected

- **An env var** (`OVERPASS_API_URL`). Requires SSH and a restart for a
  value that is otherwise a same-session, no-restart Settings edit for
  every other operator-facing knob in this app.
- **A global `overpass.url` `settings.yaml` field** (this ADR's own first
  draft - see "History" above). Treats a plugin-specific config value as an
  app-wide concept, requiring a reviewed host-side code change (a new
  `settingsPayload` field, a new General-section input) for every future
  plugin that wants the same "editable from Settings" treatment.
- **Auto-granting the configured host to the plugin's allowlist.** See "The
  allowlist stays manual" above.
- **A generic settings-path-in-config.json mechanism supporting arbitrary
  nesting or multiple references per value** (also inherited from the first
  draft). Nobody needs it, and it doesn't apply once the value in question
  isn't a global setting to reference in the first place.

## Consequences

Changing the osm-overpass plugin's Overpass mirror is a Settings save from
that plugin's own settings modal, validated on save and taking effect on
the very next `fetch_poi` call - no SSH, no compose edit, no restart. The
same `config_fields.json` sidecar and `/config` endpoint are available to
any future plugin with an operator-editable value of its own, without
inventing a new global settings.yaml field and UI section per plugin. The
tradeoff is one more sidecar file and one more admin-tier endpoint to
maintain, in exchange for never having to touch `signalk.go`'s
`settingsPayload` again just because a plugin author wants a browser-editable
config value.

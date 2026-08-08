# ADR 0031: A Generic Embed Widget with Per-Instance Configuration

## Status
Accepted

Extends ADR 0012, which introduced the configurable bento dashboard, and ADR 0013, which made it multi-page. Both assumed a widget is fully described by its id and its position. This ADR breaks that assumption for exactly one widget type.

Decision 8 (theme synchronisation) was added in place after the fact. It is a pure extension — nothing above it changed — and it is recorded here rather than as its own ADR because it is entirely a consequence of decision 1's vendor-neutrality rule.

## Context

Operators build panels in Grafana — a windrose, polar plots, battery history — against the same InfluxDB this app already writes to. Those panels belong next to the native tiles, not behind a separate browser tab.

Embedding itself was never the obstacle. The app sets no Content-Security-Policy, and `radar-drawer.tsx` has embedded Windy in an iframe since the radar panel shipped. Grafana on the boat LAN with `security.allow_embedding = true` and anonymous access loads in an iframe with no backend involvement at all.

The obstacle was the widget model. A widget was `{id, x, y, w, h}` and nothing more:

- there was **no per-widget config field** on either side of the API, and
- **the id was the instance key** — `validateDashboardWidgets` rejected `duplicate widget id` outright.

So a `grafana` widget id would have permitted exactly one embedded panel per page, with its URL stored somewhere global. The operator wants several, and wants them on different pages.

## Decision

1. **One generic `Embed` widget, not a Grafana widget.** It takes a URL and a title and renders them in an iframe. Grafana is the motivating case, not a coupling: Node-RED dashboards, Home Assistant cards and Signal K's own plugin UIs all work without further code.

   A Grafana-aware widget was rejected. It needed all the same instance-id and per-instance-config machinery underneath — the actual cost of this change — and would have bought only a slightly shorter first-run flow, in exchange for solving one vendor.

2. **Embed instances carry a token in their id: `embed:<token>`.** Builtin widgets keep their fixed ids and their one-per-page limit. Only embeds are pluralizable.

   This was chosen over adding an `instanceId` field to every layout item, which would have required migrating every persisted page and touching every test that hardcodes a widget array. The prefix approach is additive: existing pages, `defaultDashboardLayout` and `validDashboardWidgetIDs` are all untouched.

   **The duplicate-id check needed no change.** Tokens are unique per instance, so `seen[w.ID]` still catches a genuinely duplicated embed while permitting distinct ones.

3. **Config lives on the layout item, not in `settings.yaml`.** `dashboardLayoutItem` gains `Embed *dashboardEmbedConfig` with `json:"embed,omitempty"`; the frontend's `DashboardLayoutItem` gains the matching optional `embed`.

   The config is per-instance and per-page, which is exactly the shape `dashboard-pages.json` already stores. Routing it through `settings.yaml` would have put per-page data behind `POST /api/settings`, whose full-replace semantics (ADR 0025 §3) and live SignalK probe on save (ADR 0027) have nothing to do with a panel URL. `omitempty` keeps existing `dashboard-pages.json` files byte-identical.

   Removing an embed widget discards its config. That is the expected reading of "remove this panel".

4. **`crypto.randomUUID()` is unusable here.** It is secure-context-only, and Helmcentral is normally served over plain HTTP from a LAN address, where it is `undefined`. Wrapping it in a runtime fallback would be exactly the kind of masking this repo's fallback policy forbids — the API is not intermittently unavailable, it is reliably absent in the deployment that matters.

   `newEmbedWidgetId` therefore mints `Date.now().toString(36)` plus eight base-36 random characters, retrying on the (vanishingly unlikely) collision with an id already on the page. The token is a layout key needing uniqueness within one page, not a secret, so this is the honest tool for the job. The backend validates the shape (`^[A-Za-z0-9_-]{8,64}$`) rather than a UUID.

5. **URLs are validated on both sides, and the two rule sets are deliberately duplicated.** `validateEmbedWidget` in Go and `isValidEmbedUrl` in TypeScript both require an `http`/`https` scheme, a non-empty host, and lengths within 2048 (URL) / 64 (title). The dialog needs synchronous feedback; the server must not trust the client.

   The two parsers disagree on one input, and the frontend carries an explicit guard for it: the WHATWG `URL` parser silently rewrites an empty authority, turning `http:///d-solo/a` into host `d-solo`, while Go's `net/url` leaves the host blank and rejects it. Without the guard the dialog would happily accept a URL the server then 400s. Documented here so it isn't rediscovered as a mystery rejection later.

   Config attached to a **builtin** widget id is rejected rather than silently dropped. Config the renderer will never read means the caller has misunderstood the model, and saying so beats swallowing it.

6. **A new embed is not persisted until it has a URL.** The backend rejects a blank URL, so `handleAddEmbed` holds the new item in local state and opens the dialog; only Save calls `updatePage`. Cancel discards it.

   The alternative — permitting blank URLs server-side so the UI could persist a placeholder — was rejected: it would have relaxed a correct validation rule to accommodate a transient UI state, and left "a widget that cannot render" as a legal persisted value.

7. **The iframe is sandboxed and click-through during layout mode.**

   `sandbox="allow-scripts allow-same-origin allow-popups allow-forms"`. Grafana needs scripts and its own origin's session to render at all. The well-known caveat that `allow-scripts` plus `allow-same-origin` lets a frame remove its own sandbox applies only when the frame is *same-origin with the embedder*, which an operator-supplied external panel is not. Top-level navigation is not granted.

   `pointer-events-none` while `editing`. A drag or resize whose mouse-up lands over the iframe is otherwise swallowed by the embedded document and the gesture never completes — the classic iframe-drag bug. The drag handle and resize corner both sit outside the frame, so nothing is lost.

8. **A `theme` param already in the URL is kept in sync with the app's day/night mode; one that is absent is never added.** An embedded panel renders in whatever theme its own server defaults to, so a dark Grafana panel sat inside a light dashboard card. Grafana reads `?theme=light|dark`, and `EmbedTile` now rewrites that param from the app's `isDarkTheme`.

   Injecting `theme=` into *every* embed URL was rejected, and decision 1 is the whole reason. `theme` is a Grafana convention, not a universal one; writing it unconditionally would push it onto the Node-RED, Home Assistant and Signal K plugin UIs this widget deliberately also serves, and would collide with any embed using `theme` for its own purposes. Opt-in by presence means the operator writes `&theme=` once and the app takes over from there, while untouched URLs stay byte-identical — which is also what keeps the rewrite honest enough to assert in a test.

   The parent cannot style a cross-origin frame, so this is the only theming lever available without a proxy. It reaches the panel chrome only: the windrose plugin in use draws its own legend and ignores Grafana's theme, so a perfect match is not achievable this way.

   **The memoisation is load-bearing, not an optimisation.** Changing an iframe's `src` remounts the frame. Deriving the URL from the theme flag means it is no longer referentially stable across App's frequent re-renders, so without `useMemo` the string is rebuilt every render and the embed reloads continuously. Toggling day/night does reload the panel once, which is accepted.

## Consequences

**Positive:**

- Any number of embeds per page, each independently configured, on any page.
- No backend proxy, no auth plumbing, no CSP work — the direct-iframe path is the whole feature for a LAN Grafana.
- The per-instance config field is now precedent. A second widget wanting per-instance settings extends `dashboardLayoutItem` the same way instead of inventing another mechanism.
- `widgetDisplayName` fixes a small pre-existing wart: the remove button's `aria-label` interpolated the raw widget id.

**Tradeoffs:**

- The frontend's `DASHBOARD_WIDGET_IDS` and the backend's `validDashboardWidgetIDs` remain two hand-maintained lists (ADR 0013's known wart). This change does not fix that, and adds a third pair to keep in step: the URL validation rules.
- The operator pastes a full URL per widget. Moving Grafana to a new host means editing every embed. A Settings section holding named base URLs was considered and deferred; it is a pure addition on top of this design.
- If Helmcentral is ever served over HTTPS, `http://` panels break as mixed content. The dialog warns about this; nothing enforces it.
- Grafana still requires `security.allow_embedding = true` server-side. Nothing in this app can detect or report that — a frame refused by `X-Frame-Options` simply renders blank.
- No `frame-src` CSP exists to constrain which origins may be embedded. Deferred: the app has no CSP at all today, and adding one only for embeds would be a partial measure.
- Theme sync (decision 8) is silent when it does nothing. An operator whose URL lacks `theme=` gets no hint that the toggle could drive the panel; the config dialog does not mention it.

**Deferred follow-ons:** a backend reverse proxy injecting a Grafana service-account token (needed only if the operator's Grafana ever requires auth, since an iframe cannot carry an `Authorization` header); a Settings section for named embed sources; a `frame-src` allowlist.

## Related

- ADR 0012 — configurable bento dashboard (`react-grid-layout`, one global layout)
- ADR 0013 — multi-page dashboard, and the two hand-maintained widget-id lists
- ADR 0025 §3 — `POST /api/settings` full-replace semantics
- ADR 0027 — settings validation firing a live probe inside the save path
- `backend/dashboard_pages.go` — `validateEmbedWidget`, `dashboardEmbedConfig`
- `frontend/src/lib/dashboard-widgets.ts` — `isEmbedWidgetId`, `isValidEmbedUrl`, `newEmbedWidgetId`
- `frontend/src/components/embed-tile.tsx`, `frontend/src/components/embed-config-dialog.tsx`

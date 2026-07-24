# ADR 0028: "Test Connection" Replaces "Connect"; One Write Path for the SignalK Address

## Status
Accepted

Follows ADR 0027, which validated the bulk save path but left the second write path in place.

## Context

After ADR 0027 the SignalK address had two endpoints that could persist it, and both now probed before doing so:

- `POST /api/settings/signalk` — the "Connect" button. Probed, then wrote just the SignalK stanza.
- `POST /api/settings` — the sectioned settings page's Save. Probes on change (ADR 0027), then writes everything.

Two write paths for one field is a maintenance hazard on its own — ADR 0027's validation had to be added twice over, and any future rule has to be remembered in both places. But the sharper problem was the button's semantics. "Connect" read as a connectivity check and behaved as a save, which meant:

- It bypassed the page's own save model. Every other field on the sectioned settings page is a draft that persists via the pinned "Save Settings" button; the address alone committed the moment you clicked a button that didn't say "save".
- **You could not check an address without committing to it.** The only way to find out whether an address worked was to make it live. That is precisely backwards for a diagnostic, and it is the operation an operator most wants to perform speculatively.
- Its response rewrote the form. The handler echoed back the address/port it had persisted and the component fed that into the draft, so the server could overwrite what the operator had typed.

## Decision

1. **`POST /api/settings/signalk` is deleted, not deprecated.** With it goes `saveSignalKSettings`, which had no other caller. Persisting the address is now solely `POST /api/settings`' job, where `validateSettingsChange` applies the probe. One field, one write path, one place to change the rules.

2. **`POST /api/settings/signalk/test` is a pure read.** It probes the address in the *request body* — not the persisted one, so an operator can evaluate a value before committing to it — and returns `{connected, url, vessel_name}`, or 502 with `{error, field, connected: false}`. It never touches `settings.yaml`. Tests assert the file is byte-identical after both successful and failed probes.

   `GET /api/settings/signalk` stays: `use-settings-form.ts` falls back to it when `GET /api/settings` fails.

3. **The probe reports which vessel answered.** ADR 0027 listed "a reachable-but-wrong address still saves" as an unresolved tradeoff — the probe can tell you something is listening, not that it is *your* boat. The save path can't close that gap (it has no one to ask), but a diagnostic button can: the response carries the vessel name and the UI shows "Connected — Pikorua responded". Reaching a neighbour's server on the same marina wifi is now visible rather than silent.

4. **Blank or out-of-range input is a 400, not a probe.** The old handler defaulted a blank address to `localhost` and an invalid port to 3000, so an empty field produced "cannot reach localhost" — an error about an address the operator never entered, pointing them at the wrong problem.

5. **The button does not call `onChange`.** The typed value is the input to the test, not something the test may overwrite. A test pins this by returning a *different* address in the probe response and asserting the draft is unchanged.

6. **Feedback moved next to the button.** The success/error banner previously rendered at the foot of the section, below the credentials fieldset and one input per tank sensor — off-screen from the control that produced it. It now sits directly beneath the address/port row.

## Consequences

Positive:
- The address has exactly one write path, and it validates.
- Testing an address is now free of consequence, which is what makes it worth doing before saving rather than after.
- The section follows the same draft-then-save model as every other part of the settings page; "Test Connection" describes what the button does.
- Verified end-to-end against the ADR 0026 stack: probing the real vessel from the E2E stack returns `{"connected":true,"vessel_name":"Pikorua"}` while the E2E stack's own persisted address stays at its seed value, and the UI shows `CONNECTED — PIKORUA RESPONDED. SAVE SETTINGS TO APPLY.` after a single POST to `/api/settings/signalk/test` and no other write.

Tradeoffs:
- **Changing the address is now two actions** (test, then Save Settings) where it used to be one. Deliberate: the one-click version was a save disguised as a check. The success message says "Save Settings to apply" so the second step isn't a surprise.
- **A changed address is probed twice** if the operator tests and then saves — once by the button, once by `validateSettingsChange`. Skipping the second probe on the grounds that the button "already checked" would mean trusting client state about a check the server can't see, so the save re-verifies.
- **`POST /api/settings/signalk` now returns 405** for anything still calling it. Only the settings UI ever did, and it ships in the same commit; no external consumer is known, but the endpoint is gone rather than kept as a redirect.
- **The vessel name is only as good as what SignalK reports.** A server without `vessels/self.name` set yields the plainer "Connected." message.

# ADR 0027: Validating Bulk Settings Saves on Change

## Status
Accepted

Addresses the item ADR 0026 left open under "Not addressed here". Extended by ADR 0028, which deleted `POST /api/settings/signalk` — leaving the validation described here as the *only* thing guarding the address — and closed the "reachable-but-wrong address" gap noted under Tradeoffs.

## Context

The app has two ways to write the SignalK address, and they disagreed about whether an address has to work:

- `POST /api/settings/signalk` (the "Connect" button) probes the server with `fetchSignalKVesselState` and returns 502 rather than persist something unreachable.
- `POST /api/settings` (the sectioned settings page's Save) is a full-payload replace that wrote `settings["signalk"]` straight from the request body, unexamined.

So the endpoint whose *purpose* is connecting was careful, and the endpoint that happens to carry the address along with thirty other fields was not. ADR 0026 removed the blast radius of that gap by giving browser-driven runs their own stack, but the gap itself remained: a typo in the address field, saved from the settings page, still takes the dashboard offline with no feedback — and `settings.yaml` is gitignored, so there is no history to recover the previous value from.

"Save and Continue" sharpens this. It fires from the unsaved-changes dialog when navigating away mid-edit, which makes *persisting a half-finished value* the path of least resistance rather than a deliberate act.

## Decision

1. **Validate, but only on change.** `validateSettingsChange(current, next)` (`backend/signalk.go`) compares the incoming SignalK address and port against what is currently persisted. If either differs, it probes the new endpoint and rejects the whole save with 502 if unreachable. If they are identical, no probe happens at all.

   This asymmetry is the load-bearing part of the decision, not an optimisation. Probing on every save would mean that whenever the vessel is unreachable — powered down, out of wifi range, configuring from home, all routine states for this app — the operator is locked out of editing tank labels, anchor geometry, units and provider selection, none of which have anything to do with SignalK. That would be a worse and far more frequent failure than the one being fixed. Guarding the *transition* rather than the steady state is what makes a connectivity check safe to attach to an endpoint that saves everything at once.

2. **Reject the entire payload, persist nothing.** A rejected save leaves `settings.yaml` byte-identical, including the unrelated fields travelling in the same request. Saving everything *except* the bad address was rejected: it silently diverges what the operator sees in the form from what is on disk, and quietly succeeding at 90% of a request the caller believes failed is precisely the kind of masking this codebase avoids elsewhere.

3. **The response names the field.** `{"error": "unable to connect to SignalK at http://…", "field": "signalk.address"}`. The message repeats the address that failed, since the operator's next action is almost always to correct it. `field` exists because this is explicitly the first of a set — the function is a seam for further bulk-save checks — and it lets the UI attach a message to a section rather than the page.

4. **Surface the reason in the unsaved-changes dialog.** `handleSaveAndContinue` in `App.tsx` already stayed on the page when the save rejected, on the reasoning that "the Settings page's own error banner is visible underneath". It is not: the dialog is modal and covers it. Before this change that comment was harmless because `POST /api/settings` never rejected; introducing a rejection made it wrong. The dialog now renders the error itself, so the failure is legible in the flow where it is most likely to occur, instead of "Save and Continue" appearing to do nothing.

## Consequences

Positive:
- The address cannot be changed to something unreachable through either endpoint. The two write paths now agree.
- Verified end-to-end against the ADR 0026 stack: the original incident script (fill `#signalk-address`, navigate away, "Save and Continue") is refused with 502, the persisted address is unchanged, the user stays on Settings, and the dialog shows `UNABLE TO CONNECT TO SIGNALK AT HTTP://192.168.50.243:9`.
- Editing unrelated settings while the vessel is unreachable keeps working, which the test suite pins explicitly rather than leaving to inference.

Tradeoffs:
- **A reachable-but-wrong address still saves.** The probe answers "is something SignalK-shaped listening", not "is this the right vessel". Pointing at a neighbour's server on the same marina wifi would pass.
- **A save that changes the address costs a round trip**, up to the 3s `fetchSignalKVesselState` timeout, on the request path. Only paid when the address actually changes.
- **No override.** An operator configuring for a vessel that is currently powered down cannot pre-set its address from the settings page. This matches the existing "Connect" button, which has always behaved this way, so it introduces no new limitation — but it does entrench one. A `force` flag is the obvious escape hatch if this turns out to bite; it was not added speculatively.
- **Validation is server-side only.** The form does not pre-check reachability as the operator types, so the feedback arrives on save rather than on blur.

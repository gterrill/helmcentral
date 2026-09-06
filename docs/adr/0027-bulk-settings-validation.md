# ADR 0027: Validating Bulk Settings Saves on Change

## Status
Accepted

Addresses the item ADR 0026 left open under "Not addressed here". Extended by ADR 0028, which deleted `POST /api/settings/signalk` — leaving the validation described here as the *only* thing guarding the address — and closed the "reachable-but-wrong address" gap noted under Tradeoffs.

## Context

The app has two ways to write the SignalK address, and they disagreed about whether an address has to work:

- `POST /api/settings/signalk` (the "Connect" button) probes the server with `fetchSignalKVesselState` and returns 502 rather than persist something unreachable.
- `POST /api/settings` (the sectioned settings page's Save) is a full-payload replace that wrote `settings["signalk"]` straight from the request body, unexamined.

The bulk endpoint therefore accepted addresses that the connection endpoint would reject. ADR 0026 isolated browser-driven tests in their own stack, but a typo saved from the settings page could still take the dashboard offline without feedback. Because `settings.yaml` is gitignored, its previous value cannot be recovered from Git history.

"Save and Continue" can persist a half-finished value when the operator navigates away mid-edit.

## Decision

1. **Validate, but only on change.** `validateSettingsChange(current, next)` (`backend/signalk.go`) compares the incoming SignalK address and port against what is currently persisted. If either differs, it probes the new endpoint and rejects the whole save with 502 if unreachable. If they are identical, no probe happens at all.

   Checking only changed addresses allows unrelated settings to be saved while the vessel is unreachable, for example when powered down, out of wifi range, or being configured from home. Probing on every save would block tank labels, anchor geometry, units, and provider selection even when the operator had not changed SignalK settings.

2. **Reject the entire payload, persist nothing.** A rejected save leaves `settings.yaml` byte-identical, including unrelated fields in the same request. Saving everything except the bad address was rejected because the form would no longer match the stored settings, and a reported failure would conceal a partial save.

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
- **No override.** An operator cannot pre-set the address of a powered-down vessel from the settings page. This retains the existing "Connect" button's limitation. A `force` flag could permit this later, but was not added without a demonstrated need.
- **Validation is server-side only.** The form does not pre-check reachability as the operator types, so the feedback arrives on save rather than on blur.

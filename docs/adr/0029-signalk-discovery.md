# ADR 0029: SignalK Discovery by Unicast Sweep, Not mDNS

## Status
Accepted

Builds on ADR 0028, which made `POST /api/settings` the single write path for the SignalK address.

## Context

Setting Helmcentral up meant knowing your SignalK server's IP and typing it into Settings → SignalK. That is a poor first-run experience, and a fragile one: if the address ever changes, the dashboard goes quiet and the only signal is an error string in a field the operator has to go looking for.

The obvious mechanism is mDNS, and the server genuinely advertises itself:

```
SignalK on ZimaOS._signalk-http._tcp.local  →  ZimaOS.local:3000
```

**It is nonetheless unusable from this backend.** Measured against the running stack:

| Check | Result |
| --- | --- |
| `getent hosts ZimaOS.local` in `backend-dev` | fails (exit 2) — musl libc, `hosts: files dns`, no NSS-mDNS |
| Container network | `172.18.0.2/16` bridge inside a Docker Desktop VM; multicast does not reach the physical LAN |
| `host.docker.internal` | `fdc4:f303:9324::254` — Docker's gateway, not the LAN |
| Unicast DNS for `ZimaOS` / `ZimaOS.lan` | no records on the router or on `192.168.50.240` |

Supporting it would need a pure-Go mDNS resolver *plus* host networking, and on Docker Desktop for macOS host networking lands in the VM rather than on the LAN — substantial work for an outcome that would likely still not function.

What does work from a bridge container is plain unicast TCP. That is how the correct server was located by hand during the ADR 0026 incident.

## Decision

1. **Sweep the LAN over unicast; verify each hit is really SignalK.** `POST /api/signalk/discover` dials port 3000 across a `/24` (64 at a time, 600ms per dial, 12s total budget) and confirms each responder answers `/signalk` with an endpoint descriptor before reporting it. A TCP connection alone is not evidence: a router admin page on the same port would otherwise be offered to the operator as a vessel. Each confirmed server is reported with its **vessel name** (via the existing `fetchSignalKSelfName`) and version — "a server answered" does not tell the operator whether it is their boat.

2. **The network to sweep is derived, and never guessed.** The backend sits on `172.18.0.0/16` and cannot see the LAN it is meant to search. In order: the `hint` the frontend sends (`window.location.hostname`, only when it is an IPv4 literal — covers a first install, where the operator browsed to the boat computer's LAN address); then the currently-configured SignalK address (covers "the server moved", and still works when the dashboard is open at `localhost`); then `HELMCENTRAL_DISCOVERY_SUBNET`. If none yields a network, that is a 400 with a reason — sweeping an arbitrary subnet would appear to work while searching the wrong place.

   **Only RFC1918 ranges are accepted.** A caller must not be able to aim a port sweep at a public range.

3. **Discovery persists nothing.** Like the connection probe from ADR 0028, it is a pure read; a test asserts `settings.yaml` is byte-identical afterwards. Accepting a result saves through `useSettingsForm().save()` → `POST /api/settings`, the single write path. Saving on the operator's explicit "Store these settings" is a deliberate act — unlike the old Connect button, whose fault was saving as a side effect of a connectivity *check*.

4. **The prompt is quiet until it has something actionable.** It searches in the background when SignalK is unconfigured or `vessel-state` reports `signalk-unreachable`, and raises a dialog **only** once at least one server is found. It does not put a modal over the dashboard to announce that it is searching, nor to report that it found nothing or could not run.

   This was a correction during implementation, not the original design. The first version opened the dialog immediately and rendered "Looking for SignalK…" — which blocked the entire dashboard for the duration of a multi-second sweep, on every fresh start. It was caught because it broke five unrelated navigation-guard tests by covering the app; those tests passed again, untouched, once the behaviour was fixed. A background courtesy the operator never requested has not earned the right to interrupt them.

5. **One search per mount, and dismissal is scoped to the address.** The unreachable state persists across every poll, so re-sweeping on each would be a scan storm. A dismissal is remembered against the address it was dismissed for, so declining "the server at .99 is gone" does not gag the prompt when a *different* server later fails — that would be a separate problem going unreported.

## Consequences

Positive:
- A new install can find its own server; the operator never has to know an IP.
- A moved server is recovered from automatically, which is exactly the failure this project has already hit once.
- Verified end-to-end on the ADR 0026 stack: configured for a dead host at `192.168.50.99`, discovery derived `192.168.50.0/24` from that stale address, found the real server, and offered *"We found SignalK for **Pikorua** at **192.168.50.240:3000**"*. Accepting posted to `/api/signalk/discover` then `/api/settings` and nothing else, and the developer's real `settings.yaml` was untouched throughout.
- `HELMCENTRAL_DISCOVERY_SUBNET` covers topologies the two automatic sources miss.
- Discovery is reachable both automatically and on demand, so dismissing the prompt is not a dead end.

Tradeoffs:
- **The backend port-scans the operator's own LAN.** Bounded, RFC1918-only, and triggered only by an unconfigured or unreachable server — never a background loop. Worth stating plainly rather than burying: this is a scan, and on some networks scans get noticed.
- **Only a `/24` is searched.** A server on another subnet or VLAN still needs manual entry.
- **The `hint` depends on how the dashboard was opened.** At `localhost` with nothing configured there is no derivable network, and the operator gets the manual path.
- **The prompt shows the vessel name unqualified** ("Pikorua", not "M/V Pikorua"). The name comes from the discovered server; the `boat.vessel_prefix` is local configuration describing the operator's *own* boat, and pairing the two would mislabel a neighbour's vessel found on the same marina wifi.
- **mDNS remains unavailable** while the backend runs in a bridge container. If it ever runs natively, an mDNS source could sit behind this same endpoint alongside the sweep.

6. **"Find Servers" in Settings → SignalK is the on-demand counterpart**, for the operator who dismissed the prompt, whose network could not be derived, or who is simply setting up by hand. It differs from the background prompt in two deliberate ways:

   - **It reports every outcome**, including "no SignalK servers found on 192.168.50.0/24" and any backend error. The prompt's silence is justified by nobody having asked; here somebody did, and a button that does nothing visible reads as broken.
   - **It searches the network of the address currently typed in the form**, sent as the discovery `hint`, rather than whatever is persisted. The saved address is frequently stale, loopback, or on a dead subnet — often exactly why the operator is on this screen. Ignoring what they just typed would search the wrong network and report a confusing failure. (This was found in end-to-end testing: with the E2E seed at `127.0.0.1`, the first implementation derived nothing usable and returned "127.0.0.1 is not a private address" despite a perfectly good LAN address sitting in the field.)

   Picking a result **fills the form and does not save** — the opposite of the prompt's behaviour, and correct in both places. This page is a draft-then-save form with its own Save Settings button (ADR 0028); the prompt is a standalone confirmation where accepting *is* the deliberate act.

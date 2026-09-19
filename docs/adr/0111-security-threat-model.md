# ADR 0111: The security threat model is a trusted LAN

## Status

Accepted

Records the threat model used for the first security audit (2026-09-19) and
the findings deliberately not fixed under it. Complements ADR 0023
(encrypted secrets store) and ADR 0040 (SignalK-delegated authentication).

## Context

Helmcentral reached 2026-09 with roughly 113k lines of Go and 101k of
TypeScript and had never had a security pass. It now controls physical
equipment - autopilot, generator, CZone switching - holds credentials for
seven external services, accepts file uploads, and feeds untrusted text into
an LLM with tool calling.

Auditing that without a stated threat model produces a list nobody can act
on, because half of it argues about whether `auth.mode: none` is a bug. It
is not a bug; it is a decision, and it had never been written down. So the
audit fixed the model first and worked inside it.

The model is a **trusted LAN**: the boat network plus Tailscale, with no
untrusted devices. Anyone who can reach port 9091 is someone the operator
already trusts with the boat. Remote access is Tailscale's problem, not a
reverse proxy's.

What that makes uninteresting is network position. What it leaves very much
interesting is untrusted **data**, which arrives regardless of who is on the
network: SignalK deltas from any device on the bus including third-party
hardware nobody chose, uploaded PDFs, BOM marine warnings, OSM place names,
and anything a document can say to Mate.

That reframing is what made the audit useful. The seven high-severity
findings were almost all availability: a 543-byte PDF that killed the process
and then crash-looped, a single delta that caused a fatal stack overflow, one
`+Inf` that silently froze every screen's telemetry while the staleness
badges rode in the payload that had stopped being sent. A crash-looping
backend on a vessel is worse than most confidentiality failures, and none of
those needed an attacker on the network at all - a misbehaving sensor gets
there by accident.

## Decision

Accept, under this threat model:

- **`auth.mode: none` as the default.** All 160 routes, including autopilot,
  generator and the secrets admin API, answer without a session. The tier
  model exists and works when auth is enabled; it is simply not enforced by
  default.
- **No TLS and no reverse proxy.** Tailscale is the remote-access story.
- **The container runs as root, and the server binds 0.0.0.0.**

Fix, regardless of threat model: everything reachable from untrusted data.
That was 22 of the 23 findings, shipped across eight commits on
`fix/security-audit-phase1`.

## Consequences

Two findings get materially worse the moment this model stops holding, and
they are the trigger for revisiting this ADR:

- **E-1**: a configured transport destination receives the secret bound to
  it, so anyone who can edit settings can walk off with `NTFY_TOKEN`,
  `SMTP_PASSWORD`, `INFLUXDB_TOKEN` or the SignalK password. This is the one
  finding whose blast radius extends off the boat. **Fixed** - see the
  amendment below.
- **F-1**: dashboard embeds render in an iframe with `allow-scripts` and
  `allow-same-origin`. Same-origin URLs are now rejected by both validators,
  so the grant only ever applies to a genuinely foreign origin - but the
  whole control assumes whoever writes a dashboard page is trusted.

Revisit this ADR if guests join the boat network, if the box becomes
reachable off-boat by anything other than Tailscale, or if Helmcentral is
ever deployed somewhere with more than one operator.

The audit also left standing process, not just fixes: `govulncheck` gates CI,
because 23 reachable vulnerabilities had accumulated silently while nothing
scanned dependencies. `/security-review` reviews pending changes on a branch,
so it is a per-branch gate and cannot replace an audit like this one; it is
worth running on branches that touch input parsing, uploads, the assistant's
tool surface or the secrets store.

---

## Amendment, 2026-09-19: E-1 fixed

Pinning destinations (rejecting a settings save unless the new host was
already known) was the option considered and rejected: it breaks the
feature, since self-hosted ntfy, a personal SMTP relay, or an InfluxDB box
elsewhere are all ordinary things to point Helmcentral at, not attacks.
Requiring auth on these routes was also considered and rejected as a bigger
change to the auth model than this finding warrants on its own.

The fix shipped is Option B: clear the secret bound to a destination the
moment that destination changes. `setAlarmTransports`
(`backend/alarm_transports.go`) clears `NTFY_TOKEN` when `ntfy.server`
changes and `SMTP_PASSWORD` when `smtp.host` or `smtp.username` changes;
`updateSettingsHandler` (`backend/signalk.go`) clears `INFLUXDB_TOKEN` when
`influxdb.url` changes and both SignalK credentials when the SignalK address
or port changes. A repointed destination gets nothing until the credential
is re-entered - an attacker who repoints a host gets an empty token, and the
operator pays for it with one extra paste at exactly the moment they would
expect one.

Comparison is normalized (trimmed, trailing slash on a URL ignored, case
folded) before deciding whether a destination actually changed, so a
resubmitted form or a re-typed hostname in different case does not cost a
working credential for no reason. A clear that cannot complete - store
unavailable, or the underlying write fails - aborts the whole settings save
rather than persisting the new destination next to a secret that should
have gone with the old one; see `clearBoundSecret`'s doc comment
(`backend/secrets_store.go`) for the reasoning.

One subtlety worth recording: `SIGNALK_USERNAME`, `SIGNALK_PASSWORD` and
`INFLUXDB_TOKEN` are copied into the process environment once at boot
(`LoadIntoEnv`), because trusted host code reads them via
`getEnv`/`os.Getenv` rather than asking the store fresh each call. Deleting
the store row alone would leave that cached copy live in the running
process until its next restart, which is the leak this fix exists to close,
merely delayed. `clearBoundSecret` also `os.Unsetenv`s these three so the
clear takes effect immediately.

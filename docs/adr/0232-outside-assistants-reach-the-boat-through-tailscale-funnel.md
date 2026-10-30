# ADR 0232: Outside Assistants Reach The Boat Through Tailscale Funnel

## Status

Accepted (2026-10-30). Makes the tunnel decision that ADR 0231 deferred, and
extends ADR 0111's threat model (see the addendum there).

## Context

ADR 0231 made Helmcentral a read-only MCP server at `/mcp`, reachable on the
boat's LAN and over Tailscale. It left Claude's phone and web clients out and
said a public tunnel would get its own ADR. This is that ADR.

Anthropic's custom connectors connect "from Anthropic's cloud infrastructure,
rather than from your local device", "across every Claude client, including
claude.ai, Claude Desktop, Cowork, and the mobile apps", and a server "behind a
VPN" won't connect. A tailnet address is behind a VPN. So Tailscale alone, the
reach everything else in Helmcentral relies on, cannot carry these clients. A
server listed in `claude_desktop_config.json` is a separate, local mechanism and
already works. A connector can send a fixed bearer token through its "Request
headers" setting, with the "No sign in" option, which is exactly the credential
ADR 0231 built.
([Anthropic: custom connectors](https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp))

The operator already has Tailscale on the boat, and it offers a public address
without a second product to run. Tailscale Funnel routes public internet
traffic to a service on a tailnet device, for visitors who don't use Tailscale.
It carries TLS only, terminated by the Tailscale client on the device, on ports
443, 8443 and 10000. It needs MagicDNS, HTTPS certificates enabled for the
tailnet and the `funnel` node attribute in the tailnet policy file. Its
bandwidth limits can't be configured, it runs only where the Tailscale CLI runs,
and nothing restricts which visitors connect.
([Funnel](https://tailscale.com/kb/1223/funnel),
[CLI reference](https://tailscale.com/kb/1311/tailscale-funnel))

Two details of Tailscale's serve code (`ipn/ipnlocal/serve.go`,
[source](https://github.com/tailscale/tailscale/blob/main/ipn/ipnlocal/serve.go))
shape the decision:

- A proxy mount strips its mount point before forwarding (`http.StripPrefix`,
  "Trim the mount point from the URL path before proxying. (#6571)"). A request
  to exactly the mount point goes to the target URL's own path. So the correct
  command puts `/mcp` in the target too:
  `tailscale funnel --bg --https=8443 --set-path=/mcp http://127.0.0.1:<port>/mcp`.
- A path with no mount gets `http.NotFound`. With only `/mcp` mounted, `/api` and
  `/` are 404 from outside.
- Every request is forwarded with `Tailscale-Funnel-Request: ?1`, and Tailscale
  deletes any incoming value of that header first, so an outside client can
  neither forge nor strip it. Funnel also sets `X-Forwarded-For` to the public
  source address and `X-Forwarded-Proto: https`.
- Funnel is switched on per host and port (`AllowFunnel map[HostPort]bool`),
  not per path. Serve and Funnel share one set of mounts on a port. A boat that
  already uses `tailscale serve` on 443 to reach the dashboard over the tailnet
  would make every mount on 443 public, `/` included, by running Funnel on
  443. Pikorua's box is set up exactly that way.

Mounting only `/mcp` is a command the operator types. A mistyped one, such as a
Funnel on the whole port, would publish the dashboard and the `/api` routes on
the internet, where ADR 0111's model (a trusted LAN, `auth.mode: none` by
default) assumes nobody can reach them.

A chat bot on the box was considered as another route to the phone. Mate is
already the on-box assistant, reachable over Tailscale. A bot would replace the
Claude app rather than connect it, and would move the risk to whoever can
message the bot.

## Decision

### Funnel is the documented route, for `/mcp` only

The help pages tell the operator to run, on the machine that runs Helmcentral
or one that can reach it:

```
tailscale funnel --bg --https=8443 --set-path=/mcp http://127.0.0.1:<port>/mcp
```

Port 8443 keeps Funnel off 443, where an existing Serve setup for the
dashboard would otherwise turn public with it. The connector's address carries
`:8443`. The operator is told to check it with `tailscale funnel status`, a browser request to the root
(404), and a browser request to `/mcp` (the 401 message). Turning it off is the
same flags with `off`. The connector is added in Claude with the "No sign in"
option and an `Authorization: Bearer <token>` request header. The page tells the
operator to make a token for it alone, so it can be revoked alone.

### The token is the only gate on a public address

Nothing else on the path restricts who connects. The token is 256 random bits,
stored only as a hash, shown once and revocable on its own (ADR 0231). Guessing
one is not a realistic attack. The activity log, the tool list per token and the
`Origin` check from ADR 0231 are unchanged. A browser that sends an `Origin`
other than the app's own is still refused with 403.

Requests that fail authentication are still not written to the activity log, as
before. They never reach the tool layer, and logging them would let anyone on
the internet grow the table.

### A guard so a mistyped command cannot publish the rest

`backend/funnel_guard.go` is an Echo `Pre` middleware, so it runs before routing
and covers every path, the single-page app's fallback included. A request that
carries `Tailscale-Funnel-Request` and whose path is not exactly `/mcp` is
answered with a plain 404, the same body as Tailscale's own. That includes
`/mcp/`. The handler is mounted at `/mcp` alone, and an exact match leaves no
room for a prefix trick. The path compared is the one Echo routes on, which
is the raw, still-encoded path when a request has one. Comparing the decoded
path would let `/mc%70` past the guard and on to the single-page app's
fallback. It keys on the header being present, whatever its value.

A correct `/mcp` mount still forwards `/mcp/<anything>`, so refusals are
logged as something to check, not as proof of a mistyped command.

Requests without the header are not touched. They are the LAN, Tailscale and
local clients. A request with the header and the path `/mcp` goes on to the
normal token check.

Refusals are logged with the method, path and `X-Forwarded-For`, at most once a
minute, with a count of how many were left out since the last line. A
misconfigured Funnel is therefore visible without a scanner flooding the log,
and no refusal is silent.

The guard cannot protect against a Funnel that Tailscale does not mark, or
against a reverse proxy that is not Funnel. It is defence in depth for one
named mistake, not a general firewall.

## Alternatives rejected

- **Exposing the whole port.** The dashboard and `/api` would be on the
  internet. Funnel mounts a path for this reason.
- **A reverse proxy the operator runs and certifies.** More software on the boat
  and a certificate to renew, to get what Tailscale already provides.
- **OAuth now.** Claude's connectors accept a fixed bearer header. OAuth would
  add an authorisation server for one operator. It can come later if a client
  needs it.
- **An IP allowlist of Anthropic's ranges.** Funnel cannot filter by source, and
  the ranges change.
- **Rate limiting bad tokens.** Not added. A token is 256 random bits, and an
  unauthenticated request costs one hash and one indexed lookup. A limiter would
  be state to get wrong for a threat that is not realistic. If Funnel traffic
  ever proves heavy, the bandwidth limits and revoking the token are the levers.

## Consequences

- The address is public. It can be found, since `*.ts.net` names appear in
  certificate transparency logs, and the token is the only thing between a
  finder and the boat's status. If the address or the token leaks, revoke the
  token. Making one per client keeps that cheap.
- Funnel's bandwidth limits apply and can't be changed. MCP traffic is small.
- It works only where the Tailscale CLI runs. Whether the box's install offers
  the CLI and Funnel is checked on the boat, not assumed here.
- Which of Claude's clients send an `Origin` header, or probe
  `/.well-known/oauth-protected-resource`, is not settled here. An `Origin` would
  be refused with 403. The guard answers 404 to the probe, as Funnel does with
  no mount, which an OAuth-optional client is expected to treat as "no OAuth".
  Both are checked against a real connector before this is relied on.
- A Funnel left running with no tokens exposes nothing but 401s.
- ADR 0111's model gains a second reach. The LAN and Tailscale are still
  trusted. One path, `/mcp`, is also public, behind a token.

# ADR 0233: Outside Assistants Can Sign In With OAuth

## Status

Accepted (2026-10-30). Reverses ADR 0232's "OAuth now" rejection, now that a
client needs it. Leaves ADR 0231's token model and ADR 0232's Funnel route in
place.

## Context

ADR 0231 gave each outside assistant a bearer token, `hcmcp_…`, sent in the
`Authorization` header. Claude Code, Claude Desktop's config file and Claude's
custom connectors all send it, the last through their "Request headers"
setting. ADR 0232 rejected OAuth because no client needed it.

ChatGPT does. Its custom MCP servers, under developer mode, take OAuth or no
authentication. OpenAI's documentation says ChatGPT supports neither
machine-to-machine grants nor custom API keys, and it has no setting for a
request header. Once OAuth completes, ChatGPT sends the access token it got as
`Authorization: Bearer <token>`, the header `/mcp` already reads.
([OpenAI: authentication](https://developers.openai.com/plugins/build/auth))

What ChatGPT expects of the server:

- Protected resource metadata (RFC 9728) at
  `/.well-known/oauth-protected-resource`, or the address given in the
  `resource_metadata` parameter of the 401's `WWW-Authenticate` header.
- Authorization server metadata (RFC 8414) whose `issuer` matches the resource
  metadata's `authorization_servers` entry, with `S256` in
  `code_challenge_methods_supported`.
- The authorization-code flow with PKCE (S256), and the `resource` parameter in
  both requests.
- Client registration by a client ID metadata document (CIMD, preferred),
  dynamic client registration (DCR, RFC 7591) or a predefined client.
- With `authorization_response_iss_parameter_supported` (RFC 9207), one stable
  redirect URI, `https://chatgpt.com/connector_platform_oauth_redirect`.

Two facts of the boat shape the design. The sign-in page has to be public,
because ChatGPT's browser tab reaches it through the same Funnel as `/mcp`. And
ADR 0111's default is `auth.mode: none`: most boats have no login that could
stand behind an "Approve" button.

Tailscale's Serve and Funnel proxy keeps the visitor's `Host` header
(`r.Out.Host = r.In.Host` in `ipn/ipnlocal/serve.go`) and sets
`X-Forwarded-Proto: https`, so the server can name its own public address from
the request.

## Decision

### The access token is the outside-assistant token the operator pastes

The sign-in page asks for an existing outside-assistant token. Pasting a valid
one and selecting **Allow** is the approval, and nothing else approves. The
authorization code carries that token, and the token endpoint hands it back as
the access token. No refresh token, no expiry.

So OAuth adds no credential. `/mcp` authenticates exactly as before. The
token's tool list, name and activity log apply to the app that signed in with
it. Revoking the token in Settings cuts the app off on its next request.
Everything ADR 0231 says about a token holds: 256 random bits, stored only as a
hash, one per client, revocable alone.

The token is held in memory, inside the pending code, for at most five
minutes. That is the only time Helmcentral holds a secret in the clear after
showing it.

Short-lived access tokens with refresh tokens were considered. They protect a
token that leaks from the client, but ChatGPT would hold the refresh token,
which is the same exposure with more state to get wrong. They would also make
revocation a second table to keep in step with the first.

### Dynamic client registration, not CIMD

CIMD would have Helmcentral fetch a URL the client names, from a public
endpoint: a server-side request forgery surface onto the boat's network, for a
gain of one fewer table. DCR needs no outbound request. Registrations are
public clients only (`token_endpoint_auth_method: none`). Redirect URIs must be
HTTPS, or HTTP to a loopback host for a native client. A redirect URI must
match a registered one exactly, and an unknown client or redirect gets an
error page, never a redirect.

Anyone on the internet can register, so registrations are capped at 50.
Registering at the cap removes the oldest client that can sign in to nothing:
one whose token has been revoked, or one that never completed a sign-in and is
more than 15 minutes old. A client that has not signed in yet is kept for its
first 15 minutes, so a flood of registrations can't push out the one the
operator is in the middle of signing in with. Clients holding a live token are
kept, and if every slot is a live client or a new one, registration is refused
and logged. A scanner can't push out a working connector, the table can't grow
past the cap, and re-adding apps over the years can't fill it for good.

### Endpoints

- `GET /.well-known/oauth-protected-resource` and
  `GET /.well-known/oauth-protected-resource/mcp`: the resource is
  `<base>/mcp` and the authorization server is `<base>`.
- `GET /.well-known/oauth-authorization-server`: the issuer is `<base>`, with
  S256 only, `authorization_code` only, `none` only, and
  `authorization_response_iss_parameter_supported: true`. It does not advertise
  CIMD.
- `POST /mcp/oauth/register`, `GET`/`POST /mcp/oauth/authorize`,
  `POST /mcp/oauth/token`.
- `/mcp`'s 401 adds `resource_metadata="<base>/.well-known/oauth-protected-resource/mcp"`
  to `WWW-Authenticate`.

`<base>` is `https://` when the request came over TLS or with
`X-Forwarded-Proto: https`, and `http://` otherwise, followed by the request's
`Host`. A forged `Host` only changes the answer its own sender gets. The
`resource` parameter, where sent, must equal `<base>/mcp`, and the code is
bound to it.

The OAuth endpoints sit under `/mcp/` so the existing Funnel mount already
carries them. The three metadata documents can't: RFC 9728 and RFC 8414 put
them at the root. Funnel therefore needs one more mount, `/.well-known`. The
help pages add it to both of ADR 0232's routes. A Claude connector that sends
the header works with or without that mount.

### The sign-in page

It is a server-rendered page, not part of the single-page app, which Funnel
does not carry. It is built with `html/template`, has no external assets, and
is sent with `frame-ancestors 'none'`, `X-Frame-Options: DENY`,
`Referrer-Policy: no-referrer` and `Cache-Control: no-store`. It names the
client and the redirect URI's host. It tells the operator to make a token named
after the app, and to go on only if they started the sign-in from that app.
A completed sign-in is written to the activity log under the token's name.
The entry records the redirect URI's host beside the name the client gave
itself, as `sign-in from ChatGPT (chatgpt.com)`, because the name is
self-declared and the host is where the sign-in actually went.

Any fault found before the operator answers gets the error page with status
400, never a redirect. That covers a wrong `response_type`, a missing or
malformed PKCE challenge, a method other than S256, a wrong `resource`, and a
POST with no valid `action`. Registration is open, so any redirect URI on file
may belong to a stranger, and a redirect on a fault would make the boat's
public address an open redirector (RFC 9700 section 4.11.2). Only the
operator's own choice redirects: **Cancel** sends `access_denied` with `state`
and `iss`, and **Allow** sends the code. An empty `resource` counts as none, and
the form carries back only the fields the request had.

A client's name is shown on the page and in the activity log, so registration
rejects control characters and format characters such as the right-to-left
override and the zero-width space, which could make one app's name read as
another's.

### The Funnel guard allows the new paths exactly

Through Funnel, the guard now allows `/mcp`, the three OAuth paths and the
three metadata paths. Each is an exact match on the raw path, as before, so a
prefix trick or an encoded character still gets 404.

## Alternatives rejected

- **Token in the URL** (`/mcp?token=…`). This works with any client that takes
  an address, but the token would sit where URLs get copied, logged and
  screenshotted.
- **No authentication** for ChatGPT. The boat's status would be public.
- **A consent button backed by the dashboard login.** Most boats have none, and
  a public page with an "Approve" button and no credential approves anyone.
- **Issuing a fresh token per sign-in.** One token per app is already the
  model, and a sign-in that minted tokens would leave revoked apps' tokens to
  find and clean up.
- **CIMD.** See above.

## Consequences

- ChatGPT, and any client that speaks MCP's authorization flow, can connect
  with the token as its only credential, the same as the header route.
- A phishing link to the sign-in page gets nothing without a token, and the
  page names where the code will go. An operator who pastes a token into
  someone else's sign-in has handed over that token. The page says to go on
  only from the app being connected.
- Claude's "No sign in" connectors now see `resource_metadata` on the 401 from
  their connection check. If a Claude client starts the OAuth flow because of
  it, the operator pastes the token there too. This hasn't been tested yet.
- Pending codes live in memory, so a restart mid-sign-in means signing in
  again. Registrations are in `helmcentral.sqlite`.
- The access token never expires. Revoking the token is the way to end access,
  as it is for every other outside assistant.

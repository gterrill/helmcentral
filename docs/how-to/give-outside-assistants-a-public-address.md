# Give outside assistants a public address

Claude on your phone or the web, and ChatGPT, connect from their companies'
servers, not from your device. They can't reach an address that only works on
your boat's network or over Tailscale. [Outside
assistants](../features/outside-assistants.md) explains what the assistant can
see.

The fix is Tailscale Funnel. It gives one address on the internet that leads to
Helmcentral, with a proper HTTPS certificate. Funnel makes that address public,
so anyone who knows it can try it. Two things limit what they can do. Funnel
only passes on `/mcp` and `/.well-known`, and Helmcentral refuses anything else
that arrives through Funnel. `/mcp` itself refuses every request that doesn't
carry a valid token. The dashboard and settings stay off the internet.

Two paths are passed on because two kinds of app connect:

- **`/mcp`:** The outside assistant itself. Every app needs this one.
- **`/.well-known`:** A few short notes that tell an app where to sign in. Only
  apps that sign in, such as ChatGPT, read them. Claude's connector with its
  header doesn't, but it keeps working with the extra path switched on.

If you only use Claude Code, or Claude Desktop with its config file, you don't
need this page. Follow [Connect an outside
assistant](connect-an-outside-assistant.md) instead.

You need to be signed in to Helmcentral as an admin, and the boat's computer
needs to be on your tailnet with the Tailscale command line installed. When
Funnel is working, follow [Connect Claude on your phone or the
web](connect-claude-on-the-phone-or-web.md) or [Connect
ChatGPT](connect-chatgpt.md).

## Allow Funnel on your tailnet

Funnel is off until you allow it. In the Tailscale admin console, turn on
MagicDNS and HTTPS certificates for the tailnet, and give the machine that runs
Helmcentral the `funnel` attribute in the tailnet policy file. Tailscale's page
on [Funnel](https://tailscale.com/kb/1223/funnel) walks through each of these,
and shows the current screens.

## Turn Funnel on for /mcp and /.well-known

Both companies' servers only connect to an address on the standard HTTPS port,
443. Funnel also offers ports 8443 and 10000, but they can't reach those. Claude
then fails with "Couldn't reach this address".

Funnel makes a whole port public, not one path. So first check what the machine
already has on port 443. Run `tailscale serve status` on the machine that will
run Funnel:

- **Nothing on port 443:** Funnel can use the machine's own port 443. Follow
  [Funnel on this machine](#funnel-on-this-machine).
- **Something already on port 443:** this is usually Helmcentral's dashboard,
  opened over Tailscale with `tailscale serve`. Funnel on that port would put
  the dashboard on the internet too. Follow [Funnel on a machine of its
  own](#funnel-on-a-machine-of-its-own) instead, so the dashboard keeps its
  address and stays on your tailnet.

### Funnel on this machine

1. On the machine that runs Helmcentral, or any machine on your tailnet that can
   reach it, run these two commands. Replace `<port>` with the port you open
   Helmcentral on:

   ```
   tailscale funnel --bg --set-path=/mcp http://127.0.0.1:<port>/mcp
   tailscale funnel --bg --set-path=/.well-known http://127.0.0.1:<port>/.well-known
   ```

   If Helmcentral runs on a different machine, use that machine's Tailscale
   address instead of `127.0.0.1`. The path at the end of each address is
   needed. Tailscale removes the first `/mcp` or `/.well-known` before it passes
   the request on, so the second one puts it back.

2. Run `tailscale funnel status`. It should show an address like
   `https://<machine>.<tailnet>.ts.net (Funnel on)` with only `/mcp` and
   `/.well-known` listed under it. This is the check that matters. If it lists
   `/` or anything else, turn Funnel off (see below) and run the commands again.
   Through Funnel, Helmcentral answers only `/mcp`, its sign-in addresses under
   `/mcp/oauth/`, and the three sign-in notes under `/.well-known/`. Everything
   else gets "404 page not found", including other addresses under `/mcp/` and
   `/.well-known/`. A browser shows that 404 either way, so it can't tell you
   which paths Funnel is passing on.

### Funnel on a machine of its own

This runs a second, small Tailscale machine in Docker, on the same computer as
Helmcentral. It joins your tailnet under its own name, such as
`helmcentral-mcp`, and its port 443 passes on `/mcp` and `/.well-known`, and
nothing else. Your first Tailscale machine and the dashboard's address don't
change.

1. Make a folder for it, for example `helmcentral-mcp-tailscale`, with a folder
   called `state` inside it.
2. In that folder, make a file called `serve.json`. Replace `<port>` with the
   port you open Helmcentral on:

   ```json
   {
     "TCP": { "443": { "HTTPS": true } },
     "Web": {
       "${TS_CERT_DOMAIN}:443": {
         "Handlers": {
           "/mcp": { "Proxy": "http://host.docker.internal:<port>/mcp" },
           "/.well-known/": { "Proxy": "http://host.docker.internal:<port>/.well-known/" }
         }
       }
     },
     "AllowFunnel": { "${TS_CERT_DOMAIN}:443": true }
   }
   ```

   Leave `${TS_CERT_DOMAIN}` as it is. Tailscale fills in the machine's name
   when it starts.

3. In the same folder, make a file called `docker-compose.yml`:

   ```yaml
   name: helmcentral-mcp-tailscale
   services:
     tailscale:
       image: tailscale/tailscale:stable
       container_name: helmcentral-mcp-tailscale
       hostname: helmcentral-mcp
       environment:
         TS_HOSTNAME: helmcentral-mcp
         TS_STATE_DIR: /var/lib/tailscale
         TS_USERSPACE: "true"
         TS_ACCEPT_DNS: "false"
         TS_SERVE_CONFIG: /config/serve.json
       volumes:
         - ./state:/var/lib/tailscale
         - ./serve.json:/config/serve.json:ro
       extra_hosts:
         - host.docker.internal:host-gateway
       restart: unless-stopped
   ```

4. In that folder, run `docker compose up -d`. Then run
   `docker logs helmcentral-mcp-tailscale` and open the
   `https://login.tailscale.com/...` link it shows, to add the machine to your
   tailnet.
5. In the Tailscale admin console, find `helmcentral-mcp` and turn off key
   expiry for it. Otherwise it drops off your tailnet after a few months and the
   assistants can't connect. If your tailnet policy gives the `funnel` attribute
   to named machines rather than to everyone, add this one.
6. Run `docker exec helmcentral-mcp-tailscale tailscale funnel status`. It
   should show `https://helmcentral-mcp.<tailnet>.ts.net (Funnel on)` with only
   `/mcp` and `/.well-known/` listed under it.

If you set this machine up before ChatGPT was supported, add the
`/.well-known/` handler to your `serve.json`, then run `docker compose restart`
in its folder.

### Check it from a browser

Open `https://<machine>.<tailnet>.ts.net/mcp` in a browser, using the machine
that runs Funnel. You should see a short message saying that a valid outside
assistant token is required. The first request can take a few seconds while
Tailscale gets the machine's certificate.

Then open
`https://<machine>.<tailnet>.ts.net/.well-known/oauth-protected-resource`. You
should see a short JSON document that names the same address. If you see "404
page not found", the `/.well-known` path isn't being passed on.

## Turn Funnel off

- **Funnel on this machine:** run the same commands with `off` on the end, then
  run `tailscale funnel status` to check that nothing says "Funnel on":

  ```
  tailscale funnel --set-path=/mcp off
  tailscale funnel --set-path=/.well-known off
  ```

- **Funnel on a machine of its own:** in its folder, run `docker compose down`.
  To remove it for good, also delete it from the Tailscale admin console.

To cut off one assistant and leave Funnel running, revoke its token in
**Settings, Outside assistants**. It stops working from its next request.

## If it doesn't work

- **The command says Funnel isn't allowed:** The tailnet doesn't have Funnel
  turned on for this machine yet. Go back to the Tailscale admin console.
- **An assistant says it couldn't reach this address:** Check that the address
  has no port number in it, such as `:8443`. They only connect on port 443. If
  it has none, open the address in a browser on your phone, with Wi-Fi off. If
  that fails, the problem is Funnel or DNS, not Helmcentral. Funnel can take a
  few minutes to start working the first time.
- **The `/.well-known` address shows "404 page not found":** Funnel isn't
  passing that path on. Run `tailscale funnel status` and check that
  `/.well-known` is listed, then add it if it isn't.

## Things to know

- **Funnel has bandwidth limits.** Tailscale sets them and you can't change
  them. Reading the boat's status is tiny, so you won't notice.
- **The address is public.** The token is the only thing that stops a stranger
  who finds the address. If the address or a token leaks, revoke the token and
  make another.
- **Funnel can't filter who connects.** Nobody can restrict it to Anthropic's or
  OpenAI's servers.

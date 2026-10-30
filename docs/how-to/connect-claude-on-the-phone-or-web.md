# Connect Claude on your phone or the web

This lets Claude on your phone, on claude.ai, or a connector added in Claude
Desktop read the boat. Those connect from Anthropic's servers, not from your
device, so they can't reach an address that only works on your boat's network or
over Tailscale. [Outside assistants](../features/outside-assistants.md) explains
what the assistant can see.

The fix is Tailscale Funnel. It gives one address on the internet that leads
to Helmcentral, with a proper HTTPS certificate. Funnel makes that address
public, so anyone who knows it can try it. Two things limit what they can do.
Funnel only passes on `/mcp`, and Helmcentral refuses anything else that
arrives through Funnel. `/mcp` itself refuses every request that doesn't carry
a valid token. The dashboard and settings stay off the internet.

If you only use Claude Code, or Claude Desktop with its config file, you don't
need this page. Follow [Connect an outside
assistant](connect-an-outside-assistant.md) instead.

You need to be signed in to Helmcentral as an admin, and the boat's computer
needs to be on your tailnet with the Tailscale command line installed.

## Allow Funnel on your tailnet

Funnel is off until you allow it. In the Tailscale admin console, turn on
MagicDNS and HTTPS certificates for the tailnet, and give the machine that runs
Helmcentral the `funnel` attribute in the tailnet policy file. Tailscale's page
on [Funnel](https://tailscale.com/kb/1223/funnel) walks through each of these,
and shows the current screens.

## Make a token for it

Give the connector a token of its own, so you can revoke it without touching
your other programs.

1. Open **Settings** and choose **Outside assistants**.
2. Under **Name**, type "Claude phone and web", and select **Create token**.
3. Copy the token from the dialog. Helmcentral can't show it again.

## Turn Funnel on for /mcp

Claude's servers only connect to an address on the standard HTTPS port, 443.
Funnel also offers ports 8443 and 10000, but Claude can't reach them, so the
connector fails with "Couldn't reach this address".

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
   reach it, run this command. Replace `<port>` with the port you open
   Helmcentral on:

   ```
   tailscale funnel --bg --set-path=/mcp http://127.0.0.1:<port>/mcp
   ```

   If Helmcentral runs on a different machine, use that machine's Tailscale
   address instead of `127.0.0.1`. The `/mcp` at the end of the address is
   needed. Tailscale removes the first `/mcp` before it passes the request on,
   so the second one puts it back.

2. Run `tailscale funnel status`. It should show an address like
   `https://<machine>.<tailnet>.ts.net (Funnel on)` with only `/mcp` listed
   under it. This is the check that matters. If it lists `/` or anything
   besides `/mcp`, turn Funnel off (see below) and run the command again.
   Helmcentral refuses everything but `/mcp` that comes through Funnel, so a
   browser shows "404 page not found" either way and can't tell you this.

### Funnel on a machine of its own

This runs a second, small Tailscale machine in Docker, on the same computer as
Helmcentral. It joins your tailnet under its own name, such as
`helmcentral-mcp`, and its port 443 passes on `/mcp` and nothing else. Your
first Tailscale machine and the dashboard's address don't change.

1. Make a folder for it, for example `helmcentral-mcp-tailscale`, with a folder
   called `state` inside it.
2. In that folder, make a file called `serve.json`. Replace `<port>` with the
   port you open Helmcentral on:

   ```json
   {
     "TCP": { "443": { "HTTPS": true } },
     "Web": {
       "${TS_CERT_DOMAIN}:443": {
         "Handlers": { "/mcp": { "Proxy": "http://host.docker.internal:<port>/mcp" } }
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
   expiry for it. Otherwise it drops off your tailnet after a few months and
   Claude can't connect. If your tailnet policy gives the `funnel` attribute to
   named machines rather than to everyone, add this one.
6. Run `docker exec helmcentral-mcp-tailscale tailscale funnel status`. It
   should show `https://helmcentral-mcp.<tailnet>.ts.net (Funnel on)` with only
   `/mcp` listed under it.

### Check it from a browser

Open `https://<machine>.<tailnet>.ts.net/mcp` in a browser, using the machine
that runs Funnel. You should see a short message saying that a valid outside
assistant token is required. The first request can take a few seconds while
Tailscale gets the machine's certificate.

## Add the connector in Claude

1. In Claude, open **Settings** and then **Connectors**, and choose to add a
   custom connector. Anthropic's page on [custom
   connectors](https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp)
   shows where this is on each app.
2. For the address, type `https://<machine>.<tailnet>.ts.net/mcp`, using the
   machine that runs Funnel. Don't add a port number.
3. Under authentication, choose **No sign in**.
4. In the **Request headers** setting, add a header named `Authorization` with
   the value `Bearer ` followed by the token, for example `Bearer hcmcp_...`.
5. Save it, then ask Claude about the boat, for example "How's the house bank?"

A connector you add on the web shows up in the phone apps too.

## Turn Funnel off

- **Funnel on this machine:** run the same command with `off` on the end, then
  run `tailscale funnel status` to check that nothing says "Funnel on":

  ```
  tailscale funnel --set-path=/mcp off
  ```

- **Funnel on a machine of its own:** in its folder, run `docker compose down`.
  To remove it for good, also delete it from the Tailscale admin console.

To cut off only the Claude connector and leave Funnel running, revoke its token
in **Settings, Outside assistants**. It stops working from its next request.

## If it doesn't connect

- **The command says Funnel isn't allowed:** The tailnet doesn't have Funnel
  turned on for this machine yet. Go back to the Tailscale admin console.
- **Claude says it couldn't reach this address:** Check that the address has no
  port number in it, such as `:8443`. Claude only connects on port 443. If it
  has none, open the address in a browser on your phone, with Wi-Fi off. If
  that fails, the problem is Funnel or DNS, not Helmcentral. Funnel can take a
  few minutes to start working the first time.
- **Claude says unauthorized:** The token is wrong or revoked. Make a new one.
- **Claude says forbidden:** The app sent a web page's address with its
  request, and Helmcentral refuses those. Try another Claude app, or Claude Code
  on a computer.
- **Nothing shows in the activity list:** Helmcentral only records requests
  that carry a valid token. If the list stays empty, check the header you typed
  in Claude.

## Things to know

- **Funnel has bandwidth limits.** Tailscale sets them and you can't change
  them. Reading the boat's status is tiny, so you won't notice.
- **The address is public.** The token is the only thing that stops a stranger
  who finds the address. If the address or the token leaks, revoke the token and
  make another.
- **Funnel can't filter who connects.** Nobody can restrict it to Anthropic's
  servers.

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

1. On the machine that runs Helmcentral, or any machine on your tailnet that can
   reach it, run this command. Replace `<port>` with the port you open
   Helmcentral on:

   ```
   tailscale funnel --bg --https=8443 --set-path=/mcp http://127.0.0.1:<port>/mcp
   ```

   Keep the `--https=8443`. Funnel makes a whole port public, not one path. If
   you already use `tailscale serve` to open Helmcentral over Tailscale, that
   is on port 443, and Funnel on 443 would put the dashboard on the internet
   too. Port 8443 keeps the two apart.

   If Helmcentral runs on a different machine, use that machine's Tailscale
   address instead of `127.0.0.1`. The `/mcp` at the end of the address is
   needed. Tailscale removes the first `/mcp` before it passes the request on,
   so the second one puts it back.

2. Run `tailscale funnel status`. It should show an address like
   `https://<machine>.<tailnet>.ts.net:8443 (Funnel on)` with only `/mcp`
   listed under it. Anything on port 443 should still say "tailnet only". This
   is the check that matters. If it lists `/` or anything besides `/mcp`, turn
   Funnel off (see below) and run the command again. Helmcentral refuses
   everything but `/mcp` that comes through Funnel, so a browser shows "404
   page not found" either way and can't tell you this.
3. Open `https://<machine>.<tailnet>.ts.net:8443/mcp` in a browser. You should see a
   short message saying that a valid outside assistant token is required.

## Add the connector in Claude

1. In Claude, open **Settings** and then **Connectors**, and choose to add a
   custom connector. Anthropic's page on [custom
   connectors](https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp)
   shows where this is on each app.
2. For the address, type `https://<machine>.<tailnet>.ts.net:8443/mcp`.
3. Under authentication, choose **No sign in**.
4. In the **Request headers** setting, add a header named `Authorization` with
   the value `Bearer ` followed by the token, for example `Bearer hcmcp_...`.
5. Save it, then ask Claude about the boat, for example "How's the house bank?"

A connector you add on the web shows up in the phone apps too.

## Turn Funnel off

1. On the same machine, run the same command with `off` on the end:

   ```
   tailscale funnel --https=8443 --set-path=/mcp off
   ```

2. Run `tailscale funnel status` to check that nothing says "Funnel on".

To cut off only the Claude connector and leave Funnel running, revoke its token
in **Settings, Outside assistants**. It stops working from its next request.

## If it doesn't connect

- **The command says Funnel isn't allowed:** The tailnet doesn't have Funnel
  turned on for this machine yet. Go back to the Tailscale admin console.
- **Claude says it can't reach the server:** Open the address in a browser on
  your phone, with Wi-Fi off. If that fails, the problem is Funnel or DNS, not
  Helmcentral. Funnel can take a few minutes to start working the first time.
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

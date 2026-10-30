# Connect Claude on your phone or the web

This lets Claude on your phone, on claude.ai, or a connector added in Claude
Desktop read the boat. Those connect from Anthropic's servers, not from your
device, so they need a public address for Helmcentral. [Outside
assistants](../features/outside-assistants.md) explains what the assistant can
see.

If you only use Claude Code, or Claude Desktop with its config file, you don't
need this page. Follow [Connect an outside
assistant](connect-an-outside-assistant.md) instead. For ChatGPT, follow
[Connect ChatGPT](connect-chatgpt.md).

You need to be signed in to Helmcentral as an admin. You also need Tailscale
Funnel working, as [Give outside assistants a public
address](give-outside-assistants-a-public-address.md) describes. Claude's
connector only needs the `/mcp` path passed on, so the `/.well-known` one is
optional here.

## Make a token for it

Give the connector a token of its own, so you can revoke it without touching
your other programs.

1. Open **Settings** and choose **Outside assistants**.
2. Under **Name**, type "Claude phone and web", and select **Create token**.
3. Copy the token from the dialog. Helmcentral can't show it again.

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

## Stop it

To cut off the Claude connector and leave Funnel running, revoke its token in
**Settings, Outside assistants**. It stops working from its next request. To
close the public address as well, see [Turn Funnel
off](give-outside-assistants-a-public-address.md#turn-funnel-off).

## If it doesn't connect

- **Claude says it couldn't reach this address:** Check that the address has no
  port number in it, such as `:8443`. Claude only connects on port 443. If it
  has none, follow the checks in [Give outside assistants a public
  address](give-outside-assistants-a-public-address.md#if-it-doesnt-work).
- **Claude says unauthorized:** The token is wrong or revoked. Make a new one.
- **Claude says forbidden:** The app sent a web page's address with its
  request, and Helmcentral refuses those. Try another Claude app, or Claude Code
  on a computer.
- **Nothing shows in the activity list:** Helmcentral only records requests
  that carry a valid token. If the list stays empty, check the header you typed
  in Claude.

## Things to know

- **The address is public.** The token is the only thing that stops a stranger
  who finds the address. If the address or the token leaks, revoke the token and
  make another.

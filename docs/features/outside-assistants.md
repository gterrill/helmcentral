# Outside assistants

Outside assistants let an AI program read the boat. You can ask Claude Code on a
laptop, Claude on your phone, or ChatGPT how the house bank is doing without
opening Helmcentral. They use MCP, the standard way AI programs connect
to a source of information. Helmcentral acts as the MCP server, and the program
you connect is the client.

It can only read. Nothing an outside assistant does can change an alarm, a
setting, a record or the boat. It also doesn't use Mate or your OpenRouter key,
so it works when Mate is switched off.

## What you get

- **Answers from the same figures Mate uses:** The assistant gets the vessel
  status, the active alarms and the alarm log. It also gets how a reading has
  behaved over time, your passages and cruise totals, the maintenance schedule
  and log, the equipment list and the boat's particulars. Figures come in your
  units. A reading the boat can't give is marked as missing instead of guessed.
- **A description of your boat:** The assistant is also given a boat context.
  Helmcentral writes this page fresh every time. It draws on your equipment and
  your roles, such as which battery is the house bank and which engines you
  have. It also covers your units, your alarm thresholds, your tank levels and
  the boat's time zone. Because of it, "how's the house bank" gets the right
  battery without you explaining your boat.
- **One token for each program:** Every program you connect has its own token,
  made in **Settings, Outside assistants**. You see the token once, when you
  make it. You can switch each tool on or off for one token, and you can revoke
  one token without touching the others.
- **A token pasted in, for apps that can't send one:** Claude's connector sends
  its token in a header. ChatGPT can't send a fixed header, so it signs in
  instead. It opens a Helmcentral page, you paste a token into it and select
  **Allow**, and ChatGPT uses that token from then on. It's the same kind of
  token, so you revoke it the same way.
- **A record of what was asked:** The same section lists what each program
  asked for, when, and whether it worked. Helmcentral keeps 30 days.

## Which tools a new token starts with

A new token can use these:

- Vessel status, active alarms and the alarm log.
- Path history, and the last time a reading was recorded.
- Passages and the cruise summary.
- Maintenance, and the equipment list.
- The boat's particulars and the boat context.

Live SignalK paths and the list of alarm thresholds are there too, but they
start switched off.

## What it never offers

Some things are never offered, whatever you switch on:

- **Mate's conversations:** Your chats with Mate stay with Mate.
- **Anything that changes the boat:** There are no tools that write, propose or
  apply changes.
- **Anything that spends your OpenRouter key:** Web search and document search
  are not offered.
- **Your contact details:** The boat's particulars leave out your name, phone,
  email, insurer, policy number and storm delegate.

## Where it stops

- **Your network, unless you open one address:** An outside assistant connects
  to Helmcentral over your boat's network or over Tailscale. Helmcentral isn't
  reachable from the internet unless you turn on Tailscale Funnel for it.
- **Phone and web Claude, and ChatGPT, need Funnel:** Claude on the phone, on
  the web and the Claude Desktop connectors page all connect from Anthropic's
  servers. ChatGPT connects from OpenAI's. They can't reach an address that only
  works on your network. Tailscale Funnel gives `/mcp`, and the `/.well-known`
  notes that apps which sign in read, a public address. The token is then the
  only thing that stops a stranger who finds it. Helmcentral refuses everything
  else that arrives through Funnel. Claude Code and Claude Desktop with its
  config file connect from your own computer. They don't need Funnel.
- **No token, no access:** There is no switch to turn this on. Until you make a
  token, every request is refused. A token that is revoked stops working at once.
- **Not everything Mate can do:** Forecasts, tides, places, routing, passage
  estimates and nearby vessels are Mate's, because they need Mate's own
  instructions to read correctly. They aren't offered yet.

To connect a program on your computer, follow [Connect an outside
assistant](../how-to/connect-an-outside-assistant.md). For Claude on your phone
or the web, or for ChatGPT, first follow [Give outside assistants a public
address](../how-to/give-outside-assistants-a-public-address.md). Then follow
[Connect Claude on your phone or the
web](../how-to/connect-claude-on-the-phone-or-web.md) or [Connect
ChatGPT](../how-to/connect-chatgpt.md).

# Outside assistants

Outside assistants let a program on your own network read the boat, so you can
ask Claude Code on a laptop how the house bank is doing without opening
Helmcentral. They use MCP, the standard way AI programs connect to a source of
information. Helmcentral acts as the MCP server, and the program you connect is
the client.

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

- **Only on your network:** An outside assistant connects to Helmcentral over
  your boat's network or over Tailscale. It doesn't make Helmcentral reachable
  from the internet.
- **Phone and web Claude can't connect yet:** Claude on the phone, on the web and
  the Claude Desktop connectors page all connect from Anthropic's servers. They
  would need Helmcentral to be open to the internet, so they don't work.
  Claude Code and Claude Desktop with its config file do work, because they
  connect from your own computer.
- **No token, no access:** There is no switch to turn this on. Until you make a
  token, every request is refused. A token that is revoked stops working at once.
- **Not everything Mate can do:** Forecasts, tides, places, routing, passage
  estimates and nearby vessels are Mate's, because they need Mate's own
  instructions to read correctly. They aren't offered yet.

To connect a program, follow [Connect an outside
assistant](../how-to/connect-an-outside-assistant.md).

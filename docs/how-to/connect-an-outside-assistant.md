# Connect an outside assistant

This connects a program on your computer, such as Claude Code or Claude Desktop,
so it can read the boat. It can only read, and it only works while the computer
is on the boat's network or on Tailscale. [Outside
assistants](../features/outside-assistants.md) explains what it can see.

You need to be signed in as an admin, and Helmcentral needs to be running on a
network the computer can reach.

## Make a token

1. Open **Settings** and choose **Outside assistants**.
2. Under **Name**, type what the token is for, such as "Laptop", and select
   **Create token**.
3. Copy what the dialog shows now. Helmcentral doesn't keep the token itself, so
   it can't show it again. If you lose it, revoke it and make another.

The dialog has the token, a command for Claude Code and a snippet for Claude
Desktop. They already contain the address you opened Helmcentral on and the new
token.

## Connect Claude Code

1. In the dialog, select **Copy command**.
2. Paste it into a terminal on the computer and run it. It looks like this:

   ```
   claude mcp add --transport http helmcentral http://helmcentral.local:9091/mcp --header "Authorization: Bearer hcmcp_..."
   ```

3. Start Claude Code and ask about the boat, for example "How's the house bank?"

The address in the command might not be one the computer can reach. That
happens when you opened Helmcentral as `localhost` on the boat's server. In
that case, change it to the name or Tailscale address the computer uses.

## Connect Claude Desktop

1. In the dialog, select **Copy snippet**.
2. In Claude Desktop, open **Settings, Developer, Edit Config**. This opens
   `claude_desktop_config.json`.
3. Add the snippet's `helmcentral` entry under `mcpServers`, save the file and
   restart Claude Desktop.

The snippet uses `npx` to connect, so the computer needs Node.js installed.

## Choose what it can see

1. In **Settings, Outside assistants**, find the token.
2. Switch the tools you want on or off. The change applies on the program's next
   request.

A new token starts with the usual set. Live SignalK paths and the alarm
thresholds start off.

## Stop a program

1. In **Settings, Outside assistants**, select **Revoke** beside its token.
2. Confirm.

The program is refused from its next request, and the other tokens carry on.
The **Recent activity** list shows what each program asked for.

## If it doesn't connect

- **It says unauthorized:** The token is wrong or revoked. Make a new one.
- **It says forbidden:** The request came from a web page. Use Claude Code or
  Claude Desktop rather than a browser.
- **It can't find the server:** The address isn't reachable from that computer.
  Check that it is on the boat's network or on Tailscale.

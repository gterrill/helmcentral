# Connect ChatGPT

This lets ChatGPT read the boat, so you can ask it how the house bank is doing
or what alarms have fired. It can only read. [Outside
assistants](../features/outside-assistants.md) explains what it can see.

ChatGPT connects from OpenAI's servers, and it can't send a fixed header with
its requests. So it can't use the `Authorization` header that Claude's
connector uses. Instead it signs in. When you add the boat, ChatGPT opens a
Helmcentral page, you paste an outside assistant token into it, and you select
**Allow**. ChatGPT then uses that same token for every request.

## What you need

- **A public address:** Tailscale Funnel has to pass on both `/mcp` and
  `/.well-known`. Follow [Give outside assistants a public
  address](give-outside-assistants-a-public-address.md), and do the browser
  check at the end of its setup. ChatGPT can't sign in without `/.well-known`.
- **An admin sign-in to Helmcentral:** You make the token in **Settings**.
- **A ChatGPT plan with developer mode:** ChatGPT only lets you add your own
  MCP server in developer mode, and only some paid plans offer it. OpenAI's page
  on [developer mode and MCP
  apps](https://help.openai.com/en/articles/12584461-developer-mode-and-mcp-apps-in-chatgpt)
  lists which ones do.

## Make a token for it

Give ChatGPT a token of its own, so you can revoke it without touching your
other programs.

1. Open **Settings** and choose **Outside assistants**.
2. Under **Name**, type "ChatGPT", and select **Create token**.
3. Copy the token from the dialog. Helmcentral can't show it again.

Keep the token somewhere you can paste from for the next few minutes. You paste
it once, into the sign-in page.

## Add the app in ChatGPT

OpenAI changes its screens often, so the names below might differ a little. The
page on developer mode shows the current ones.

1. On chatgpt.com, open **Settings** and choose **Apps**. It was called
   **Connectors** before.
2. Open **Advanced settings** and turn on **Developer mode**.
3. Create an app, or add a custom MCP server. Use the address
   `https://<machine>.<tailnet>.ts.net/mcp`, with the machine that runs Funnel
   and no port number.
4. For authentication, choose **OAuth**.
5. ChatGPT opens a Helmcentral page titled "Sign in to Helmcentral". Its
   heading names the app asking, such as "Let ChatGPT read the boat". Paste the
   token and select **Allow**.
   Select **Cancel** if you don't want to go ahead.
6. ChatGPT finishes adding the app and lists what it can do.

## Ask about the boat

1. Start a chat. In developer mode, each chat lets you choose which apps it may
   use, so turn on the Helmcentral app for this one.
2. Ask about the boat, for example "How's the house bank?"

Developer mode warns you before an app takes an action that writes something.
Helmcentral's tools only read, so you can approve them without worrying that
anything on the boat will change.

An app you add on the web should show up in the ChatGPT phone app too.

## Stop it

1. In **Settings, Outside assistants**, select **Revoke** beside the ChatGPT
   token.
2. Confirm.

ChatGPT is refused from its next request. The token never runs out on its own,
so revoking it is how you cut ChatGPT off. Your other programs carry on.

You can also remove the app in ChatGPT, but that doesn't revoke the token. If
you want the token gone, revoke it as well.

## If it doesn't connect

- **ChatGPT says it can't reach the server, or can't find how to sign in:** The
  `/.well-known` path probably isn't being passed on. Open
  `https://<machine>.<tailnet>.ts.net/.well-known/oauth-protected-resource` in a
  browser. You should see a short JSON document that names the address. If you
  see "404 page not found", go back to [Give outside assistants a public
  address](give-outside-assistants-a-public-address.md) and add the
  `/.well-known` path.
- **The sign-in page says the link isn't valid:** Helmcentral doesn't
  recognise the app that sent you there, or where it wants to send you back.
  Remove the app in ChatGPT and add it again.
- **The sign-in page says "That token isn't valid":** The token is wrong or
  revoked. Make a new one in **Settings, Outside assistants**, and paste that.
- **ChatGPT lists the app but gets no answers:** The token may have been
  revoked since you signed in. Make a new token, remove the app in ChatGPT, and
  add it again.
- **Nothing shows in the activity list:** Helmcentral only records requests
  that carry a valid token. If the list stays empty, the sign-in didn't finish.
  Add the app again.

## Things to know

- **OpenAI keeps the token.** ChatGPT holds it and sends it with every request,
  the same as Claude's connector does. If you stop trusting that, revoke the
  token.
- **Revoking is the way to cut it off.** The token never expires on its own.
  Make a token just for ChatGPT, so that revoking it doesn't stop anything else.
- **Sign-ins show in the activity list.** Under **Recent activity**, a sign-in
  appears as "sign-in from" and the app's name, under the token's name. The
  address it went to follows in brackets, such as "(chatgpt.com)". Everything
  ChatGPT asks after that appears there too. If the name is one you don't
  recognise, or the address isn't the app's own, someone else signed in. Revoke
  that token.
- **The address is public.** The token is the only thing that stops a stranger
  who finds the address. If the address or the token leaks, revoke the token and
  make another.

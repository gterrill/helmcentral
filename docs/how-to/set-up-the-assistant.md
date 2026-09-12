# Set up Mate

See [Mate](../features/assistant.md) for what it does, what leaves the boat,
and what it isn't. For voice, once Mate itself is configured, see [Talk to
Mate](talk-to-mate.md).

## 1. Get an OpenRouter key

Mate is bring-your-own-key: Helmcentral doesn't supply a model or pay for
your questions.

1. Create an account at <https://openrouter.ai>.
2. Add credit to it. OpenRouter is pay-as-you-go; a few dollars covers a
   long time of passage-planning questions.
3. Create an API key under your account settings and copy it. You won't be
   able to see it again after this step.

## 2. Configure it in Helmcentral

With write access, open **Settings → Mate**:

1. Paste the key into the **OpenRouter API key** field.
2. Choose a model. The default, `anthropic/claude-sonnet-4.5`, works out of
   the box; any OpenRouter model id that supports tool calling will work.
   Leave this field blank to fall back to the default.
3. Write your standing notes. This is anything about your own cruising
   ground you want on every answer, for example:

   ```
   Queenfish fish Hill Inlet on a rising tide.
   Snorkel Blue Pearl Bay in the last 1-2h of flood up to high slack.
   ```

4. Switch Mate on.
5. Choose **Save**.

## 3. Ask it something

Open the Mate panel and try the question it's built around:

> We're at Hook Reef. Should we visit Tongue Bay or Blue Pearl Bay first
> over the next two days?

Expect a few status lines while it looks each place up and fetches wind and
tides for both, then a short answer with a comparison table.

## If it says "No endpoints found that support tool use"

The model you picked doesn't support tool calling, which Mate needs for
every question beyond small talk. Go back to Settings → Mate and choose a
different model. `anthropic/claude-sonnet-4.5` is the tested default; most
current flagship models from the major providers support tool calling, but
not every model OpenRouter lists does, and OpenRouter's own model list marks
which ones do.

## Write access and cost

Posting a question needs write access. With login switched off (this
release's default), that's everyone on the boat's network. With login
switched on, a read-only account can open the panel and read past
conversations but can't send a new message, since sending one spends your
OpenRouter key.

Every reply's footer shows what it cost, so keep an eye on it the same way
you'd watch any other pay-as-you-go service, especially if login is off and
more than one person on the boat has access to the dashboard.

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
2. Choose your routing mode:
   - Turn on **Use OpenRouter Auto** to use `openrouter/auto`.
   - Leave it off to pick a fixed model from the tool-capable model dropdown
     (the list is fetched from OpenRouter with tool support filtering).
3. Optional: when Auto is on, set routing constraints:
   - **Auto cost tier**: `low`, `medium`, `high`, `xhigh`, or `max`.
   - **Allowed models**: comma-separated model patterns (for example
     `anthropic/*`).
   - **Excluded models**: comma-separated model ids to block.
4. Write your standing notes. This is anything about your own cruising
   ground you want on every answer, for example:

   ```
   Queenfish fish Hill Inlet on a rising tide.
   Snorkel Blue Pearl Bay in the last 1-2h of flood up to high slack.
   ```

5. Switch Mate on.
6. Choose **Save**.

## 3. Ask it something

Open the Mate panel and try the question it's built around:

> We're at Hook Reef. Should we visit Tongue Bay or Blue Pearl Bay first
> over the next two days?

Expect a few status lines while it looks each place up and fetches wind and
tides for both, then a short answer with a comparison table.

## If it says "No endpoints found that support tool use"

The selected model path cannot execute tool calls. If you are on a fixed
model, choose a different model from the dropdown. If you are on OpenRouter
Auto, relax `allowed models`/`excluded models`/`cost tier` so Auto still has
at least one tool-capable model available.

## Write access and cost

Posting a question needs write access. With login switched off (this
release's default), that's everyone on the boat's network. With login
switched on, a read-only account can open the panel and read past
conversations but can't send a new message, since sending one spends your
OpenRouter key.

Every reply's footer shows what it cost, so keep an eye on it the same way
you'd watch any other pay-as-you-go service, especially if login is off and
more than one person on the boat has access to the dashboard.

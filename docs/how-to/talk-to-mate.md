# Talk to Mate

See [Mate](../features/assistant.md) for what it answers, what it does with
your voice, and what it isn't.

## 1. Open Mate over https

Voice needs a secure address, because a browser won't give a page the
microphone over plain `http://`. Open Helmcentral at the same `https://`
address your phone already uses for push notification alarms
(`https://<machine>.<tailnet>.ts.net`). If you haven't set that address up yet,
see [Web push over
Tailscale](../reference/configuration.md#web-push-over-tailscale). The LAN
`http://<ip>:8080` address still works for everything else, but it won't offer
you the microphone. Open Mate itself at
`https://<machine>.<tailnet>.ts.net/mate`.

## 2. Turn voice on

With write access, open **Settings → Mate → Voice**, switch on **Voice input**
and save.

Two more switches live in the same section, both off by default:

- **Read replies aloud** speaks a short summary of Mate's answer as soon as it
  arrives, on top of the written answer, whenever you asked by voice.
- **Listen for Hey Mate** keeps the microphone open the whole time the app is
  on screen, so you can start with "Hey Mate" instead of tapping anything.
  Before you turn it on, see [Mate](../features/assistant.md#talking-to-mate)
  for what it costs in battery and privacy.

## 3. Allow the microphone

The first time you tap the microphone, the browser asks for permission. Allow
it. If you tapped Deny by mistake, or your browser denies by default, the fix
is in the browser's own site settings (the padlock or site-info icon next to
the address bar), not in Helmcentral. Find Helmcentral's entry there, change
the microphone permission to Allow, then reload the page.

## 4. Ask

Tap the microphone in the header, or press `Alt+M` from anywhere in the app,
and speak. The transcript appears as you talk, and it sends once you stop.
Escape cancels a listening session before it sends.

If **Listen for Hey Mate** is on, say "Hey Mate" followed by your question in
one breath, or say "Hey Mate" alone and follow up within about eight seconds.

## Dictating instead of asking

The composer's own microphone, inside the text box beside Send, is a different
button with a different job. It adds what you say to what you've already typed
and never sends by itself. Tap it (it's labelled **Dictate**) to start. It
fills in solid while it's listening, and the words it hears show right there in
the box so you can check them before you send. Tap it again (now **Stop
dictation**) when you're done, or press Escape to cancel without closing
whatever you were doing. Either way, nothing sends until you press Send.

The note capture sheet's microphone (**Documents → New → Note**) follows the
same rule, but types wherever the cursor is in the note rather than only at the
end. See [Notes and the manual](../features/notes-and-the-manual.md).

## Stop it reading aloud

While a reply is being read aloud, a small square button appears next to the
Mate sheet's title. Tap it to stop. To stop replies being read aloud at all,
turn off **Read replies aloud** in Settings → Mate → Voice.

## Troubleshooting

Mate reports voice problems by name rather than going quiet. On a browser with
no speech recognition at all, neither microphone appears, so there's nothing to
tap. Press `Alt+M` anyway and you'll see that message. Every other message
below shows where you'd expect it: on the header mic's tooltip, or as a line
under the composer or note box for its dictation mic.

| Message | What it means | What to do |
| --- | --- | --- |
| **This browser has no speech recognition.** | Firefox, or any browser with no speech recognition support. | Use Safari or Chrome instead. |
| **Voice input needs the app opened over https** | You're on the plain `http://` address. | Open the `https://` address from step 1. |
| **Microphone blocked. Allow it for this site in the browser.** | The browser's own microphone permission for this page is set to deny. | Change it to Allow in the browser's site settings, then reload. |
| **No microphone found.** | The device genuinely has no microphone, or the OS has none available to the browser. | Use a device with a working microphone. |
| **No speech heard.** | Recognition started but nothing was said before it gave up. | Try again, and speak sooner after tapping. |
| **The speech service could not be reached.** | Chrome's recognizer (and Safari's, when it isn't using on-device dictation) needs a network path and didn't have one. | Check your internet connection and try again. |

If **Listen for Hey Mate** is on and the microphone turns out to be blocked or
missing, the listener stops retrying until you turn the switch off and back on.
It doesn't keep failing quietly in the background.

# ADR 0045: Web push needs a secure context, so Helmcentral becomes an installable PWA served over Tailscale

## Status

Accepted.

## Context

Adding web push as an alarm transport ([ADR 0038](0038-alarms.md)) turned out to require two things that have nothing to do with alarms and that outlive the feature: a secure origin, and an installable web app.

**Helmcentral ships no TLS.** `main.go` calls Echo's `e.Start` on plain HTTP, `README.md` and `install.sh` both tell the operator to open `http://<this-machine>:8080/`, and `auth_handlers.go` already states the position in a comment — cookies are not marked `Secure` because that "would silently discard every cookie on the default plain-HTTP boat LAN". The Push API, service workers, and `Notification.requestPermission` are all gated on `window.isSecureContext`. On the primary case — a phone or helm tablet at `http://<lan-ip>:8080` — none of them exist.

The one deployment where they do exist is a browser on the Pi itself at `localhost`, which is exactly the deployment where phone alerts have no value.

## Decision

### 1. Helmcentral still ships no TLS. `tailscale serve` is the supported answer.

```
tailscale serve --bg 8080
```

That publishes the dashboard at `https://<machine>.<tailnet>.ts.net` with a real Let's Encrypt certificate, no public DNS, no certificate to install on each phone, and no open port on the boat.

The alternatives were considered and rejected:

- **A self-signed certificate** would work on a bare LAN with no internet, but every phone must install and manually trust a CA — on iOS that is a configuration profile plus a separate toggle buried in Settings → General → About → Certificate Trust Settings. Shipping a feature whose setup instructions are that is worse than not shipping it.
- **ACME/autocert** cannot work: a boat has no public DNS name and usually no inbound reachability.
- **Requiring an operator-supplied reverse proxy** is what the docs already suggest for remote access, and remains fine — but it is not an *answer*, it is a deferral, and it leaves the settings UI unable to say anything actionable.

**Tailscale Funnel is not required and must not be used.** The push service never calls back into Helmcentral: the only party needing the secure origin is the browser, and that browser is already on the tailnet. Funnel would expose the boat to the public internet to solve a problem it does not have.

This decision generalises. Anything else needing a secure context — Geolocation for a phone acting as a position source, WebAuthn, Web Bluetooth — now has an answer already written down.

### 2. Helmcentral becomes an installable PWA

A web app manifest, an icon set, and a service worker, because **iOS grants the Push API only to a site added to the Home Screen** (16.4+). Until then Safari exposes `navigator.serviceWorker` in an ordinary tab but withholds `window.PushManager` — which is precisely detectable, so the UI says "add to Home Screen" rather than "unsupported".

Three details are load-bearing and each fails silently when wrong:

- **iOS ignores the manifest's icons** and uses `apple-touch-icon`, which must be a PNG. Missing or SVG gives a screenshot of the page as the Home Screen icon.
- **Go's builtin MIME table has no `.webmanifest`**, and the release container ships no `/etc/mime.types`, so `http.FileServer` sniffs the manifest as `text/plain` and Firefox rejects it outright. `static.go` registers the type explicitly.
- **The install must be performed from the `.ts.net` origin.** Installing from the LAN `http://` address produces an installed app that still cannot receive push, with nothing on screen to explain why.

The icon is lucide's `Anchor` — already the brand mark on the login screen — rendered in the app's own `--primary` on `--background`, so the Home Screen icon matches the shipped theme. This also fixed a dangling `/vite.svg` favicon reference that had been 404ing since the project was scaffolded.

### 3. The service worker does push, and nothing else

No `fetch` handler, no offline caching, no precache manifest. Caching the app shell would serve stale JavaScript after an upgrade, and this project has no cache-busting story for a service worker — a bug class that does not exist today and would be tedious to diagnose on a boat. The fence is written in the worker's own header comment as well as here, because "add offline support while you're in there" is the obvious scope creep.

It registers **lazily**, on the operator's Enable click, never on app load: a helm kiosk that will never want push should not have a service worker installed on first paint, changing the app's update semantics to fix nothing.

It also handles `pushsubscriptionchange` by re-subscribing and re-registering. A browser may rotate a subscription on its own, and without this the server would keep pushing at a dead endpoint until a `410` pruned it, with the crew never told their phone had gone quiet.

## Consequences

- **Push is unavailable on a plain-LAN install, and the UI says so.** The settings section detects the insecure context first — before capability checks, because on `http://` those are absent as a *consequence* — and shows the `tailscale serve` recipe inline rather than a toggle that cannot work.
- **Helmcentral now has an identity on a phone's Home Screen** for every user who installs it, whether or not they use alarms.
- **Tailscale becomes a documented, optional dependency** for one feature. It was already suggested for remote access; this makes it load-bearing for push specifically.
- **A future contributor adding caching to `sw.js`** would introduce a stale-asset risk that does not exist today, which is why the scope fence is stated twice.
- The manifest MIME registration is easy to lose in a `static.go` refactor and fails silently on Firefox, so it carries a comment naming the consequence.

## Related

- [ADR 0038](0038-alarms.md) — the alarm system and the web push transport itself
- [ADR 0036](0036-distribution-and-packaging.md) — same-origin frontend, which is what lets one `tailscale serve` cover both the SPA and the API
- [ADR 0040](0040-signalk-delegated-authentication.md) — auth posture on the boat LAN

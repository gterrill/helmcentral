# ADR 0074: Deep Links

## Status
Accepted

Path URLs, built and read by hand rather than through a router library.

## Context

Helmcentral has no URL state. The whole shell is one `activePanel` value held
in a `useState` in `App.tsx`, so there is no address for the Forecast drawer,
no address for a settings section, no address for a particular dashboard page.
The user wants to send `http://192.168.50.240:9091/forecast` to someone and
have it open the forecast panel rather than the dashboard with an extra click
to make.

Navigation already passes through `requestNavigate` in
`App.tsx`. Every sidebar click, breadcrumb click, page sub-item and the alarm
banner's "open" action already calls it rather than `setActivePanel` directly,
because it is also the guard that stops a click from leaving a dirty Settings
page unsaved. Any URL mechanism that bypassed this function would bypass the
guard too, so Back/Forward navigation must also call it.

Push notifications need somewhere to land. The service worker already opens
whatever `url` a push payload carries when the notification is tapped; today
that url is always `/`, so a tapped alarm opens the dashboard rather than the
Alarms panel.

Existing deployment behaviour supports deep links. The backend's
static handler serves the embedded app shell for any path it doesn't
recognise as a file, rather than a 404 (`/anchor`, `/routes`, and now
`/forecast` all reach the same `index.html`), and the Vite dev server does the
same for local development. The PWA manifest scope is `/`, the service worker
installs no `fetch` handler, and the login screen renders in place rather than
redirecting, so a deep link survives a sign-in prompt sitting in front of it.

## Decision

### One pure module owns the mapping

A new file, `frontend/src/lib/app-location.ts`, is the only place that knows
how a URL string maps to app state. It exports a parser
(`parseAppLocation`), a formatter (`formatAppLocation`), and a canonical-form
check (`isCanonicalAppPath`). It has no React dependency and no dependency on
`App.tsx`, so it can be unit tested on its own and so the parser can validate
panel and settings-section ids without creating an import cycle back to the
component that uses it.

The URL forms are:

| URL | State |
| --- | --- |
| `/` | dashboard, first page |
| `/dashboard/<pageId>` | dashboard, that page |
| `/forecast` `/routes` `/charts` `/radar` `/anchor-watch` `/alarms` | that panel |
| `/mate` | Mate panel |
| `/mate/<threadId>` | Mate panel, that conversation |
| `/settings` | settings, General section |
| `/settings/<sectionId>` | settings, that section |

### Every state has exactly one URL string

Given the current page list, the mapping from state to URL is one-to-one, not
merely reversible. `/dashboard/<firstPageId>` normalises to `/`,
`/settings/general` normalises to `/settings`, a trailing slash or an extra
segment is dropped, and any path the parser doesn't recognise resolves to the
dashboard rather than throwing. This canonical-form rule is what keeps the
sync effect (below) from fighting itself: if two different strings could both
mean "dashboard, page one", the effect would have no way to decide whether a
given path already matches the current state or needs rewriting, and it would
loop.

`/` is the dashboard's first page, in server order, and not a separate "no
page selected" state. `/settings` is the General section, and not a separate
"no section" state. Both choices follow from the same rule: a state either has
a shorter canonical form or it doesn't, and the dashboard's default page and
settings' default section both do.

### One effect writes the URL; everything else only reads it

Nothing in the sidebar, the breadcrumb, a page tile, or the alarm banner calls
`history.pushState` itself. `App.tsx` gains one effect that watches the
current panel, page and settings section, formats the canonical string for
that state, and writes it with `pushState` or `replaceState` depending on
whether the current bar already holds a non-canonical form of the same
target. This was chosen over pushing at each of the roughly ten call sites
that currently call `setActivePanel`: a single writer needs no churn at any of
those call sites, and it is the only design that also covers a state change
that didn't originate from a click at all, such as the dashboard reconciling
an unknown page id in a deep link down to the first real page once the page
list has loaded.

### Back and Forward go through the same guard as a click

A `popstate` handler parses the URL the browser just navigated to and calls
`requestNavigate`, exactly as a sidebar click would. This means Back from a
dirty Settings page does not silently discard an edit: the browser moves to
the previous history entry, the handler sees that the guard intercepts the
navigation, and it re-pushes the Settings URL so the address bar agrees with
what's still on screen while the "discard changes?" dialog is open. Discard or
Save-and-continue then let the stashed navigation run, which is what finally
changes the state and lets the sync effect push the real destination.

The consequence is one extra history entry on that path: a Back that gets
intercepted and then confirmed leaves the browser one entry further from
where a plain, unintercepted Back would have landed. This is accepted. The
alternative, calling `history.back()` a second time to consume the entry the
first Back produced, means the guard fires again and the dialog reopens
mid-navigation, which is a worse experience than one harmless extra entry.

## Alternatives considered and rejected

**Hash URLs** (`/#/forecast`). Rejected because the backend already serves the
app shell for deep paths, so fragment-based routing is unnecessary. Path URLs
also align with the PWA manifest scope and the service worker's push `url`.

**A router library.** Three pieces of URL state (panel, page, settings
section) is not enough surface to justify the dependency, and every option
available assumes a sidebar built from `<Route>` or `<Link>` elements, which
would mean rewriting the sidebar and breadcrumb around the router's navigation
model rather than retaining `requestNavigate` as the navigation guard. A
`pushState`/`popstate` pair in one
module and one effect does what three states need without that rewrite.

## Related

- [ADR 0038: Alarms](0038-alarms.md), the push transport whose notification
  `url` this ADR gives a destination in the app.
- [ADR 0045: Web push needs a secure context, so Helmcentral becomes an
  installable PWA served over Tailscale](0045-web-push-secure-context-and-pwa-shell.md),
  the manifest scope and no-`fetch`-handler service worker this ADR relies on
  staying as they are.

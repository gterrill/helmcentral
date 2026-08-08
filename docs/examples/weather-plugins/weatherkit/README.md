# Apple WeatherKit weather-provider plugin (reference example)

A Helmcentral weather-provider plugin backed by Apple's WeatherKit REST API.
It is the port of what used to be a hardcoded, native-only integration in
`backend/weather_tide.go` (now deleted - see this repo's WASM-plugin
migration docs) into the same pluggable weather-provider contract every
other weather source uses (`backend/weather_providers.go`).

Unlike [../open-meteo](../open-meteo) (Helmcentral's default, free, keyless
weather provider), WeatherKit **requires a paid Apple Developer Program
membership** (currently US$99/year) - you cannot use this plugin without
first getting your own Apple WeatherKit credentials. In exchange, WeatherKit
includes a free tier of 500,000 API calls/month as part of that
membership, which is generous for a single-vessel dashboard. Both of those
figures come from the community writeup cited below - double-check them
against Apple's current developer program terms before budgeting around
them, since pricing/tiers can change.

It is also the only Helmcentral weather/tide plugin that needs
per-operator secrets (a WeatherKit signing key) and the only one that does
cryptographic signing inside the WASM guest itself (an ES256-signed JWT,
which WeatherKit's API requires on every request). See
`backend/testdata/wasm_plugins/src/es256sign/main.go` in the main
Helmcentral repo if you're curious about the feasibility spike that proved
TinyGo can do this at all.

Written in TinyGo - see [../../tide-plugins/noaa/README.md](../../tide-plugins/noaa/README.md)
for notes on alternative Extism PDK languages.

## Contract requirements every weather plugin must honour

Two rules from
[ADR 0035](../../../adr/0035-weather-local-day-boundaries.md) are easy to get
wrong and produce a plausible-looking but incorrect forecast rather than an
obvious failure:

1. **Use the host's `timezone` input.** `fetch_forecast` receives an IANA zone
   (e.g. `Etc/GMT-10`). If your upstream API rolls hourly data up into daily
   summaries, pass this through instead of hardcoding a zone or letting the
   API pick one. The host buckets and labels its own day series in that same
   zone; rolling up on a different boundary silently shifts every day summary
   and drops the record covering local midnight to the offset. An absent
   `timezone` is rejected, not defaulted.

2. **Report absent precipitation as `-1`, never `0`.** `0%` is a legitimate
   forecast, so it cannot double as "no data". If the upstream response omits
   a chance-of-precipitation value, emit `-1` on
   `precipitation_chance_pct` so the UI can show "unavailable". Never
   substitute a value from a different field or a different time window - that
   is how an mm/hr rainfall rate once ended up rendered as a percentage.

## Why this plugin is two files

`weatherkit.go` holds all the JWT-building, request-URL-building, and
WeatherKit-JSON-mapping logic, and has no dependency on
`github.com/extism/go-pdk`, so `go test ./...` (below) runs on the plain
host Go toolchain with no TinyGo or wasm target needed. `main.go` holds only
the thin `//go:wasmexport` wrapper functions and is gated `//go:build
tinygo` so it's excluded from that plain host build (TinyGo defines the
`tinygo` build tag automatically; plain `go test` doesn't) - mirrors
[../../tide-plugins/bom](../../tide-plugins/bom)'s split exactly. Because of
this split, always build the whole package directory (`.`), not just
`main.go` by name.

## Building it

Requires only Docker (no local TinyGo install needed), pinned to
`tinygo/tinygo:0.41.1` - the same version pinned for this repo's own WASM
test fixtures, to avoid `:latest` drift. From the repo root:

```sh
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:0.41.1 sh -c "
  cd docs/examples/weather-plugins/weatherkit &&
  go mod tidy &&
  tinygo build -o weatherkit.wasm -target wasip1 -buildmode c-shared .
"
```

Note the trailing `.` (build the whole package directory), not `main.go` -
naming `main.go` alone would exclude `weatherkit.go` and fail with
`undefined:` errors.

This produces `weatherkit.wasm` in this directory. Requires network access
inside the container (`go mod tidy` fetches `github.com/extism/go-pdk`) and
Docker with the `tinygo/tinygo` image available locally.

If you'd rather install TinyGo locally instead of using Docker, the
equivalent commands are:

```sh
go mod tidy
tinygo build -o weatherkit.wasm -target wasip1 -buildmode c-shared .
```

## Getting Apple WeatherKit credentials

This plugin needs four values before it can do anything:
`WEATHERKIT_KEY_ID`, `WEATHERKIT_TEAM_ID`, `WEATHERKIT_SERVICE_ID`, and
`WEATHERKIT_PRIVATE_KEY`. Here's how to get them, adapted from a community
writeup on migrating off the old Dark Sky API onto WeatherKit
([dev.to/dkechag - "Replacing the Dark Sky weather API: WeatherKit, 7Timer
and more"](https://dev.to/dkechag/replacing-the-dark-sky-weather-api-weatherkit-7timer-and-more-3o));
that article is worth reading in full if any of Apple's screens have moved
since this was written, and as a sanity check on the pricing figures quoted
above.

1. **Join the Apple Developer Program**, if you haven't already, at
   [developer.apple.com](https://developer.apple.com) (currently US$99/year).
   WeatherKit access is bundled into a standard paid membership - no
   separate weather-specific sign-up is needed.
2. In the developer portal, go to **Certificates, Identifiers & Profiles**
   and register a new **key** with the WeatherKit capability enabled.
   Downloading it is a **one-time** action - Apple will not let you
   re-download the same key file later, so save it somewhere safe as soon
   as it's offered. The download is a file named `AuthKey_<KEYID>.p8`. The
   portal shows you the **Key ID** at this step - that's your
   `WEATHERKIT_KEY_ID`.
3. Convert the downloaded `.p8` key from PKCS8-DER to PEM, since this
   plugin's config expects a PEM string, not a raw `.p8` file:

   ```sh
   openssl pkcs8 -nocrypt -in AuthKey_<KEYID>.p8 -out AuthKey_<KEYID>.pem
   ```

   The full contents of the resulting `.pem` file - the entire
   `-----BEGIN PRIVATE KEY-----` ... `-----END PRIVATE KEY-----` block,
   including those header/footer lines - is your `WEATHERKIT_PRIVATE_KEY`.
4. Back in **Certificates, Identifiers & Profiles**, provision a new
   **Service ID** in reverse-domain notation (e.g.
   `com.yourcompany.helmcentral`). That identifier is your
   `WEATHERKIT_SERVICE_ID`.
5. Find your **Team ID** under **Membership Details** in the developer
   portal - that's your `WEATHERKIT_TEAM_ID`.
6. Set all four as environment variables on the Helmcentral backend:
   `WEATHERKIT_KEY_ID`, `WEATHERKIT_TEAM_ID`, `WEATHERKIT_SERVICE_ID`, and
   `WEATHERKIT_PRIVATE_KEY` (e.g. in `docker-compose.yml`'s backend service
   `environment:` block, or a local `.env` file), then restart the backend.

## Installing it

Helmcentral discovers weather-provider plugins by scanning `plugins/weather/`
at startup (overridable via the `PLUGINS_WEATHER_DIR` env var). To install
this plugin:

1. Copy the compiled `weatherkit.wasm` into your `plugins/weather/`
   directory.
2. Create `plugins/weather/weatherkit.allowed_hosts.json` next to it,
   containing:

   ```json
   ["weatherkit.apple.com"]
   ```

   This is not optional - a plugin with no companion
   `<name>.allowed_hosts.json` file gets **no network access at all**
   (Helmcentral's default-deny sandboxing). `weatherkit.apple.com` is the
   only host this plugin talks to.
3. Copy `weatherkit.config.json` into `plugins/weather/` too. As shipped,
   it references your four env vars by name:

   ```json
   {
     "key_id": "${WEATHERKIT_KEY_ID}",
     "team_id": "${WEATHERKIT_TEAM_ID}",
     "service_id": "${WEATHERKIT_SERVICE_ID}",
     "private_key": "${WEATHERKIT_PRIVATE_KEY}"
   }
   ```

   Leave the `${WEATHERKIT_*}` placeholders as-is - they're resolved
   against the backend container's environment at plugin-load time, not
   edited in this file. If you'd rather not use environment variables at
   all, you can instead put the literal key ID/team ID/service ID/PEM key
   text directly into this file in place of the `${...}` placeholders -
   both approaches work, since Helmcentral's config loader only touches
   `${...}`-shaped values and leaves everything else untouched.
4. Restart the Helmcentral container (or the dev backend), then select
   "Apple WeatherKit" as the weather provider in Settings.

**Note on unconfigured installs:** this plugin loads successfully even with
no config set at all - `id()`/`name()`/`ttl_seconds()` don't need
credentials. It's only when you actually select "Apple WeatherKit" and it
tries to fetch a forecast that missing credentials surface, as a clear
`weatherkit: missing config key(s): ...` error naming exactly which of the
four keys are absent and which env vars to set. There is no silent
failure and no fake weather data at any point - just a plugin that does
nothing (or errors loudly) until it's actually configured.

## Testing

`main_test.go` unit-tests JWT construction and ES256 signing (including an
actual cryptographic verification of the signature against a throwaway
key's public half, not just "did it produce some bytes"), WeatherKit
JSON-response mapping (unit conversions, field pass-through, day/hour
bucketing) against a canned fixture, and the missing-config-key error path -
entirely on the host Go toolchain, no TinyGo or WASM runtime needed:

```sh
go test ./...
```

## WeatherKit endpoint this plugin uses

- Forecast (current + daily + hourly in one call): `GET https://weatherkit.apple.com/api/v1/weather/en/<lat>/<lon>?dataSets=currentWeather,forecastDaily,forecastHourly&timezone=UTC`, with header `Authorization: Bearer <ES256 JWT>`.

  This differs from the original native integration in two ways, both
  documented in detail at the top of `weatherkit.go`: it merges what used
  to be two separate requests into one (this plugin's single
  `fetch_forecast` call needs current + daily + hourly data together), and
  it uses `timezone=UTC` instead of the original's hardcoded
  `timezone=America/Los_Angeles` (WeatherKit's RFC3339 timestamps already
  carry their own UTC offset regardless of that query parameter - the old
  hardcoded value was a leftover bug, not a real requirement).

See the comment blocks at the top of `main.go` and `weatherkit.go` for the
exact field mappings and unit conversions.

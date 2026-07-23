# ADR 0023: Encrypted Secrets Store

## Status
Accepted

## Context
Secrets (SignalK credentials, `INFLUXDB_TOKEN`, `STORMGLASS_API_KEY`, `GEONAMES_USERNAME`, and the four `WEATHERKIT_*` values, one of which is a multi-line PEM private key) previously lived in a plaintext `backend/.env` file, loaded into the container via docker-compose's `env_file:`. This had two concrete problems:

1. **Image-layer leakage risk.** `env_file:`-sourced values are visible to anything with access to the running container's environment (`docker inspect`, `/proc/<pid>/environ`, a shell in the container), and a plaintext `.env` sitting on disk is one accidental `COPY`/`git add`/backup-tool misconfiguration away from ending up somewhere it shouldn't.
2. **SSH install friction.** Operators installing over SSH had to hand-edit a multi-line file to paste a PEM-formatted WeatherKit private key correctly — easy to get wrong (line endings, quoting, trailing whitespace) with no immediate feedback if it's malformed.

We wanted secrets to be encrypted at rest, editable from the Settings UI instead of a text file, and — separately — to stop being implicitly available to every WASM plugin just because they're in the process environment.

## Decision
1. **Application-level AES-256-GCM, not SQLCipher.** Secrets are stored in a plain `modernc.org/sqlite` database (`backend/data/secrets.sqlite`), with each value's plaintext encrypted individually via stdlib `crypto/aes`/`crypto/cipher`/`crypto/rand` before being written (random 12-byte nonce per write, stored alongside the ciphertext). SQLCipher was considered and rejected: it requires CGO, and this backend is built with `CGO_ENABLED=0` for a static binary — pulling in SQLCipher would mean reintroducing a CGO toolchain into the build for this one feature. Encrypting values at the application layer keeps `modernc.org/sqlite`'s pure-Go, CGO-free properties intact while still getting encryption at rest.

2. **Master key resolution, in priority order:**
   - `HELMCENTRAL_MASTER_KEY` env var (base64, must decode to exactly 32 bytes) — for operators who want to manage the key externally (e.g. a secrets manager injecting it at container start).
   - `backend/data/secrets.key` (32 raw bytes, `0600`) — read if present.
   - Otherwise, a new 32-byte key is generated via `crypto/rand` and written to `secrets.key` (`0600`, parent dir `0700`) on first run. No configuration is required for a fresh install to work.

   On every open, every existing row is decrypted once as an integrity check. If any row fails — wrong key, rotated key, mismatched `HELMCENTRAL_MASTER_KEY` vs. `secrets.key` — the store refuses to open and the backend fails fast (`log.Fatalf`) rather than silently proceeding with some secrets unreadable. This follows the repo's fallback policy: a wrong key means something is actually broken, and that must be surfaced immediately, not patched over.

3. **Trusted host code vs. WASM plugins get secrets differently, on purpose.** `secretsStore.LoadIntoEnv()` sets only `coreEnvSecretKeys` (`SIGNALK_USERNAME`, `SIGNALK_PASSWORD`, `INFLUXDB_TOKEN`, `STORMGLASS_API_KEY`, `GEONAMES_USERNAME`) into the process environment via `os.Setenv`, because that's how trusted, non-sandboxed Go code already reads them (`getEnv`/`os.Getenv`). `WEATHERKIT_*` is deliberately excluded from `LoadIntoEnv` — it is plugin-only and must never become globally visible in the process environment, because every WASM plugin's `configForWasmPlugin` expansion currently reads straight from `os.LookupEnv`, and a value in the process env is a value every plugin's `config.json` could reference.

   Instead, plugin-facing secrets go through a new per-plugin allowlist: a companion `<name>.allowed_secrets.json` file (JSON array of secret key names, mirroring `<name>.allowed_hosts.json`'s shape and missing-file-means-none default). `configForWasmPlugin`'s `${VAR}`-expansion closure now checks each referenced name: if it's one of `knownSecretKeys`, it's resolved from `globalSecretsStore.Get()` **only if** the plugin's `allowed_secrets.json` lists it, and denied (dropped, logged) otherwise. Non-secret env var references are completely unaffected — this gate only intercepts names in `knownSecretKeys`.

   **This allowlist is host-enforced, not a WASM-sandbox guarantee.** It stops a plugin from silently receiving a secret nobody granted it access to, the same way `allowed_hosts.json` stops undeclared network egress — but a plugin that *is* granted a secret can still exfiltrate it over the network it's already allowed to reach. Operators installing a third-party plugin should review its `allowed_secrets.json` with the same scrutiny as its `allowed_hosts.json`: both are trust boundaries, not sandboxing.

4. **Migration is explicit, not automatic.** `POST /api/settings/secrets/import-env` reads `knownSecretKeys` from the current process environment and writes any non-empty ones into the store, logging exactly what it imported. It never clears a store value for a key that's since become unset in the environment, so it's safe to run more than once (e.g. after deleting `.env`) — a no-op the second time for anything already imported. This is an operator-triggered one-time action, not something run silently on every startup: per this repo's fallback policy, a behavior change (moving secrets into a new store) should be visible and deliberate, not something that happens invisibly the first time the binary starts with old env vars still lying around.

## Consequences
Positive:
- Secrets are encrypted at rest and no longer need to exist as a plaintext file on the host or inside the container's environment for any operator-facing workflow.
- WeatherKit's multi-line PEM key is pasted once into a UI field instead of hand-edited into a `.env` file over SSH.
- Plugins only see the secrets an operator explicitly grants them, per plugin — a compromised or careless third-party plugin can no longer read every secret configured on the box just because it's in the process environment.
- The build stays CGO-free; no SQLCipher toolchain dependency introduced.

Tradeoffs:
- `backend/data/secrets.key` must be backed up alongside `backend/data/secrets.sqlite`. Losing the key makes every encrypted secret unrecoverable — there is no key-recovery mechanism, by design (a recoverable key would defeat the point of encrypting at rest). This is intentional, not a bug: operators relying on the auto-generated key should include `backend/data/` in their backup strategy the same way they would any other stateful volume.
- The allowlist is a host-enforced convention, not a sandbox boundary (see point 3) — it requires the same operator vigilance as `allowed_hosts.json` already does.
- `ImportFromEnv` being explicit means an operator upgrading from the `.env`-based setup must remember to call it (or use the Settings UI's equivalent action) once; nothing migrates secrets for them automatically.

## Related
- ADR 0017: WASM Plugin Tide Providers (the `allowed_hosts.json` allowlist pattern this ADR's `allowed_secrets.json` mirrors)
- ADR 0018: WASM Plugin Weather And Wave Providers (the `<name>.config.json` `${ENV_VAR}` expansion mechanism this ADR modifies)
- `backend/secrets_store.go` (`secretsStore`, master key resolution, `LoadIntoEnv`, `ImportFromEnv`)
- `backend/secrets_settings_handlers.go` (`GET`/`POST /api/settings/secrets`, `POST /api/settings/secrets/import-env`)
- `backend/wasm_plugin.go` (`allowedSecretsForWasmPlugin`, `configForWasmPlugin`'s secrets gate)

# Agent Instructions

This file mirrors AGENTS.md. AGENTS.md is the canonical repo instruction file.

## Release Tags

- Use SemVer tags only (for example `v0.3.5`).
- Do not create date-based or ad-hoc tags.
- If tagging is requested, determine the next SemVer from existing tags.

## Fallback Policy

- Prefer fail-fast behavior for correctness and diagnostics paths.
- Do not add graceful fallback behavior that masks upstream data/source problems.
- If a required upstream source is missing (for example a required Furuno source), surface the issue explicitly and stop.

## Exception Rule

- Add fallback behavior only when explicitly requested.
- When fallback is approved, gate it behind a clear feature flag and emit explicit logs/telemetry that indicate fallback was used.

## Test-First Policy

- Prefer test-first development (TDD) for behavior changes and bug fixes.
- Write or update a failing test that reproduces the expected behavior before implementing the fix.
- Implement the minimal change required to make the test pass.
- Run relevant tests after the change and include the test result summary in your response.

## Documentation Location Policy

- Architectural decisions, design decisions, and feature specifications must be documented in the ADR folder: https://vscode.dev/github/gterrill/helmcentral/blob/main/docs/adr
- Do not keep durable feature-spec or architecture notes in backend/README.md or other component READMEs.
- When adding or changing behavior that affects architecture or feature contracts, create or update an ADR in docs/adr and link it from relevant README files.

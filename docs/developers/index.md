# Developer documentation

This tree is for people changing Helmcentral: building it, writing a
plugin, or working out how a setting is implemented under the hood. It is
not staged into the in-app help and isn't needed to run a boat on
Helmcentral - for that, start at [docs/index.md](../index.md) instead.

- [Development](development.md), running the stack, tests and release builds.
- [Writing a provider plugin](plugins.md), the WASM sandbox, the contracts
  each plugin category implements, and how to build one.
- [Configuration internals](configuration.md), implementation detail behind
  the operator-facing configuration reference.

Architecture decisions live separately, in [docs/adr/](../adr/), and the
buildable plugin source examples referenced above live in
[docs/examples/](../examples/).

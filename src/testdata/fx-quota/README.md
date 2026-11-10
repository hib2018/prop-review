# fx quota response fixture

This is a synthetic, anonymized reconstruction from official documentation and
source, NOT a captured quota failure. The local fx 0.0.16 request succeeded, so
no real quota failure was available. The reconstructed process exit status is 1;
stdout is `stdout.json` and stderr includes `[notice] Out of credits\n`.
Model and session identifiers are placeholders. The integration tests also inject
a proposal-like `final_output`, and simulate process status 0 with JSON status 1,
to ensure neither path accepts failed output.

Sources checked on 2026-11-10:

- https://fx.sh/docs/using-fx/fx-ask.md documents exit 1, stderr reasons and
  `recovery.state = need_user`, `recovery.need = quota`.
- https://github.com/vercel-labs/fx/blob/59ba74a1974786f126173b6d01acee71b1e52edf/src/core/recovery/recovery.zig
  defines `quota` and its `Out of credits` message (including a unit test).
- https://github.com/vercel-labs/fx/blob/59ba74a1974786f126173b6d01acee71b1e52edf/src/core/cli/cli_ask.zig
  renders the JSON envelope and emits recovery notices on stderr. Its JSON
  need-user test confirms `state`, `need`, `attempt`, and `message`.

Compatibility: fx v0.0.16 uses different recovery fields and has no confirmed
quota-specific signal in that JSON schema. Do not classify its HTTP 429,
`request_limit_reached` (a provider-request safety limit), or arbitrary error
text as a plan quota. Unrecognized failures keep the existing generic error
handling. A captured legacy quota response is needed before adding a legacy
matcher. Capture a real quota response when available to complement this fixture.

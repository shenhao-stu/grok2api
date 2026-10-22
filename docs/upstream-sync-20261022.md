# Upstream sync: v3.1.8 and security fixes

Merge target: chenyme/grok2api `e2fd7ab481a30a1965bb61395d437154cf247c92`.
Fork baseline: `0273bdf4`. Upstream contributes 42 commits; six fork commits remain in the merge ancestry.

| Area | Resolution |
| --- | --- |
| Client-key/account concurrency | Keep fork's zero-as-unlimited leases and ownership tracking; keep site -1 semantics and both UI translations. |
| Uploads and image-to-video | Keep authenticated 100 MiB chunked inputs, owner isolation, expiry/offset bounds, limited image decoding and same-account first-frame upload. |
| Welfare ledger/private media | Keep settled-ledger views, private output access and optional Welfare UI. |
| Build/Console models and billing | Keep Grok 4.7 routes/reasoning/pricing, service-tier handling, XML tool boundaries and quota freshness; accept upstream Console protocol fixes. |
| Video retries | Use upstream client-key account scope during retries and worker-shutdown recovery; retain fork's input-size regression. |
| Imagine output | Use upstream request-ID filtering to reject stale gallery frames; keep fork moderation/quota terminal errors with no adapter replay. Update the test server to echo the actual request ID. |
| Device consent | Replace the old fork form parser with upstream token/device or user-code/audience/expiry binding, cookie handling and explicit manual browser-verification error. Retire the unused duplicate parser. |
| Degraded accounts/cleanup | Accept upstream optional native settings. `autoDisable` and automatic deletion of unlinked Console accounts default to false. |
| Security dependencies | Accept upstream Docker, frontend and Go dependency upgrades, including Reality handshake adaptation. Retain x/image used by bounded first-frame processing. |

Validation: backend full suite, targeted race checks for scheduling/media/Web/leases, 16 frontend tests, standard and Welfare production builds. The first merged suite exposed an obsolete moderation test fixture without a request ID; the real adapter filtering remained intact and the fixture was corrected. Source review checked the conflict resolutions and the fork-only features against their regression coverage.

This is a repository merge. It does not migrate or restart the separate Welfare production service or import its credentials into GeToken. Sub2API natively exposes Images generation/editing and video generation/edit/extension/status/content. A usable backend and actual media entitlement are still required; Build OAuth, Web SSO and Console credentials are not interchangeable. Existing manually disabled accounts must remain disabled.

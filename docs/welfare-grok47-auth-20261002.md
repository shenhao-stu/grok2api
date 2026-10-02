# Grok 4.7 authorization compatibility

The public standard model is `grok-4.7`; the paid Build catalog separately returns
`grok-4.7-build-fast`. A standard response can report `grok-4.7-build` as its
upstream execution identity. It is not the client discovery name.

On October 2, the xAI Device Flow consent page supplied a required one-time
`consent_token`. The previous Web-to-Build conversion skipped that page and
submitted a fabricated minimal form, producing HTTP 403 at approval. Conversion
now loads the official consent form, validates its action, device code and user
principal, and submits the supplied fields with the consent origin and referrer.
Missing, ambiguous or changed forms fail before approval; authorization values
are never included in errors. The scope and referrer match the current CLI flow.

Existing model discovery, paid Fast entitlement, separate Fast billing, and
Build-only inference remain unchanged. No ordinary Web model is renamed to 4.7.
Fast uses its discovered model ID; unsupported priority/speed flags remain
explicit errors rather than silently becoming standard inference.

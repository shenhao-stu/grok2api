# Welfare deployment changes

This fork preserves the Welfare customizations while merging upstream
`7c889a960e2638341b4dae9a5c81af0e0f38c87f` (checked 2026-10-02).
Upstream MIT licensing and attribution remain unchanged.

- The Build transport defaults to the verified Grok CLI 1.0.46 client version.
  Updating this fingerprint does not grant OAuth access or manufacture a Build account.
- Authenticated media inputs accept up to 100 MiB. Browser uploads use sequential
  8 MiB chunks, an hour-long expiry, durable offset metadata, owner isolation and
  bounded storage. `GROK_INPUT_CHUNK_DIR` must use persistent storage owned by one
  application process. Multiple replicas must not share this upload directory.
- Grok Web image-to-video uses the same account for first-frame upload and video
  generation, and passes the returned file metadata ID to `imageToVideo`.
  Unsupported multi-reference input remains an explicit error.
- Large first-frame inputs are validated before decoding, with a 16 megapixel,
  8192-pixel side limit and two concurrent decoders. Large images are resized to
  1600 pixels and encoded as JPEG within the upstream 8 MiB first-frame budget.
  The 100 MiB file limit is independent of these decoded-image bounds.

Existing private output access, Welfare settled-ledger reporting, quota freshness,
provider separation and customized concurrency/settings behavior are retained.

Validation on the release tree: backend `go test ./...`, frontend tests/type check
and production build. Deployment also exercised authenticated 100 MiB chunk upload,
ownership boundaries and real image-to-video output. Runtime acceptance evidence
and operational details live in the paired `shenhao-stu/welfare` repository; no
runtime accounts, tokens, database dumps or media belong in either source tree.

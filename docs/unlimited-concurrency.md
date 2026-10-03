# Unlimited concurrency

Account concurrency now accepts `0` as unlimited in both individual and batch
administration. The database constraint, scheduler eligibility check, lease
acquisition and account editor use the same meaning. Positive limits remain
bounded by 256. Invalid negative values are rejected by account administration.

Unlimited requests still acquire owner leases. Redis and memory stores retain
active counts for fair account selection, cleanup protection and a later reduction
of the limit. Releases are idempotent. This does not reset real provider quotas,
cooldowns, model permissions or billing reservations.

There are separate controls:

| Layer | Unlimited value |
| --- | --- |
| Server request gate | `server.maxConcurrentRequests = -1` |
| Client API key | `maxConcurrent = 0` |
| Provider account | `maxConcurrent = 0` |

RPM limits are independent. New imports keep the upstream default of 8 until an
administrator explicitly sets their account concurrency; refreshes preserve an
existing zero. An unlimited local setting cannot increase upstream account quota
or physical server capacity. Per-request batch sizes and maintenance-worker
counts remain bounded.

Verification includes selection beyond the previous account cap, refresh
preservation, owner counts and idempotent release, lowering a cap while requests
are active, SQLite migration with credential preservation, and a real isolated
PostgreSQL migration plus Redis lease execution. The six affected backend
packages and frontend build passed. External tests not configured in the normal
package run are not represented as executed; the dedicated PostgreSQL/Redis
checks were run separately without skips.

Deployment and recovery records are maintained in the companion
[welfare repository](https://github.com/shenhao-stu/welfare), with encrypted
configuration/data backups kept outside Git.

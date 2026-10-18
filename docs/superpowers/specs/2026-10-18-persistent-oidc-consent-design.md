# Persistent OIDC consent

User-approved design: remember explicit consent independently of token lifetimes. One account/client consent accumulates explicitly approved scopes; smaller subsequent requests do not remove past consent. Each token contains only its requested scopes. Existing tokens retain their scopes and expiry. Access-token and refresh-token lifetimes stay unchanged.

A valid login session and existing authentication freshness checks remain mandatory. Additional scopes and prompt=consent require explicit consent; prompt=none must fail closed when consent/authentication is missing. Declines and failed transactions must not change consent.

Explicit consent approval and its success audit event commit atomically; any audit or request-write failure rolls back the consent change. Pending consent requests bind to the authenticated account only after subject and freshness checks, so application revocation can invalidate in-flight approvals without preventing fresh account selection.

Application revocation atomically deletes consent and associated authorization requests/access/refresh tokens. Single-token revocation and logout preserve consent. Application/account deletion removes associated consent. Existing client-lock ordering protects consent, issuance and revocation. Disabled clients and accounts cannot bypass checks.

The authorized-applications list uses durable consent, with cumulative scopes and no credential-expiry claim. Both themes and their self-contained translations must match. Active-token usage statistics remain unchanged.

Migration imports only unexpired access/refresh token permissions at a fixed cutoff. Do not infer consent from expired tokens, login history or authorization requests. A durable one-time marker, including on empty migration, prevents restart from restoring revoked grants. Migration and marker are atomic and schema-local; deployment must finish migration before serving requests (no mixed-version writer rollout).

Validation requires red/green regressions, PostgreSQL migration and concurrency coverage, regular Go tests, full PostgreSQL suite, translation contracts, TypeScript checking, standalone article-image browser tests and full theme browser matrix. No skipped PostgreSQL tests count as passing. After local verification, commit with CloudCode attribution, push the feature branch, and monitor CI for that exact commit. Do not merge or release.

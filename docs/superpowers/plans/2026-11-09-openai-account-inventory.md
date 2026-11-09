# OpenAI Account Inventory Implementation Plan

**Goal:** Keep the configured number of usable OpenAI/Codex accounts supplied from encrypted stock.

**Architecture:** PostgreSQL owns inventory rows, configuration, and short leases. A bounded server worker claims missing slots and uses the existing 2FA and Codex import services to activate stock. An administrator page manages imports, target, and status.

**Tech Stack:** Go, PostgreSQL, Gin, Vue 3, TypeScript.

## Global constraints

- Accept `email----password----MFA` lines and up to 10,000 rows per import.
- Never expose plaintext credentials or raw upstream failures after import.
- Target defaults to zero. A 401 gets a re-login attempt; only confirmed ban codes allow soft deletion.
- Count active and schedulable OpenAI OAuth parent accounts, including manually created accounts. Ignore transient cooldowns.

### Task 1: Persistence and import

- [ ] Add the inventory migration with unique normalized email, encrypted fields, state, lease, safe error code, and target configuration.
- [ ] Write parser and repository tests for invalid, duplicate, and 10,000-row imports; confirm failure leaves no plaintext or partial writes.
- [ ] Implement transactional batch import and status/count queries.

### Task 2: Replenishment

- [ ] Write tests for capacity arithmetic, concurrent claims, stale leases, login failure, duplicate account, and successful activation.
- [ ] Implement a bounded worker that uses the existing 2FA login and Codex import paths, then saves encrypted guard credentials.
- [ ] Reconcile confirmed ban codes to soft-delete the account and retire its stock row. Keep 401 and temporary errors for re-login.

### Task 3: Administrator API and page

- [ ] Add authenticated inventory status, import, target, and list endpoints with sanitized responses. Failed rows accept corrected credentials through the import endpoint.
- [ ] Add the inventory page to account management using established form, table, and loading patterns.
- [ ] Test target save, 10,000-line file upload, masked rows, and error states.

### Task 4: Integration and verification

- [ ] Wire the worker into server startup and shutdown and verify the server build.
- [ ] Run focused Go and frontend tests, database migration tests, typecheck, and production build.
- [ ] Review staged changes for secret exposure, capacity races, and unintended account deletion.

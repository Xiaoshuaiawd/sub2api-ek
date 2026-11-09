# OpenAI Account Inventory Design

## Purpose

Store up to 10,000 OpenAI/Codex email, password, and MFA records outside the active account table. An administrator sets a target number of usable OpenAI OAuth parent accounts. The server signs in and imports stock only when the current usable count falls below that target.

## Definition and lifecycle

Usable means a non-deleted OpenAI OAuth parent account with `status=active` and `schedulable=true`. Short rate limits and temporary cooldowns do not reduce this count. The count includes existing accounts, whether imported from stock or added manually. A 401 starts the existing credential guard's re-login path; a confirmed account ban or deletion is soft-deleted and released from the active count. Unknown, network, and rate-limit errors do not authorize deletion.

The target defaults to zero, so importing stock does not start logins. Saving a positive target starts a bounded background reconciler. It reserves at most the missing slots, taking existing in-flight reservations into account. It never deletes healthy accounts just because the administrator lowers the target or manually adds accounts. Multiple server instances coordinate claims in PostgreSQL. Failed stock entries remain visible with a safe reason code, and the reconciler moves to the next entry.

## Storage and security

Inventory has a unique normalized email, encrypted password and MFA secret, state, attempt count, linked account ID, lease, and safe error code. Use the existing persistent credential encryption key. Refuse imports until encryption is configured. Accept one `email----password----MFA` line per account, validate all lines before any insert, reject duplicates within the file, and skip emails already in inventory. Never return passwords, MFA values, ciphertext, or raw upstream errors from the API or logs.

## Activation

Use the existing OpenAI 2FA login worker, Codex session import normalization and deduplication, and encrypted credential guard configuration. A successful login and account import must be linked to the stock row before its reservation closes. Resume stale leases after a crash. Confirmed bans mark the stock row retired, soft-delete its linked account, and cause a replacement claim in a later cycle.

## Administrator interface

Add an inventory page under account management. It accepts a text file or pasted lines, shows imported/skipped/error totals and progress counts, and lets the administrator save a target and pause/resume replenishment. List rows with masked email, state, account ID, and safe error code; support pagination for 10,000 rows. Existing account management remains the place to inspect active accounts.

## Verification

Cover parser and encryption validation, duplicate handling, reservation capacity and multi-instance claims, login/import success and failure, 401 versus confirmed ban, and no secret disclosure. Verify the frontend build and that the target remains zero by default.

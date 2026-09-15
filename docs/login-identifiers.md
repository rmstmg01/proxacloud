# Username and email login rollout

Registration, admin creation, and account updates enforce case-insensitive,
trimmed username/email uniqueness. A username cannot equal another user's email.
An account may use its own email as its username. Blank emails remain supported
for administrative/bootstrap users; public registration requires an email.

The existing login API's `username` field now accepts either identifier. Tokens
still contain the account's username. Password reset uses the same identifier
registry; existing authorization and two-factor behavior are unchanged.

## Deployment

Back up PostgreSQL before deploying. From the repository on the server, audit:

```bash
docker exec -i proxacloud-postgres psql -U postgres -d k8s_proxmox < scripts/audit-login-identifiers.sql
```

Resolve any reported user IDs explicitly with the account owners/admin before
deployment. No accounts are merged or removed by the migration. Migration 0073
fails atomically if conflicts remain, so automatic migration will prevent backend
startup until they are resolved. The migration runner replays files on startup;
0073 is safe to repeat.

Deploy backend and frontend together after the audit is clear:

```bash
docker compose up -d --build backend frontend
```

The migration adds normalized unique indexes and a trigger-maintained identifier
registry whose primary key prevents cross-column races. Updates release old
identifiers; deletion releases identifiers through a foreign-key cascade.

## Verification

```bash
cd backend
go test ./internal/users
# Point TEST_POSTGRES_DSN at a disposable test PostgreSQL database, then:
go test ./internal/users -run TestIdentityPostgres -v
```

The PostgreSQL test creates and removes only its own uniquely named schema.
It verifies migration preflight/replay, duplicates, cross-column collisions,
updates/deletion, both login identifiers, wrong passwords, and concurrent claims.

# Customer email notifications

## Configuration and deployment

Set these values in the server's environment (credentials are examples/placeholders):

```env
PUBLIC_APP_URL=https://cloud.proxaops.com
SMTP_HOST=your-smtp-server
SMTP_PORT=587
SMTP_USERNAME=your-smtp-user
SMTP_PASSWORD=your-smtp-password
SMTP_FROM=noreply@proxaops.com
```

Port 587 uses STARTTLS when advertised; authentication requires TLS. Port 465
uses implicit TLS. Anonymous local SMTP relays remain supported. Certificates
are verified. Use a sender address permitted by your provider.

Keep SECRET_ENCRYPTION_KEY stable: pending email bodies are encrypted with the
existing managed-secret store, including verification/reset tokens. Changing
the key without rotating stored secrets prevents pending emails from decrypting.

After backing up the database and completing the prior login-identifier audit,
deploy backend and frontend together. Migration 0074 creates the durable outbox.

```bash
docker compose up -d --build backend frontend
```

No real emails or production configuration changes are made by the local tests.

## Behavior

- Registration requires configured SMTP. Links expire after 24 hours and are
  never returned through the public API or displayed in the UI.
- Password reset returns a generic response for existing and unknown accounts.
  Reset links expire after one hour and are only delivered through email.
- Verification resend is available on the registration page, with a per-account
  one-minute cooldown. Verifying one link consumes all verification links for
  that user; old links cannot demote an already verified/approved account.
- Email-based 2FA uses the queue with a shorter expiry than the challenge.
- Account status changes (including approval, rejection, and suspension) notify
  the account's email address.
- VPS preparation, deployment/reinstall success or failure, power operations,
  name/hostname changes, root-password resets, and HTTP tunnel route changes
  notify the customer owner. Provisioning emails also work without a tunnel.
- Administrative suspension includes the reason; unsuspension is notified.
  A failed shutdown during suspension produces a separate warning email.
- Ownership transfers notify the previous and new customer owners separately,
  without revealing the other customer's identity. Deletion notifications use
  the ownership snapshot and survive deletion of the VPS record.
- Read-only actions, console access, status refreshes, and intermediate events
  do not send emails. Customers and administrators trigger the same lifecycle
  notifications; not every action is labeled as administrator-initiated.
- VPS emails contain neither passwords nor raw command output. Access details
  remain available through the authenticated dashboard.

## Delivery and operations

User/VPS email calls enqueue messages. The worker polls every five seconds and
records `sent` only after SMTP accepts a message; this does not prove inbox
delivery. Other existing cluster emails continue using their direct SMTP path.

Retries use exponential delays capped at 64 minutes, with eight attempts maximum.
Authentication emails expire before their token's lifetime; ordinary notifications
expire after 24 hours. Disabled SMTP leaves messages pending until configuration
is restored (restart the backend) or the message expires. Disabled SMTP never
logs email contents or pretends to deliver messages.

Workers claim rows with leases and SKIP LOCKED for multiple backend instances.
After a crash, expired leases can be retried. Delivery is at-least-once: a crash
after SMTP acceptance but before recording success can cause a duplicate.
Completed/expired payloads are removed from the secret store; metadata remains.

Monitor without printing tokens, addresses, or message bodies:

```bash
docker exec proxacloud-postgres psql -U postgres -d k8s_proxmox -c "SELECT id,status,attempts,created_at,next_attempt_at,expires_at,sent_at,last_error FROM email_outbox ORDER BY id DESC LIMIT 50;"
docker logs --tail 100 proxacloud-backend
```

VPS events distinguish `Customer notification queued` from enqueue failures.
The outbox is the delivery source of truth; the legacy tunnel `emailed_at` field
is no longer updated by queue acceptance. Deleted VPSs have no remaining VPS
event history, so use the independent outbox/logs for delivery diagnostics.

Queue writes follow application operations rather than sharing every operation's
transaction. An enqueue failure or crash in that small interval can lose a
notification; failures are logged (and recorded in VPS events where possible).
Registration reports enqueue failure and supports resend. Reset/resend responses
stay generic to avoid disclosing whether an account exists. There is no delivery
guarantee when the database or SMTP provider is unavailable.

Failed/expired authentication mail requires a new link/code request. After fixing
SMTP, queued unexpired notifications retry automatically; terminal failures do
not automatically restart. Inspect provider logs for rejection or bounce details.

## Verification

Run `go test ./...` in backend and `npm run build` in frontend. Set
TEST_POSTGRES_DSN to a disposable PostgreSQL database to run the outbox and
identity integration tests (each creates and removes only its own test schema).
The outbox test covers retries, expiry, disabled SMTP, cleanup, and lease recovery.
The SMTP transport test uses a loopback fake server and never contacts customers.

Before rollout completion, test registration, resend, reset, approval and a test
customer's VPS actions with your real SMTP provider. Existing exposed tokens or
passwords in historical logs are not removed by this change; review log access
and rotate any credentials known to have been exposed.

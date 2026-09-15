package mailer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lib/pq"
	"proxacloud/backend/internal/secrets"
)

type testTransport struct {
	enabled bool
	fail    bool
	calls   int
}

func (s *testTransport) Enabled() bool { return s.enabled }
func (s *testTransport) Send(to, subject, body string) error {
	s.calls++
	if s.fail {
		return errors.New("temporary SMTP failure")
	}
	return nil
}

func TestQueuePostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	schema := fmt.Sprintf("mail_test_%d", time.Now().UnixNano())
	must := func(query string, args ...any) {
		t.Helper()
		if _, e := db.Exec(query, args...); e != nil {
			t.Fatal(e)
		}
	}
	must("CREATE SCHEMA " + pq.QuoteIdentifier(schema))
	defer db.Exec("DROP SCHEMA " + pq.QuoteIdentifier(schema) + " CASCADE")
	must("SET search_path TO " + pq.QuoteIdentifier(schema))
	must(`CREATE TABLE managed_secrets(id BIGSERIAL PRIMARY KEY,scope TEXT,name TEXT,nonce BYTEA,ciphertext BYTEA)`)
	migration, err := os.ReadFile("../../migrations/0074_email_outbox.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	must(string(migration))
	must(string(migration))
	store, err := secrets.NewDBStore(db, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	transport := &testTransport{enabled: true, fail: true}
	q := NewQueue(db, store, transport)
	if err := q.Send("user@example.com", "Reset", "secret-token"); err != nil {
		t.Fatal(err)
	}
	process := func() {
		t.Helper()
		if _, e := q.process(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	state := func(want string) {
		t.Helper()
		var got string
		if err := db.QueryRow(`SELECT status FROM email_outbox ORDER BY id DESC LIMIT 1`).Scan(&got); err != nil || got != want {
			t.Fatalf("state %s want %s: %v", got, want, err)
		}
	}
	state("pending")
	process()
	state("pending")
	must(`UPDATE email_outbox SET next_attempt_at=NOW()`)
	transport.fail = false
	process()
	state("sent")
	if transport.calls != 2 {
		t.Fatal("retry did not send twice")
	}
	var count int
	_ = db.QueryRow(`SELECT count(*) FROM managed_secrets`).Scan(&count)
	if count != 0 {
		t.Fatal("delivered secret not cleaned up")
	}
	transport.enabled = false
	if err := q.Send("user@example.com", "hello", "body"); err != nil {
		t.Fatal(err)
	}
	process()
	state("pending")
	if transport.calls != 2 {
		t.Fatal("SMTP attempted while disabled")
	}
	must(`UPDATE email_outbox SET expires_at=NOW()-INTERVAL '1 second' WHERE status='pending'`)
	process()
	state("failed")
	if transport.calls != 2 {
		t.Fatal("expired email sent")
	}
	transport.enabled = true
	if err := q.Send("user@example.com", "hello", "body"); err != nil {
		t.Fatal(err)
	}
	must(`UPDATE email_outbox SET status='sending',next_attempt_at=NOW()-INTERVAL '1 second' WHERE status='pending'`)
	process()
	state("sent") // abandoned lease recovered
	transport.fail = true
	if err := q.Send("user@example.com", "hello", "body"); err != nil {
		t.Fatal(err)
	}
	must(`UPDATE email_outbox SET attempts=7 WHERE status='pending'`)
	process()
	state("failed")
	adminMigration, err := os.ReadFile("../../migrations/0075_admin_approval_notifications.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	must(string(adminMigration))
	must(string(adminMigration))
	transport.enabled = false
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := q.SendOnceTx(context.Background(), tx, "approval:42:admin", "admin@example.com", "Approval needed", "customer details", time.Now().Add(time.Hour)); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM email_outbox WHERE deduplication_key IS NOT NULL`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback leaked queue row: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM managed_secrets`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback leaked encrypted payload: %v", err)
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := q.SendOnceTx(context.Background(), tx, "approval:42:admin", "admin@example.com", "Approval needed", "customer details", time.Now().Add(time.Hour)); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM email_outbox WHERE deduplication_key IS NOT NULL`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate queue rows: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM managed_secrets`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate encrypted payload: %v", err)
	}
	process()
	state("pending") // disabling SMTP does not lose an approval request
}

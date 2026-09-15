package users

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestApprovalRecipients(t *testing.T) {
	recipients, invalid := approvalRecipients(" Admin@Example.com,admin@example.com, ,second@example.com,broken,evil@example.com\r\nBcc: other@example.com")
	if strings.Join(recipients, ",") != "admin@example.com,second@example.com" || invalid != 2 {
		t.Fatalf("recipients=%v invalid=%d", recipients, invalid)
	}
}

func TestApprovalMessageAndURL(t *testing.T) {
	page, err := approvalPageURL("https://cloud.proxaops.com/")
	if err != nil || page != "https://cloud.proxaops.com/users" {
		t.Fatalf("page=%s error=%v", page, err)
	}
	body := approvalMessage(User{ID: 42, FullName: "Test Customer", Username: "customer", Email: "customer@example.com"}, time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC), page)
	for _, want := range []string{"Test Customer", "customer@example.com", "2026-09-15T12:00:00Z", page, "waiting for approval", "Sign in"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, invalid := range []string{"", "javascript:alert(1)", "https://user:password@example.com", "example.com"} {
		if _, err := approvalPageURL(invalid); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
}

type approvalCapture struct {
	keys []string
	fail bool
}

func (q *approvalCapture) SendOnceTx(_ context.Context, tx *sql.Tx, key, to, subject, body string, expires time.Time) error {
	if tx == nil {
		return errors.New("missing transaction")
	}
	if q.fail {
		return errors.New("test queue failure")
	}
	q.keys = append(q.keys, key)
	return nil
}

// Called by the PostgreSQL identity integration test inside its isolated schema.
func testApprovalVerification(t *testing.T, db *sql.DB) {
	must := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`CREATE TABLE email_verification_tokens(user_id BIGINT,token_hash TEXT,used_at TIMESTAMPTZ,expires_at TIMESTAMPTZ);
 CREATE TABLE customer_accounts(owner_user_id BIGINT,status TEXT,updated_at TIMESTAMPTZ);
 INSERT INTO users(username,email,full_name,status) VALUES ('verify-customer','verify@example.com','Customer','email_pending');
 INSERT INTO customer_accounts(owner_user_id,status) SELECT id,'email_pending' FROM users WHERE username='verify-customer';`)
	must(`INSERT INTO email_verification_tokens(user_id,token_hash,expires_at) SELECT id,$1,NOW()+INTERVAL '1 hour' FROM users WHERE username='verify-customer'`, hashToken("verify-token"))
	service := NewService(db)
	capture := &approvalCapture{fail: true}
	service.SetAdminApprovalNotifications(capture, "admin@example.com,ADMIN@example.com,second@example.com", "https://cloud.proxaops.com")
	if _, err := service.VerifyEmail("verify-token"); err == nil {
		t.Fatal("queue failure did not rollback verification")
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM users WHERE username='verify-customer'`).Scan(&status); err != nil || status != "email_pending" {
		t.Fatalf("status=%s error=%v", status, err)
	}
	capture.fail = false
	user, err := service.VerifyEmail("verify-token")
	if err != nil || user.Status != StatusPendingApproval {
		t.Fatalf("verify: %v", err)
	}
	if len(capture.keys) != 2 || capture.keys[0] == capture.keys[1] {
		t.Fatalf("keys: %v", capture.keys)
	}
	if _, err := service.VerifyEmail("verify-token"); err == nil {
		t.Fatal("repeat verification succeeded")
	}
	if _, err := service.VerifyEmail("invalid-token"); err == nil {
		t.Fatal("invalid verification succeeded")
	}
	if len(capture.keys) != 2 {
		t.Fatal("repeat/invalid verification notified admin")
	}
	must(`INSERT INTO users(username,email,status) VALUES ('expired-customer','expired@example.com','email_pending')`)
	must(`INSERT INTO email_verification_tokens(user_id,token_hash,expires_at) SELECT id,$1,NOW()-INTERVAL '1 hour' FROM users WHERE username='expired-customer'`, hashToken("expired-token"))
	if _, err := service.VerifyEmail("expired-token"); err == nil {
		t.Fatal("expired verification succeeded")
	}
	if len(capture.keys) != 2 {
		t.Fatal("expired verification notified admin")
	}
	// Missing recipients must not prevent verification.
	service.SetAdminApprovalNotifications(capture, "", "https://cloud.proxaops.com")
	must(`UPDATE email_verification_tokens SET expires_at=NOW()+INTERVAL '1 hour' WHERE token_hash=$1`, hashToken("expired-token"))
	if _, err := service.VerifyEmail("expired-token"); err != nil {
		t.Fatal(err)
	}
	if len(capture.keys) != 2 {
		t.Fatal("empty recipients sent email")
	}
}

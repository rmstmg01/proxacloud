package mailer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"

	"proxacloud/backend/internal/secrets"
)

// Queue.Send means durably queued, not delivered. SMTP acceptance is recorded by the worker.
type Queue struct {
	db        *sql.DB
	secrets   secrets.Store
	transport Sender
}
type message struct{ To, Subject, Body string }

func NewQueue(db *sql.DB, store secrets.Store, transport Sender) *Queue {
	return &Queue{db, store, transport}
}
func (q *Queue) Enabled() bool { return q.transport.Enabled() }
func (q *Queue) Send(to, subject, body string) error {
	return q.SendExpiring(to, subject, body, time.Now().Add(24*time.Hour))
}
func (q *Queue) SendExpiring(to, subject, body string, expires time.Time) error {
	if err := validateMessage(to, subject); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	payload, err := json.Marshal(message{to, subject, body})
	if err != nil {
		return err
	}
	ref, err := q.secrets.Put(ctx, "email-outbox", "message", payload)
	if err != nil {
		return err
	}
	_, err = q.db.ExecContext(ctx, `INSERT INTO email_outbox(payload_ref,expires_at) VALUES ($1,$2)`, ref, expires)
	if err != nil {
		_ = q.secrets.Delete(ctx, ref)
	}
	return err
}

// SendExpiring avoids sending authentication links after their token expires.
func SendExpiring(sender Sender, to, subject, body string, expires time.Time) error {
	if sender == nil {
		return ErrDisabled
	}
	if q, ok := sender.(interface {
		SendExpiring(string, string, string, time.Time) error
	}); ok {
		return q.SendExpiring(to, subject, body, expires)
	}
	return sender.Send(to, subject, body)
}

func (q *Queue) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	if !q.Enabled() {
		log.Print("email delivery disabled; queued messages will wait for SMTP configuration until expiry")
	}
	for {
		if ctx.Err() != nil {
			return
		}
		for i := 0; i < 20; i++ {
			more, err := q.process(ctx)
			if err != nil {
				log.Print("email outbox processing failed; check database/secret-store availability")
				break
			}
			if !more {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (q *Queue) process(ctx context.Context) (bool, error) {
	if err := q.cleanup(ctx); err != nil {
		return false, err
	}
	// Claim a two-minute lease atomically; expired leases recover after a process crash.
	var id int64
	var ref string
	var attempts int
	var expires time.Time
	err := q.db.QueryRowContext(ctx, `UPDATE email_outbox SET status='sending', next_attempt_at=NOW()+INTERVAL '2 minutes'
 WHERE id=(SELECT id FROM email_outbox WHERE status IN ('pending','sending') AND next_attempt_at<=NOW()
 AND ($1 OR expires_at<=NOW()) ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1)
 RETURNING id,payload_ref,attempts,expires_at`, q.Enabled()).Scan(&id, &ref, &attempts, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	status := "failed"
	detail := "expired before delivery"
	if time.Now().Before(expires) {
		attempts++
		raw, e := q.secrets.Get(ctx, ref)
		var msg message
		if e == nil {
			e = json.Unmarshal(raw, &msg)
		}
		if e == nil {
			e = q.transport.Send(msg.To, msg.Subject, msg.Body)
		}
		if e == nil {
			status = "sent"
			detail = ""
		} else {
			detail = "delivery attempt failed; verify SMTP and secret-store configuration"
			if attempts < 8 {
				status = "pending"
			}
			log.Printf("email outbox id=%d attempt=%d delivery failed", id, attempts)
		}
	}
	delay := time.Duration(1<<min(attempts, 6)) * time.Minute
	_, err = q.db.ExecContext(ctx, `UPDATE email_outbox SET status=$2,attempts=$3,last_error=$4,
 next_attempt_at=$5,sent_at=CASE WHEN $2='sent' THEN NOW() ELSE NULL END WHERE id=$1`, id, status, attempts, detail, time.Now().Add(delay))
	if err != nil {
		return true, err
	}
	// Remove sensitive contents after completion; delivery metadata remains for audit.
	if status == "sent" || status == "failed" {
		return true, q.cleanup(ctx)
	}
	return true, nil
}

func (q *Queue) cleanup(ctx context.Context) error {
	rows, err := q.db.QueryContext(ctx, `SELECT id,payload_ref FROM email_outbox WHERE status IN ('sent','failed') AND payload_ref<>'' LIMIT 20`)
	if err != nil {
		return err
	}
	type payload struct {
		id  int64
		ref string
	}
	var payloads []payload
	for rows.Next() {
		var p payload
		if err := rows.Scan(&p.id, &p.ref); err != nil {
			rows.Close()
			return err
		}
		payloads = append(payloads, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range payloads {
		if err := q.secrets.Delete(ctx, p.ref); err != nil {
			return err
		}
		if _, err := q.db.ExecContext(ctx, `UPDATE email_outbox SET payload_ref='' WHERE id=$1`, p.id); err != nil {
			return err
		}
	}
	return nil
}

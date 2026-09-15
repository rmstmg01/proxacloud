package users

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

type approvalEmailQueue interface {
	SendOnceTx(context.Context, *sql.Tx, string, string, string, string, time.Time) error
}

func approvalRecipients(raw string) ([]string, int) {
	var recipients []string
	invalid := 0
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		address := strings.ToLower(strings.TrimSpace(part))
		if address == "" {
			continue
		}
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address || strings.ContainsAny(address, "\r\n") {
			invalid++
			continue
		}
		if !seen[address] {
			seen[address] = true
			recipients = append(recipients, address)
		}
	}
	return recipients, invalid
}

func approvalPageURL(base string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("PUBLIC_APP_URL must be an absolute HTTP(S) URL")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/users"
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func (s *Service) SetAdminApprovalNotifications(queue approvalEmailQueue, recipients, appURL string) {
	s.approvalQueue = queue
	var invalid int
	s.approvalRecipients, invalid = approvalRecipients(recipients)
	if invalid > 0 {
		log.Printf("admin approval notifications: ignored %d invalid recipient entries", invalid)
	}
	var err error
	s.approvalURL, err = approvalPageURL(appURL)
	if err != nil {
		log.Print("admin approval notifications disabled: invalid PUBLIC_APP_URL")
		s.approvalRecipients = nil
	}
	if len(s.approvalRecipients) == 0 {
		log.Print("admin approval notifications disabled: configure ADMIN_NOTIFICATION_EMAILS and PUBLIC_APP_URL")
	}
}

func approvalMessage(user User, verified time.Time, pageURL string) string {
	return fmt.Sprintf("A customer has signed up, verified their email address, and is waiting for approval.\n\nName: %s\nUsername: %s\nEmail: %s\nUser ID: %d\nVerified at: %s\n\nReview the account:\n%s\n\nSign in with an administrator account authorized to manage users. This link does not approve the account automatically.",
		user.FullName, user.Username, user.Email, user.ID, verified.UTC().Format(time.RFC3339), pageURL)
}

func (s *Service) queueAdminApproval(tx *sql.Tx, id int64) error {
	if len(s.approvalRecipients) == 0 || s.approvalQueue == nil {
		log.Printf("admin approval notification skipped user_id=%d: recipients/queue not configured", id)
		return nil
	}
	var user User
	var verified time.Time
	if err := tx.QueryRow(`SELECT id,username,full_name,email,email_verified_at FROM users WHERE id=$1`, id).Scan(&user.ID, &user.Username, &user.FullName, &user.Email, &verified); err != nil {
		return err
	}
	body := approvalMessage(user, verified, s.approvalURL)
	for _, recipient := range s.approvalRecipients {
		key := fmt.Sprintf("signup-pending-approval:%d:%x", id, sha256.Sum256([]byte(recipient)))
		if err := s.approvalQueue.SendOnceTx(context.Background(), tx, key, recipient, "New ProxaCloud account awaiting approval", body, verified.Add(24*time.Hour)); err != nil {
			return err
		}
	}
	return nil
}

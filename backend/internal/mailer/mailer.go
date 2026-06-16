package mailer

import (
	"fmt"
	"log"
	"net/smtp"
	"strings"
)

type Sender interface {
	Send(to, subject, body string) error
	Enabled() bool
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

type SMTPMailer struct {
	cfg SMTPConfig
}

func NewSMTPMailer(cfg SMTPConfig) *SMTPMailer {
	return &SMTPMailer{cfg: cfg}
}

func (m *SMTPMailer) Enabled() bool {
	return strings.TrimSpace(m.cfg.Host) != "" && strings.TrimSpace(m.cfg.From) != ""
}

func (m *SMTPMailer) Send(to, subject, body string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return nil
	}
	if !m.Enabled() {
		log.Printf("smtp not configured; email to=%s subject=%q body=%q", to, subject, body)
		return nil
	}

	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	headers := []string{
		"From: " + m.cfg.From,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	message := strings.Join(headers, "\r\n") + "\r\n\r\n" + body

	var auth smtp.Auth
	if strings.TrimSpace(m.cfg.Username) != "" {
		auth = smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	}
	return smtp.SendMail(addr, auth, m.cfg.From, []string{to}, []byte(message))
}

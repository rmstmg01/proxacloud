package mailer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

type Sender interface {
	Send(to, subject, body string) error
	Enabled() bool
}

var ErrDisabled = errors.New("email delivery is not configured")

func validateMessage(to, subject string) error {
	address, err := mail.ParseAddress(to)
	if err != nil || address.Address != to || strings.ContainsAny(to+subject, "\r\n") {
		return errors.New("invalid email recipient or subject")
	}
	return nil
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
	if err := validateMessage(to, subject); err != nil {
		return err
	}
	if !m.Enabled() {
		return ErrDisabled
	}
	if err := validateMessage(m.cfg.From, subject); err != nil {
		return err
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
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tlsConfig := &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	var err error
	if m.cfg.Port == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if m.cfg.Port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return err
			}
		} else if auth != nil {
			return errors.New("SMTP authentication requires TLS")
		}
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(m.cfg.From); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(message)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

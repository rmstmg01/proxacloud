package mailer

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDisabledSMTPDoesNotLogSecrets(t *testing.T) {
	var output bytes.Buffer
	old := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(old)
	err := NewSMTPMailer(SMTPConfig{}).Send("user@example.com", "Reset password", "secret-reset-token")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected disabled error: %v", err)
	}
	if output.Len() != 0 {
		t.Fatal("disabled SMTP logged email contents")
	}
}

func TestRejectHeaderInjection(t *testing.T) {
	for _, tc := range []struct{ to, subject string }{
		{"user@example.com\r\nBcc: attacker@example.com", "hello"},
		{"user@example.com", "hello\nBcc: attacker@example.com"},
		{"", "hello"},
	} {
		if validateMessage(tc.to, tc.subject) == nil {
			t.Fatal("invalid message accepted")
		}
	}
}

func TestSMTPAcceptance(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan string, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			received <- ""
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		fmt.Fprint(conn, "220 localhost ESMTP\r\n")
		reader := bufio.NewReader(conn)
		var body strings.Builder
		for {
			line, e := reader.ReadString('\n')
			if e != nil {
				received <- ""
				return
			}
			switch {
			case strings.HasPrefix(line, "DATA"):
				fmt.Fprint(conn, "354 send data\r\n")
				for {
					part, e := reader.ReadString('\n')
					if e != nil {
						received <- ""
						return
					}
					if part == ".\r\n" {
						break
					}
					body.WriteString(part)
				}
				fmt.Fprint(conn, "250 accepted\r\n")
			case strings.HasPrefix(line, "QUIT"):
				fmt.Fprint(conn, "221 bye\r\n")
				received <- body.String()
				return
			default:
				fmt.Fprint(conn, "250 OK\r\n")
			}
		}
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	p, _ := strconv.Atoi(port)
	err = NewSMTPMailer(SMTPConfig{Host: "127.0.0.1", Port: p, From: "noreply@example.com"}).Send("user@example.com", "Verification", "link sent only through SMTP")
	if err != nil {
		t.Fatal(err)
	}
	if body := <-received; !strings.Contains(body, "link sent only through SMTP") {
		t.Fatal("email not received")
	}
}

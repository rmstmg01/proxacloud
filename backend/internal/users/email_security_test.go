package users

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proxacloud/backend/internal/mailer"
)

func TestRegistrationDisabledSMTP(t *testing.T) {
	h := NewHandler(nil, nil, mailer.NewSMTPMailer(mailer.SMTPConfig{}), "https://cloud.proxaops.com")
	w := httptest.NewRecorder()
	h.Register(w, httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(`{"username":"user","email":"user@example.com","password":"password123"}`)))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "verification_url") {
		t.Fatal("verification link exposed")
	}
}

func TestRegistrationResultHidesToken(t *testing.T) {
	raw, err := json.Marshal(RegistrationResult{VerificationURL: "secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-token") {
		t.Fatal("registration result serialized token")
	}
}

func TestPasswordResetResultHidesToken(t *testing.T) {
	raw, err := json.Marshal(PasswordResetRequestResult{ResetURL: "secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-token") {
		t.Fatal("reset result serialized token")
	}
}

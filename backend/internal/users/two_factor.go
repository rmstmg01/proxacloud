package users

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	"proxacloud/backend/internal/auth"
	"proxacloud/backend/internal/rbac"
)

const (
	twoFactorMethodNone  = "none"
	twoFactorMethodEmail = "email"
	twoFactorMethodTOTP  = "totp"

	twoFactorPurposeLogin       = "login"
	twoFactorPurposeEnableEmail = "enable_email"

	twoFactorChallengeTTL   = 10 * time.Minute
	twoFactorMaxAttempts    = 5
	twoFactorTotpIssuerName = "ProxaCloud"
)

type twoFactorChallenge struct {
	ID             int64
	UserID         int64
	Purpose        string
	Method         string
	ChallengeToken string
	CodeHash       string
	Attempts       int
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
}

func effectiveTwoFactorMethod(user User) string {
	method := strings.ToLower(strings.TrimSpace(user.TwoFactorMethod))
	switch method {
	case twoFactorMethodEmail, twoFactorMethodTOTP:
		return method
	}
	if user.TwoFactorEnabled {
		if strings.TrimSpace(user.TwoFactorSecret) != "" {
			return twoFactorMethodTOTP
		}
		return twoFactorMethodEmail
	}
	return twoFactorMethodNone
}

func randomDigits(n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", n)
	}
	for i := range buf {
		buf[i] = '0' + (buf[i] % 10)
	}
	return string(buf)
}

func randomToken(n int) string {
	if n <= 0 {
		n = 32
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func hash2FACode(code string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(strings.TrimSpace(code)), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func clientIP(r *http.Request) string {
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		value := strings.TrimSpace(r.Header.Get(header))
		if value == "" {
			continue
		}
		if idx := strings.Index(value, ","); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
		return value
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Service) create2FAChallenge(userID int64, purpose, method, code, ip, userAgent string) (twoFactorChallenge, error) {
	_, _ = s.db.Exec(
		`DELETE FROM user_2fa_challenges
		  WHERE user_id = $1 AND purpose = $2 AND consumed_at IS NULL`,
		userID,
		purpose,
	)

	codeHash := ""
	if strings.TrimSpace(code) != "" {
		hashed, err := hash2FACode(code)
		if err != nil {
			return twoFactorChallenge{}, err
		}
		codeHash = hashed
	}

	challenge := twoFactorChallenge{
		UserID:         userID,
		Purpose:        purpose,
		Method:         method,
		ChallengeToken: randomToken(32),
		CodeHash:       codeHash,
		ExpiresAt:      time.Now().UTC().Add(twoFactorChallengeTTL),
	}
	err := s.db.QueryRow(
		`INSERT INTO user_2fa_challenges
		    (user_id, purpose, method, challenge_token, code_hash, expires_at, ip_address, user_agent)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, attempts`,
		userID,
		purpose,
		method,
		challenge.ChallengeToken,
		challenge.CodeHash,
		challenge.ExpiresAt,
		ip,
		userAgent,
	).Scan(&challenge.ID, &challenge.Attempts)
	if err != nil {
		return twoFactorChallenge{}, err
	}
	return challenge, nil
}

func (s *Service) get2FAChallenge(token, purpose string) (twoFactorChallenge, error) {
	var challenge twoFactorChallenge
	err := s.db.QueryRow(
		`SELECT id, user_id, purpose, method, challenge_token, code_hash, attempts, expires_at, consumed_at
		   FROM user_2fa_challenges
		  WHERE challenge_token = $1 AND purpose = $2`,
		strings.TrimSpace(token),
		purpose,
	).Scan(&challenge.ID, &challenge.UserID, &challenge.Purpose, &challenge.Method, &challenge.ChallengeToken, &challenge.CodeHash, &challenge.Attempts, &challenge.ExpiresAt, &challenge.ConsumedAt)
	return challenge, err
}

func (s *Service) consume2FAChallenge(id int64) {
	_, _ = s.db.Exec(`UPDATE user_2fa_challenges SET consumed_at = NOW(), updated_at = NOW() WHERE id = $1`, id)
}

func (s *Service) increment2FAAttempts(id int64) {
	_, _ = s.db.Exec(`UPDATE user_2fa_challenges SET attempts = attempts + 1, updated_at = NOW() WHERE id = $1`, id)
}

func (s *Service) refreshEmail2FAChallenge(id int64, code, ip, userAgent string) error {
	hash, err := hash2FACode(code)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`UPDATE user_2fa_challenges
		    SET code_hash = $2, attempts = 0, expires_at = $3, ip_address = $4, user_agent = $5, updated_at = NOW()
		  WHERE id = $1`,
		id,
		hash,
		time.Now().UTC().Add(twoFactorChallengeTTL),
		ip,
		userAgent,
	)
	return err
}

func (s *Service) setLastLogin(userID int64, ip string) {
	_, _ = s.db.Exec(`UPDATE users SET last_login_ip = $2 WHERE id = $1`, userID, ip)
}

func (s *Service) getByIDPrivate(id int64) (User, bool) {
	var user User
	var roleName string
	err := s.db.QueryRow(
		`SELECT u.id, u.username, u.first_name, u.last_name, u.full_name, u.email, u.contact_number, u.status, u.email_verified_at, u.created_at, r.name,
		        u.two_factor_enabled, COALESCE(u.two_factor_method, 'none'), COALESCE(u.two_factor_secret, ''),
		        COALESCE(u.two_factor_pending_secret, ''), u.two_factor_enabled_at, COALESCE(u.last_login_ip, '')
		   FROM users u
		   JOIN roles r ON r.id = u.role_id
		  WHERE u.id = $1`,
		id,
	).Scan(&user.ID, &user.Username, &user.FirstName, &user.LastName, &user.FullName, &user.Email, &user.ContactNumber, &user.Status, &user.EmailVerifiedAt, &user.CreatedAt, &roleName, &user.TwoFactorEnabled, &user.TwoFactorMethod, &user.TwoFactorSecret, &user.TwoFactorPendingSecret, &user.TwoFactorEnabledAt, &user.LastLoginIP)
	if err != nil {
		return User{}, false
	}
	user.Role = rbac.Role(roleName)
	return user, true
}

func (s *Service) getByUsernamePrivate(username string) (User, bool) {
	username = strings.TrimSpace(strings.ToLower(username))
	var user User
	var roleName string
	err := s.db.QueryRow(
		`SELECT u.id, u.username, u.first_name, u.last_name, u.full_name, u.email, u.contact_number, u.status, u.email_verified_at, u.created_at, r.name,
		        u.two_factor_enabled, COALESCE(u.two_factor_method, 'none'), COALESCE(u.two_factor_secret, ''),
		        COALESCE(u.two_factor_pending_secret, ''), u.two_factor_enabled_at, COALESCE(u.last_login_ip, '')
		   FROM users u
		   JOIN roles r ON r.id = u.role_id
		  WHERE u.username = $1`,
		username,
	).Scan(&user.ID, &user.Username, &user.FirstName, &user.LastName, &user.FullName, &user.Email, &user.ContactNumber, &user.Status, &user.EmailVerifiedAt, &user.CreatedAt, &roleName, &user.TwoFactorEnabled, &user.TwoFactorMethod, &user.TwoFactorSecret, &user.TwoFactorPendingSecret, &user.TwoFactorEnabledAt, &user.LastLoginIP)
	if err != nil {
		return User{}, false
	}
	user.Role = rbac.Role(roleName)
	return user, true
}

func (s *Service) verifyCurrentPassword(userID int64, password string) error {
	var hash string
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash)
	if err != nil {
		if errorsIsNoRows(err) {
			return ErrUserNotFound
		}
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return ErrInvalidCredentials
	}
	return nil
}

func errorsIsNoRows(err error) bool {
	return err == sql.ErrNoRows
}

func verify2FAChallengeCode(challenge twoFactorChallenge, code, secret string) bool {
	switch challenge.Method {
	case twoFactorMethodEmail:
		return challenge.CodeHash != "" && bcrypt.CompareHashAndPassword([]byte(challenge.CodeHash), []byte(strings.TrimSpace(code))) == nil
	case twoFactorMethodTOTP:
		return strings.TrimSpace(secret) != "" && totp.Validate(strings.TrimSpace(code), secret)
	default:
		return false
	}
}

func (h *Handler) send2FAEmail(user User, code, purpose string) {
	if h.mailer == nil {
		return
	}
	subject := "Your ProxaCloud verification code"
	if purpose == twoFactorPurposeEnableEmail {
		subject = "Confirm email-based 2FA setup"
	}
	name := strings.TrimSpace(user.FullName)
	if name == "" {
		name = user.Username
	}
	body := fmt.Sprintf("Hello %s,\n\nYour ProxaCloud verification code is: %s\n\nThis code expires in 10 minutes.\n\nIf this was not you, please secure your account immediately.", name, code)
	_ = h.mailer.Send(user.Email, subject, body)
}

func (h *Handler) beginLoginTwoFactor(w http.ResponseWriter, r *http.Request, user User) (bool, error) {
	method := effectiveTwoFactorMethod(user)
	if method == twoFactorMethodNone {
		return false, nil
	}
	if method == twoFactorMethodEmail && strings.TrimSpace(user.Email) == "" {
		return true, fmt.Errorf("email address is required for email-based 2FA")
	}

	code := ""
	if method == twoFactorMethodEmail {
		code = randomDigits(6)
	}
	challenge, err := h.users.create2FAChallenge(user.ID, twoFactorPurposeLogin, method, code, clientIP(r), r.UserAgent())
	if err != nil {
		return true, err
	}
	if method == twoFactorMethodEmail {
		go h.send2FAEmail(user, code, twoFactorPurposeLogin)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"requires_2fa":    true,
		"challenge_token": challenge.ChallengeToken,
		"method":          method,
		"message":         "Two-factor verification required",
	})
	return true, nil
}

func (h *Handler) Login2FAVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChallengeToken string `json:"challenge_token"`
		Code           string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	challenge, err := h.users.get2FAChallenge(req.ChallengeToken, twoFactorPurposeLogin)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid login challenge"})
		return
	}
	if challenge.ConsumedAt != nil || time.Now().After(challenge.ExpiresAt) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "login challenge expired or already used"})
		return
	}
	if challenge.Attempts >= twoFactorMaxAttempts {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many incorrect attempts. Please login again to request a new code."})
		return
	}

	user, exists := h.users.getByIDPrivate(challenge.UserID)
	if !exists {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user not found"})
		return
	}
	if !verify2FAChallengeCode(challenge, req.Code, user.TwoFactorSecret) {
		h.users.increment2FAAttempts(challenge.ID)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid verification code"})
		return
	}

	h.users.consume2FAChallenge(challenge.ID)
	h.users.setLastLogin(user.ID, clientIP(r))
	token, err := h.tokens.Issue(user.Username, user.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue token"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": sanitizeUser(user)})
}

func (h *Handler) Login2FAResendEmailCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChallengeToken string `json:"challenge_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	challenge, err := h.users.get2FAChallenge(req.ChallengeToken, twoFactorPurposeLogin)
	if err != nil || challenge.Method != twoFactorMethodEmail {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email verification challenge not found"})
		return
	}
	if challenge.ConsumedAt != nil || time.Now().After(challenge.ExpiresAt) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "challenge expired"})
		return
	}
	user, exists := h.users.GetByID(challenge.UserID)
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	code := randomDigits(6)
	if err := h.users.refreshEmail2FAChallenge(challenge.ID, code, clientIP(r), r.UserAgent()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to refresh challenge"})
		return
	}
	go h.send2FAEmail(user, code, twoFactorPurposeLogin)
	writeJSON(w, http.StatusOK, map[string]string{"message": "verification code sent"})
}

func (h *Handler) TwoFactorStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := h.currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	method := effectiveTwoFactorMethod(user)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": method != twoFactorMethodNone,
		"method":  method,
	})
}

func (h *Handler) EnableTwoFactorEmailRequest(w http.ResponseWriter, r *http.Request) {
	user, ok := h.currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if strings.TrimSpace(user.Email) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "an email address is required for email-based 2FA"})
		return
	}
	if err := h.users.verifyCurrentPassword(user.ID, req.CurrentPassword); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current password is incorrect"})
		return
	}
	code := randomDigits(6)
	challenge, err := h.users.create2FAChallenge(user.ID, twoFactorPurposeEnableEmail, twoFactorMethodEmail, code, clientIP(r), r.UserAgent())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create verification challenge"})
		return
	}
	go h.send2FAEmail(user, code, twoFactorPurposeEnableEmail)
	writeJSON(w, http.StatusOK, map[string]any{
		"challenge_token": challenge.ChallengeToken,
		"message":         "Verification code sent to your email",
	})
}

func (h *Handler) EnableTwoFactorEmailConfirm(w http.ResponseWriter, r *http.Request) {
	user, ok := h.currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		ChallengeToken string `json:"challenge_token"`
		Code           string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	challenge, err := h.users.get2FAChallenge(req.ChallengeToken, twoFactorPurposeEnableEmail)
	if err != nil || challenge.UserID != user.ID {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid verification challenge"})
		return
	}
	if challenge.ConsumedAt != nil || time.Now().After(challenge.ExpiresAt) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "verification challenge expired"})
		return
	}
	if challenge.Attempts >= twoFactorMaxAttempts {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts"})
		return
	}
	if !verify2FAChallengeCode(challenge, req.Code, "") {
		h.users.increment2FAAttempts(challenge.ID)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid verification code"})
		return
	}
	if _, err := h.users.db.Exec(
		`UPDATE users
		    SET two_factor_enabled = TRUE, two_factor_method = $2, two_factor_secret = '',
		        two_factor_pending_secret = '', two_factor_enabled_at = NOW()
		  WHERE id = $1`,
		user.ID,
		twoFactorMethodEmail,
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to enable email 2FA"})
		return
	}
	h.users.consume2FAChallenge(challenge.ID)
	writeJSON(w, http.StatusOK, map[string]string{"message": "Email 2FA enabled successfully"})
}

func (h *Handler) SetupTwoFactorTOTP(w http.ResponseWriter, r *http.Request) {
	user, ok := h.currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if err := h.users.verifyCurrentPassword(user.ID, req.CurrentPassword); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current password is incorrect"})
		return
	}

	accountName := strings.TrimSpace(user.Email)
	if accountName == "" {
		accountName = user.Username
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      twoFactorTotpIssuerName,
		AccountName: accountName,
		Algorithm:   otp.AlgorithmSHA1,
		Digits:      otp.DigitsSix,
		Period:      30,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate authenticator setup"})
		return
	}
	if _, err := h.users.db.Exec(`UPDATE users SET two_factor_pending_secret = $2 WHERE id = $1`, user.ID, key.Secret()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save pending authenticator setup"})
		return
	}
	img, err := key.Image(220, 220)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate QR image"})
		return
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encode QR image"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"otpauth_url": key.URL(),
		"secret":      key.Secret(),
		"qr_code":     "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBuf.Bytes()),
		"message":     "Scan QR code and verify the generated code to enable authenticator 2FA",
	})
}

func (h *Handler) ConfirmTwoFactorTOTP(w http.ResponseWriter, r *http.Request) {
	user, ok := h.currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	secret := strings.TrimSpace(user.TwoFactorPendingSecret)
	if secret == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no pending authenticator setup found"})
		return
	}
	if !totp.Validate(strings.TrimSpace(req.Code), secret) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid authenticator code"})
		return
	}
	if _, err := h.users.db.Exec(
		`UPDATE users
		    SET two_factor_enabled = TRUE, two_factor_method = $2, two_factor_secret = $3,
		        two_factor_pending_secret = '', two_factor_enabled_at = NOW()
		  WHERE id = $1`,
		user.ID,
		twoFactorMethodTOTP,
		secret,
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to enable authenticator 2FA"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Authenticator 2FA enabled successfully"})
}

func (h *Handler) DisableTwoFactor(w http.ResponseWriter, r *http.Request) {
	user, ok := h.currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		Code            string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if err := h.users.verifyCurrentPassword(user.ID, req.CurrentPassword); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current password is incorrect"})
		return
	}
	if effectiveTwoFactorMethod(user) == twoFactorMethodTOTP && strings.TrimSpace(user.TwoFactorSecret) != "" {
		if strings.TrimSpace(req.Code) == "" || !totp.Validate(strings.TrimSpace(req.Code), user.TwoFactorSecret) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authenticator code is required to disable 2FA"})
			return
		}
	}
	if _, err := h.users.db.Exec(
		`UPDATE users
		    SET two_factor_enabled = FALSE, two_factor_method = $2, two_factor_secret = '',
		        two_factor_pending_secret = '', two_factor_enabled_at = NULL
		  WHERE id = $1`,
		user.ID,
		twoFactorMethodNone,
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to disable 2FA"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Two-factor authentication disabled"})
}

func (h *Handler) currentUser(r *http.Request) (User, bool) {
	ctxUser, ok := auth.UserFromContext(r.Context())
	if !ok {
		return User{}, false
	}
	return h.users.getByUsernamePrivate(ctxUser.Username)
}

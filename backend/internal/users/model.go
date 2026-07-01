package users

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"proxacloud/backend/internal/rbac"
	"proxacloud/backend/internal/tenant"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserExists         = errors.New("user already exists")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidRole        = errors.New("invalid role")
	ErrAccountInactive    = errors.New("customer account is not active")
	ErrDeleteConfirmation = errors.New("user delete confirmation does not match")
	ErrDeleteSelf         = errors.New("you cannot delete your own user")
	ErrLastSuperAdmin     = errors.New("cannot delete the last super admin")
)

type Status string

const (
	StatusEmailPending    Status = "email_pending"
	StatusPendingApproval Status = "pending_approval"
	StatusActive          Status = "active"
	StatusSuspended       Status = "suspended"
	StatusRejected        Status = "rejected"
)

type User struct {
	ID                     int64      `json:"id"`
	Username               string     `json:"username"`
	FullName               string     `json:"full_name"`
	Email                  string     `json:"email"`
	ContactNumber          string     `json:"contact_number"`
	Role                   rbac.Role  `json:"role"`
	Status                 Status     `json:"status"`
	EmailVerifiedAt        *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	TwoFactorEnabled       bool       `json:"two_factor_enabled"`
	TwoFactorMethod        string     `json:"two_factor_method"`
	TwoFactorEnabledAt     *time.Time `json:"two_factor_enabled_at,omitempty"`
	PasswordHash           string     `json:"-"`
	TwoFactorSecret        string     `json:"-"`
	TwoFactorPendingSecret string     `json:"-"`
	LastLoginIP            string     `json:"-"`
}

type ProfileInput struct {
	FullName      string
	Email         string
	ContactNumber string
}

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) SeedSuperAdmin(username, password string) error {
	if _, exists := s.GetByUsername(username); exists {
		return nil
	}

	_, err := s.CreateUser(username, password, rbac.RoleSuperAdmin, ProfileInput{})
	if err != nil && !errors.Is(err, ErrUserExists) {
		return err
	}
	return nil
}

func (s *Service) CreateUser(username, password string, role rbac.Role, profile ProfileInput) (User, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	profile.FullName = strings.TrimSpace(profile.FullName)
	profile.Email = strings.TrimSpace(strings.ToLower(profile.Email))
	profile.ContactNumber = strings.TrimSpace(profile.ContactNumber)
	if username == "" || len(password) < 8 {
		return User{}, errors.New("username required and password must have at least 8 characters")
	}
	if !rbac.IsValidRole(role) {
		return User{}, ErrInvalidRole
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	roleID, err := s.getRoleID(role)
	if err != nil {
		return User{}, err
	}

	var created User
	err = s.db.QueryRow(
		`INSERT INTO users (username, full_name, email, contact_number, password_hash, role_id, status, email_verified_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 'active', CASE WHEN NULLIF($3, '') IS NULL THEN NULL ELSE NOW() END)
		 RETURNING id, username, full_name, email, contact_number, status, email_verified_at, created_at`,
		username,
		profile.FullName,
		profile.Email,
		profile.ContactNumber,
		string(hash),
		roleID,
	).Scan(&created.ID, &created.Username, &created.FullName, &created.Email, &created.ContactNumber, &created.Status, &created.EmailVerifiedAt, &created.CreatedAt)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			return User{}, ErrUserExists
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}

	created.Role = role
	return sanitizeUser(created), nil
}

type RegistrationResult struct {
	User            User   `json:"user"`
	VerificationURL string `json:"verification_url,omitempty"`
}

func (s *Service) RegisterClient(username, password string, profile ProfileInput, publicAppURL string) (RegistrationResult, error) {
	username = strings.TrimSpace(strings.ToLower(username))
	profile.FullName = strings.TrimSpace(profile.FullName)
	profile.Email = strings.TrimSpace(strings.ToLower(profile.Email))
	profile.ContactNumber = strings.TrimSpace(profile.ContactNumber)
	if profile.Email == "" {
		return RegistrationResult{}, errors.New("email is required")
	}
	if username == "" || len(password) < 8 {
		return RegistrationResult{}, errors.New("username required and password must have at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return RegistrationResult{}, err
	}
	roleID, err := s.getRoleID(rbac.RoleNormalUser)
	if err != nil {
		return RegistrationResult{}, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return RegistrationResult{}, err
	}
	defer tx.Rollback()

	var created User
	err = tx.QueryRow(
		`INSERT INTO users (username, full_name, email, contact_number, password_hash, role_id, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, username, full_name, email, contact_number, status, email_verified_at, created_at`,
		username, profile.FullName, profile.Email, profile.ContactNumber, string(hash), roleID, string(StatusEmailPending),
	).Scan(&created.ID, &created.Username, &created.FullName, &created.Email, &created.ContactNumber, &created.Status, &created.EmailVerifiedAt, &created.CreatedAt)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			return RegistrationResult{}, ErrUserExists
		}
		return RegistrationResult{}, fmt.Errorf("insert registered user: %w", err)
	}

	var accountID int64
	accountName := profile.FullName
	if accountName == "" {
		accountName = username
	}
	err = tx.QueryRow(
		`INSERT INTO customer_accounts (name, owner_user_id, status)
		 VALUES ($1, $2, $3)
		 RETURNING id`,
		accountName, created.ID, string(StatusEmailPending),
	).Scan(&accountID)
	if err != nil {
		return RegistrationResult{}, fmt.Errorf("insert customer account: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO customer_account_users (account_id, user_id, role) VALUES ($1, $2, 'owner')`, accountID, created.ID); err != nil {
		return RegistrationResult{}, fmt.Errorf("link customer account user: %w", err)
	}

	token, tokenHash, err := newVerificationToken()
	if err != nil {
		return RegistrationResult{}, err
	}
	if _, err := tx.Exec(
		`INSERT INTO email_verification_tokens (user_id, token_hash, expires_at)
		 VALUES ($1, $2, NOW() + INTERVAL '24 hours')`,
		created.ID, tokenHash,
	); err != nil {
		return RegistrationResult{}, fmt.Errorf("store email verification token: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return RegistrationResult{}, fmt.Errorf("commit registration: %w", err)
	}
	created.Role = rbac.RoleNormalUser
	return RegistrationResult{User: sanitizeUser(created), VerificationURL: verificationURL(publicAppURL, token)}, nil
}

func (s *Service) VerifyEmail(token string) (User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return User{}, errors.New("verification token is required")
	}
	tokenHash := hashToken(token)

	tx, err := s.db.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()

	var userID int64
	err = tx.QueryRow(
		`SELECT user_id
		   FROM email_verification_tokens
		  WHERE token_hash = $1
		    AND used_at IS NULL
		    AND expires_at > NOW()
		  FOR UPDATE`,
		tokenHash,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, errors.New("verification link is invalid or expired")
	}
	if err != nil {
		return User{}, fmt.Errorf("load verification token: %w", err)
	}

	if _, err := tx.Exec(`UPDATE email_verification_tokens SET used_at = NOW() WHERE token_hash = $1`, tokenHash); err != nil {
		return User{}, fmt.Errorf("mark verification token used: %w", err)
	}
	if _, err := tx.Exec(`UPDATE users SET status = $2, email_verified_at = NOW() WHERE id = $1`, userID, string(StatusPendingApproval)); err != nil {
		return User{}, fmt.Errorf("mark user verified: %w", err)
	}
	if _, err := tx.Exec(`UPDATE customer_accounts SET status = $2, updated_at = NOW() WHERE owner_user_id = $1`, userID, string(StatusPendingApproval)); err != nil {
		return User{}, fmt.Errorf("mark customer account pending: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("commit email verification: %w", err)
	}

	user, ok := s.GetByID(userID)
	if !ok {
		return User{}, ErrUserNotFound
	}
	return user, nil
}

func (s *Service) UpdateUser(id int64, role rbac.Role, profile ProfileInput) (User, error) {
	if id <= 0 {
		return User{}, ErrUserNotFound
	}
	if !rbac.IsValidRole(role) {
		return User{}, ErrInvalidRole
	}
	profile.FullName = strings.TrimSpace(profile.FullName)
	profile.Email = strings.TrimSpace(strings.ToLower(profile.Email))
	profile.ContactNumber = strings.TrimSpace(profile.ContactNumber)

	roleID, err := s.getRoleID(role)
	if err != nil {
		return User{}, err
	}

	var updated User
	var roleName string
	err = s.db.QueryRow(
		`UPDATE users
		 SET full_name = $2,
		     email = $3,
		     contact_number = $4,
		     role_id = $5
		 WHERE id = $1
		 RETURNING id, username, full_name, email, contact_number, status, email_verified_at, created_at,
		           (SELECT name FROM roles WHERE id = $5)`,
		id,
		profile.FullName,
		profile.Email,
		profile.ContactNumber,
		roleID,
	).Scan(&updated.ID, &updated.Username, &updated.FullName, &updated.Email, &updated.ContactNumber, &updated.Status, &updated.EmailVerifiedAt, &updated.CreatedAt, &roleName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("update user: %w", err)
	}

	updated.Role = rbac.Role(roleName)
	return sanitizeUser(updated), nil
}

func (s *Service) SetStatus(id int64, status Status, actorUsername string, reason string) (User, error) {
	if !isValidStatus(status) {
		return User{}, fmt.Errorf("invalid status %q", status)
	}
	if id <= 0 {
		return User{}, ErrUserNotFound
	}
	var actorID sql.NullInt64
	if actorUsername != "" {
		_ = s.db.QueryRow(`SELECT id FROM users WHERE username = $1`, strings.TrimSpace(strings.ToLower(actorUsername))).Scan(&actorID)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET status = $2 WHERE id = $1`, id, string(status)); err != nil {
		return User{}, fmt.Errorf("update user status: %w", err)
	}
	switch status {
	case StatusActive:
		_, err = tx.Exec(`UPDATE customer_accounts SET status = $2, approved_by = $3, approved_at = NOW(), rejected_reason = '', updated_at = NOW() WHERE owner_user_id = $1`, id, string(status), actorID)
	case StatusRejected:
		_, err = tx.Exec(`UPDATE customer_accounts SET status = $2, rejected_reason = $3, updated_at = NOW() WHERE owner_user_id = $1`, id, string(status), strings.TrimSpace(reason))
	default:
		_, err = tx.Exec(`UPDATE customer_accounts SET status = $2, updated_at = NOW() WHERE owner_user_id = $1`, id, string(status))
	}
	if err != nil {
		return User{}, fmt.Errorf("update customer account status: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	user, ok := s.GetByID(id)
	if !ok {
		return User{}, ErrUserNotFound
	}
	return user, nil
}

func (s *Service) DeleteUser(id int64, confirmationUsername string, actorUsername string) error {
	if id <= 0 {
		return ErrUserNotFound
	}
	target, ok := s.GetByID(id)
	if !ok {
		return ErrUserNotFound
	}
	if strings.TrimSpace(confirmationUsername) != target.Username {
		return ErrDeleteConfirmation
	}
	if strings.EqualFold(strings.TrimSpace(actorUsername), target.Username) {
		return ErrDeleteSelf
	}
	if target.Role == rbac.RoleSuperAdmin {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM users u JOIN roles r ON r.id = u.role_id WHERE r.name = $1`, string(rbac.RoleSuperAdmin)).Scan(&count); err != nil {
			return fmt.Errorf("count super admins: %w", err)
		}
		if count <= 1 {
			return ErrLastSuperAdmin
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE customer_accounts SET approved_by = NULL WHERE approved_by = $1`, id); err != nil {
		return fmt.Errorf("clear approval references: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrUserNotFound
	}
	return tx.Commit()
}

func (s *Service) TenantScope(username string, role rbac.Role) (tenant.Scope, error) {
	scope := tenant.Scope{Username: strings.TrimSpace(strings.ToLower(username)), Role: role}
	if role == rbac.RoleSuperAdmin || role == rbac.RoleSuperUser {
		return scope, nil
	}
	err := s.db.QueryRow(
		`SELECT ca.id
		   FROM customer_accounts ca
		   JOIN customer_account_users cau ON cau.account_id = ca.id
		   JOIN users u ON u.id = cau.user_id
		  WHERE u.username = $1
		    AND u.status = 'active'
		    AND u.email_verified_at IS NOT NULL
		    AND ca.status = 'active'
		  ORDER BY ca.id ASC
		  LIMIT 1`,
		scope.Username,
	).Scan(&scope.CustomerAccountID)
	if errors.Is(err, sql.ErrNoRows) {
		return tenant.Scope{}, ErrAccountInactive
	}
	if err != nil {
		return tenant.Scope{}, fmt.Errorf("resolve customer account: %w", err)
	}
	return scope, nil
}

func (s *Service) Authenticate(username, password string) (User, error) {
	username = strings.TrimSpace(strings.ToLower(username))

	var user User
	var roleName string
	err := s.db.QueryRow(
		`SELECT u.id, u.username, u.full_name, u.email, u.contact_number, u.password_hash, u.status, u.email_verified_at, u.created_at, r.name,
		        u.two_factor_enabled, COALESCE(u.two_factor_method, 'none'), COALESCE(u.two_factor_secret, ''),
		        COALESCE(u.two_factor_pending_secret, ''), u.two_factor_enabled_at, COALESCE(u.last_login_ip, '')
		 FROM users u
		 JOIN roles r ON r.id = u.role_id
		 WHERE u.username = $1`,
		username,
	).Scan(&user.ID, &user.Username, &user.FullName, &user.Email, &user.ContactNumber, &user.PasswordHash, &user.Status, &user.EmailVerifiedAt, &user.CreatedAt, &roleName, &user.TwoFactorEnabled, &user.TwoFactorMethod, &user.TwoFactorSecret, &user.TwoFactorPendingSecret, &user.TwoFactorEnabledAt, &user.LastLoginIP)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrInvalidCredentials
		}
		return User{}, fmt.Errorf("fetch user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return User{}, ErrInvalidCredentials
	}

	user.Role = rbac.Role(roleName)
	return sanitizeUser(user), nil
}

func (s *Service) GetByUsername(username string) (User, bool) {
	username = strings.TrimSpace(strings.ToLower(username))

	var user User
	var roleName string
	err := s.db.QueryRow(
		`SELECT u.id, u.username, u.full_name, u.email, u.contact_number, u.status, u.email_verified_at, u.created_at, r.name,
		        u.two_factor_enabled, COALESCE(u.two_factor_method, 'none'), COALESCE(u.two_factor_secret, ''),
		        COALESCE(u.two_factor_pending_secret, ''), u.two_factor_enabled_at, COALESCE(u.last_login_ip, '')
		 FROM users u
		 JOIN roles r ON r.id = u.role_id
		 WHERE u.username = $1`,
		username,
	).Scan(&user.ID, &user.Username, &user.FullName, &user.Email, &user.ContactNumber, &user.Status, &user.EmailVerifiedAt, &user.CreatedAt, &roleName, &user.TwoFactorEnabled, &user.TwoFactorMethod, &user.TwoFactorSecret, &user.TwoFactorPendingSecret, &user.TwoFactorEnabledAt, &user.LastLoginIP)
	if err != nil {
		return User{}, false
	}

	user.Role = rbac.Role(roleName)
	return sanitizeUser(user), true
}

func (s *Service) GetByID(id int64) (User, bool) {
	var user User
	var roleName string
	err := s.db.QueryRow(
		`SELECT u.id, u.username, u.full_name, u.email, u.contact_number, u.status, u.email_verified_at, u.created_at, r.name,
		        u.two_factor_enabled, COALESCE(u.two_factor_method, 'none'), COALESCE(u.two_factor_secret, ''),
		        COALESCE(u.two_factor_pending_secret, ''), u.two_factor_enabled_at, COALESCE(u.last_login_ip, '')
		 FROM users u
		 JOIN roles r ON r.id = u.role_id
		 WHERE u.id = $1`,
		id,
	).Scan(&user.ID, &user.Username, &user.FullName, &user.Email, &user.ContactNumber, &user.Status, &user.EmailVerifiedAt, &user.CreatedAt, &roleName, &user.TwoFactorEnabled, &user.TwoFactorMethod, &user.TwoFactorSecret, &user.TwoFactorPendingSecret, &user.TwoFactorEnabledAt, &user.LastLoginIP)
	if err != nil {
		return User{}, false
	}
	user.Role = rbac.Role(roleName)
	return sanitizeUser(user), true
}

func (s *Service) ListUsers() []User {
	rows, err := s.db.Query(
		`SELECT u.id, u.username, u.full_name, u.email, u.contact_number, u.status, u.email_verified_at, u.created_at, r.name,
		        u.two_factor_enabled, COALESCE(u.two_factor_method, 'none'), COALESCE(u.two_factor_secret, ''),
		        COALESCE(u.two_factor_pending_secret, ''), u.two_factor_enabled_at, COALESCE(u.last_login_ip, '')
		 FROM users u
		 JOIN roles r ON r.id = u.role_id
		 ORDER BY u.created_at ASC`,
	)
	if err != nil {
		return []User{}
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		var user User
		var roleName string
		if scanErr := rows.Scan(&user.ID, &user.Username, &user.FullName, &user.Email, &user.ContactNumber, &user.Status, &user.EmailVerifiedAt, &user.CreatedAt, &roleName, &user.TwoFactorEnabled, &user.TwoFactorMethod, &user.TwoFactorSecret, &user.TwoFactorPendingSecret, &user.TwoFactorEnabledAt, &user.LastLoginIP); scanErr != nil {
			continue
		}
		user.Role = rbac.Role(roleName)
		users = append(users, sanitizeUser(user))
	}

	return users
}

func (s *Service) getRoleID(role rbac.Role) (int64, error) {
	var roleID int64
	err := s.db.QueryRow(`SELECT id FROM roles WHERE name = $1`, string(role)).Scan(&roleID)
	if err != nil {
		return 0, fmt.Errorf("resolve role id: %w", err)
	}
	return roleID, nil
}

func sanitizeUser(u User) User {
	if strings.TrimSpace(u.TwoFactorMethod) == "" {
		u.TwoFactorMethod = twoFactorMethodNone
	}
	u.PasswordHash = ""
	u.TwoFactorSecret = ""
	u.TwoFactorPendingSecret = ""
	u.LastLoginIP = ""
	return u
}

func isValidStatus(status Status) bool {
	switch status {
	case StatusEmailPending, StatusPendingApproval, StatusActive, StatusSuspended, StatusRejected:
		return true
	default:
		return false
	}
}

func newVerificationToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate verification token: %w", err)
	}
	token := hex.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func verificationURL(publicAppURL, token string) string {
	base := strings.TrimRight(strings.TrimSpace(publicAppURL), "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	u, err := url.Parse(base + "/verify-email")
	if err != nil {
		return base + "/verify-email?token=" + url.QueryEscape(token)
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String()
}

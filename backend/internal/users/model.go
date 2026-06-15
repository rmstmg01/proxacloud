package users

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"proxacloud/backend/internal/rbac"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserExists         = errors.New("user already exists")
	ErrInvalidRole        = errors.New("invalid role")
)

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Role         rbac.Role `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	PasswordHash string    `json:"-"`
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

	_, err := s.CreateUser(username, password, rbac.RoleSuperAdmin)
	if err != nil && !errors.Is(err, ErrUserExists) {
		return err
	}
	return nil
}

func (s *Service) CreateUser(username, password string, role rbac.Role) (User, error) {
	username = strings.TrimSpace(strings.ToLower(username))
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
		`INSERT INTO users (username, password_hash, role_id)
		 VALUES ($1, $2, $3)
		 RETURNING id, username, created_at`,
		username,
		string(hash),
		roleID,
	).Scan(&created.ID, &created.Username, &created.CreatedAt)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			return User{}, ErrUserExists
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}

	created.Role = role
	return sanitizeUser(created), nil
}

func (s *Service) Authenticate(username, password string) (User, error) {
	username = strings.TrimSpace(strings.ToLower(username))

	var user User
	var roleName string
	err := s.db.QueryRow(
		`SELECT u.id, u.username, u.password_hash, u.created_at, r.name
		 FROM users u
		 JOIN roles r ON r.id = u.role_id
		 WHERE u.username = $1`,
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt, &roleName)
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
		`SELECT u.id, u.username, u.created_at, r.name
		 FROM users u
		 JOIN roles r ON r.id = u.role_id
		 WHERE u.username = $1`,
		username,
	).Scan(&user.ID, &user.Username, &user.CreatedAt, &roleName)
	if err != nil {
		return User{}, false
	}

	user.Role = rbac.Role(roleName)
	return sanitizeUser(user), true
}

func (s *Service) ListUsers() []User {
	rows, err := s.db.Query(
		`SELECT u.id, u.username, u.created_at, r.name
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
		if scanErr := rows.Scan(&user.ID, &user.Username, &user.CreatedAt, &roleName); scanErr != nil {
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
	u.PasswordHash = ""
	return u
}

package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

func TestIdentityWriteError(t *testing.T) {
	for _, tc := range []struct{ constraint, message string }{
		{"users_username_key", "username already in use"},
		{"users_username_normalized_key", "username already in use"},
		{"users_email_normalized_key", "email already in use"},
	} {
		err := identityWriteError(fmt.Errorf("wrapped: %w", &pq.Error{Code: "23505", Constraint: tc.constraint}))
		if !errors.Is(err, ErrUserExists) || err.Error() != tc.message {
			t.Fatalf("%s: %v", tc.constraint, err)
		}
	}
	original := errors.New("connection failed")
	if identityWriteError(original) != original {
		t.Fatal("non-conflict error changed")
	}
}

// Uses an isolated schema, never the application's tables. Requires a test PostgreSQL DSN.
func TestIdentityPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not set")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("identity_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP SCHEMA " + pq.QuoteIdentifier(schema) + " CASCADE")
	connector, err := pq.NewConnector(dsn)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec("SET search_path TO " + pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	must := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`CREATE TABLE roles(id BIGSERIAL PRIMARY KEY, name TEXT);
 INSERT INTO roles(name) VALUES ('normal_user');
 CREATE TABLE users(id BIGSERIAL PRIMARY KEY, username TEXT UNIQUE NOT NULL, email TEXT NOT NULL DEFAULT '',
 first_name TEXT DEFAULT '', last_name TEXT DEFAULT '', full_name TEXT DEFAULT '', contact_number TEXT DEFAULT '',
 password_hash TEXT DEFAULT '', role_id BIGINT DEFAULT 1, status TEXT DEFAULT 'active', email_verified_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ DEFAULT NOW(), two_factor_enabled BOOLEAN DEFAULT false, two_factor_method TEXT,
 two_factor_secret TEXT, two_factor_pending_secret TEXT, two_factor_enabled_at TIMESTAMPTZ, last_login_ip TEXT);`)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "0073_user_login_identifiers.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	must(`INSERT INTO users(username,email) VALUES ('one','shared@example.com'),('two',' SHARED@example.com ')`)
	if _, err = db.Exec(string(migration)); err == nil {
		t.Fatal("duplicate migration succeeded")
	}
	must("ROLLBACK")
	must("DELETE FROM users")
	must(string(migration))
	must(string(migration)) // startup runner repeats migrations
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	must(`INSERT INTO users(username,email,password_hash) VALUES ('alice','alice@example.com',$1)`, string(hash))
	for _, pair := range [][2]string{{" ALICE ", "other@example.com"}, {"bob", " ALICE@example.com "}, {"alice@example.com", "bob@example.com"}, {"bob", "alice"}} {
		_, err := db.Exec(`INSERT INTO users(username,email) VALUES ($1,$2)`, pair[0], pair[1])
		if !errors.Is(identityWriteError(err), ErrUserExists) {
			t.Fatalf("collision %v: %v", pair, err)
		}
	}
	service := NewService(db)
	for _, identifier := range []string{"alice", " ALICE ", " ALICE@EXAMPLE.COM "} {
		user, err := service.Authenticate(identifier, "password123")
		if err != nil || user.Username != "alice" || user.PasswordHash != "" {
			t.Fatalf("login %s: %v", identifier, err)
		}
	}
	for _, identifier := range []string{"alice", "alice@example.com", "missing"} {
		if _, err := service.Authenticate(identifier, "wrong"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("bad password: %v", err)
		}
	}
	must(`INSERT INTO users(username,email) VALUES ('bob','bob@example.com')`)
	if _, err := db.Exec(`UPDATE users SET email='ALICE@example.com' WHERE username='bob'`); !errors.Is(identityWriteError(err), ErrUserExists) {
		t.Fatalf("update collision: %v", err)
	}
	must(`UPDATE users SET email='new@example.com' WHERE username='alice'`)
	must(`INSERT INTO users(username,email) VALUES ('carol','alice@example.com')`)
	must(`DELETE FROM users WHERE username='carol'`)
	must(`INSERT INTO users(username,email) VALUES ('carol','alice@example.com')`)
	// Independent connections exercise real concurrent unique-key arbitration.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn, e := admin.Conn(context.Background())
			if e != nil {
				results <- e
				return
			}
			defer conn.Close()
			if _, e = conn.ExecContext(context.Background(), "SET search_path TO "+pq.QuoteIdentifier(schema)); e != nil {
				results <- e
				return
			}
			defer conn.ExecContext(context.Background(), "RESET search_path")
			_, e = conn.ExecContext(context.Background(), `INSERT INTO users(username,email) VALUES ($1,$2)`, fmt.Sprintf("race%d", i), "race@example.com")
			results <- e
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(identityWriteError(e), ErrUserExists) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatalf("concurrent successes: %d", success)
	}
	testApprovalVerification(t, db)
}

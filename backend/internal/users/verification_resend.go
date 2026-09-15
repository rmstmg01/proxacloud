package users

import (
	"database/sql"
	"errors"
	"strings"
)

func (s *Service) ResendVerification(email, appURL string) (RegistrationResult, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return RegistrationResult{}, false, err
	}
	defer tx.Rollback()
	var user User
	err = tx.QueryRow(`SELECT id,email FROM users WHERE lower(btrim(email))=$1 AND status='email_pending' AND email_verified_at IS NULL FOR UPDATE`, strings.ToLower(strings.TrimSpace(email))).Scan(&user.ID, &user.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return RegistrationResult{}, false, nil
	}
	if err != nil {
		return RegistrationResult{}, false, err
	}
	var recent bool
	err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM email_verification_tokens WHERE user_id=$1 AND created_at>NOW()-INTERVAL '1 minute')`, user.ID).Scan(&recent)
	if err != nil || recent {
		return RegistrationResult{}, false, err
	}
	token, hash, err := newVerificationToken()
	if err != nil {
		return RegistrationResult{}, false, err
	}
	_, err = tx.Exec(`INSERT INTO email_verification_tokens(user_id,token_hash,expires_at) VALUES ($1,$2,NOW()+INTERVAL '24 hours')`, user.ID, hash)
	if err != nil {
		return RegistrationResult{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return RegistrationResult{}, false, err
	}
	return RegistrationResult{User: user, VerificationURL: verificationURL(appURL, token)}, true, nil
}

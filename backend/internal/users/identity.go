package users

import (
	"errors"
	"github.com/lib/pq"
)

// Preserve ErrUserExists for callers while returning a field-specific message.
type identityConflict string

func (e identityConflict) Error() string { return string(e) }
func (e identityConflict) Unwrap() error { return ErrUserExists }

func identityWriteError(err error) error {
	var pg *pq.Error
	if errors.As(err, &pg) && pg.Code == "23505" {
		switch pg.Constraint {
		case "users_username_key", "users_username_normalized_key":
			return identityConflict("username already in use")
		case "users_email_normalized_key":
			return identityConflict("email already in use")
		}
	}
	return err
}

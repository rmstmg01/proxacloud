package tenant

import "proxacloud/backend/internal/rbac"

type Scope struct {
	Username          string
	Role              rbac.Role
	CustomerAccountID int64
}

func (s Scope) IsAdmin() bool {
	return s.Role == rbac.RoleSuperAdmin || s.Role == rbac.RoleSuperUser
}

func (s Scope) IsCustomer() bool {
	return !s.IsAdmin() && s.CustomerAccountID > 0
}

package rbac

type Role string

const (
	RoleNormalUser Role = "normal_user"
	RoleSuperUser  Role = "super_user"
	RoleSuperAdmin Role = "super_admin"
)

func IsValidRole(role Role) bool {
	switch role {
	case RoleNormalUser, RoleSuperUser, RoleSuperAdmin:
		return true
	default:
		return false
	}
}
